// Package settings owns configuration changes that also affect secret storage.
// Its public views deliberately exclude opaque secret references and values.
package settings

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

type ProfileInput struct {
	Name     string
	Token    []byte
	Defaults config.MessageDefaults
}
type RecipientInput struct {
	Alias            string
	UserKey, E2EEKey []byte
	Kind             domain.TargetKind
	Devices          []string
	Validated        bool
}
type SetupInput struct {
	Backend    config.Backend
	Profile    ProfileInput
	Recipients []RecipientInput
}

type RemoveProfileOptions struct {
	ReplaceWith string
	Force       bool
}
type ProfileView struct {
	Name          string
	Default       bool
	Defaults      config.MessageDefaults
	TokenResolves bool
}
type RecipientView struct {
	Alias                            string
	Kind                             domain.TargetKind
	Validated                        bool
	Devices                          []string
	UserKeyResolves, E2EEKeyResolves bool
}
type SetupView struct {
	Profile    ProfileView
	Recipients []RecipientView
}
type SecretConsumer struct{ Resource, Name, Field string }
type RemovalImpact struct {
	Consumers                           []SecretConsumer
	DefaultProfile, ReplacementRequired bool
	ReplacementProfile                  string
	ObsoleteSecretCount                 int
}

type Service interface {
	Bootstrap(context.Context, SetupInput) (SetupView, error)
	AddProfile(context.Context, ProfileInput) (ProfileView, error)
	Profiles(context.Context) ([]ProfileView, error)
	Profile(context.Context, string) (ProfileView, error)
	UseProfile(context.Context, string) error
	RemoveProfile(context.Context, string, RemoveProfileOptions) ([]string, error)
	AddRecipient(context.Context, RecipientInput) (RecipientView, error)
	Recipients(context.Context) ([]RecipientView, error)
	Recipient(context.Context, string) (RecipientView, error)
	RemoveRecipient(context.Context, string) ([]string, error)
	PreviewRemoveProfile(context.Context, string, RemoveProfileOptions) (RemovalImpact, error)
	PreviewRemoveRecipient(context.Context, string) (RemovalImpact, error)
	// SelectedProfileToken returns an owned copy of the selected or default
	// profile token. The caller must wipe it when it is no longer needed.
	SelectedProfileToken(context.Context, string) ([]byte, error)
	// RecipientUserKey returns an owned copy of a recipient's user key. The
	// caller must wipe it when it is no longer needed.
	RecipientUserKey(context.Context, string) ([]byte, error)
}

type Manager struct {
	Config  config.Store
	Secrets secrets.Store
	mu      sync.Mutex
}

func (m *Manager) Bootstrap(ctx context.Context, input SetupInput) (SetupView, error) {
	if err := validateSetupInput(input); err != nil {
		return SetupView{}, err
	}
	if m == nil || m.Config == nil || m.Secrets == nil {
		return SetupView{}, settingError("configuration_unavailable", "configuration is unavailable", apperror.CategoryConfig)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	initializer, ok := m.Config.(config.Initializer)
	if !ok {
		return SetupView{}, settingError("configuration_unavailable", "configuration is unavailable", apperror.CategoryConfig)
	}
	if _, err := m.Config.Load(ctx); err == nil {
		return SetupView{}, settingError("configuration_already_exists", "configuration already exists", apperror.CategoryConfig)
	} else if !errors.Is(err, config.ErrNotFound) {
		return SetupView{}, settingError("configuration_unavailable", "configuration is unavailable", apperror.CategoryConfig)
	}
	staged := make([]domain.SecretRef, 0, 1+len(input.Recipients)*2)
	profileRef, err := m.stage(ctx, input.Profile.Token)
	if err != nil {
		return SetupView{}, err
	}
	staged = append(staged, profileRef)
	result := config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: copyBackend(input.Backend), DefaultProfile: input.Profile.Name, Profiles: map[string]config.Profile{input.Profile.Name: {Name: input.Profile.Name, TokenRef: profileRef, Defaults: copyDefaults(input.Profile.Defaults)}}, Recipients: make(map[string]config.Recipient, len(input.Recipients))}
	for _, recipient := range input.Recipients {
		userRef, stageErr := m.stage(ctx, recipient.UserKey)
		if stageErr != nil {
			if rollbackErr := m.rollback(staged); rollbackErr != nil {
				return SetupView{}, rollbackErr
			}
			return SetupView{}, stageErr
		}
		staged = append(staged, userRef)
		var e2eeRef domain.SecretRef
		if len(recipient.E2EEKey) != 0 {
			e2eeRef, stageErr = m.stage(ctx, recipient.E2EEKey)
			if stageErr != nil {
				if rollbackErr := m.rollback(staged); rollbackErr != nil {
					return SetupView{}, rollbackErr
				}
				return SetupView{}, stageErr
			}
			staged = append(staged, e2eeRef)
		}
		result.Recipients[recipient.Alias] = config.Recipient{Alias: recipient.Alias, UserKeyRef: userRef, E2EEKeyRef: e2eeRef, Kind: recipient.Kind, Devices: append([]string(nil), recipient.Devices...), Validated: recipient.Validated}
	}
	if err := config.Validate(result); err != nil {
		if rollbackErr := m.rollback(staged); rollbackErr != nil {
			return SetupView{}, rollbackErr
		}
		return SetupView{}, settingError("setup_invalid", "setup input is invalid", apperror.CategoryValidation)
	}
	if err := initializer.Initialize(ctx, result); err != nil {
		if errors.Is(err, config.ErrAlreadyExists) {
			if rollbackErr := m.rollback(staged); rollbackErr != nil {
				return SetupView{}, rollbackErr
			}
			return SetupView{}, settingError("configuration_already_exists", "configuration already exists", apperror.CategoryConfig)
		}
		return SetupView{}, m.commitError(ctx, config.Config{}, result, staged, err)
	}
	view := SetupView{Profile: m.profileView(ctx, result, result.Profiles[input.Profile.Name]), Recipients: make([]RecipientView, 0, len(input.Recipients))}
	for _, recipient := range input.Recipients {
		view.Recipients = append(view.Recipients, m.recipientView(ctx, result.Recipients[recipient.Alias]))
	}
	return view, nil
}

func (m *Manager) AddProfile(ctx context.Context, input ProfileInput) (ProfileView, error) {
	if err := validateProfileInput(input); err != nil {
		return ProfileView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.load(ctx)
	if err != nil {
		return ProfileView{}, err
	}
	if _, exists := before.Profiles[input.Name]; exists {
		return ProfileView{}, settingError("profile_exists", "profile already exists", apperror.CategoryValidation)
	}
	ref, err := m.stage(ctx, input.Token)
	if err != nil {
		return ProfileView{}, err
	}
	after := config.Clone(before)
	after.Profiles[input.Name] = config.Profile{Name: input.Name, TokenRef: ref, Defaults: copyDefaults(input.Defaults)}
	if err := m.commitStaged(ctx, before, after, []domain.SecretRef{ref}); err != nil {
		return ProfileView{}, err
	}
	return m.profileView(ctx, after, after.Profiles[input.Name]), nil
}

func (m *Manager) Profiles(ctx context.Context) ([]ProfileView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(value.Profiles))
	for name := range value.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]ProfileView, 0, len(names))
	for _, name := range names {
		result = append(result, m.profileView(ctx, value, value.Profiles[name]))
	}
	return result, nil
}
func (m *Manager) Profile(ctx context.Context, name string) (ProfileView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load(ctx)
	if err != nil {
		return ProfileView{}, err
	}
	profile, ok := value.Profiles[name]
	if !ok {
		return ProfileView{}, settingError("profile_missing", "profile is unavailable", apperror.CategoryConfig)
	}
	return m.profileView(ctx, value, profile), nil
}
func (m *Manager) UseProfile(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.load(ctx)
	if err != nil {
		return err
	}
	if _, ok := before.Profiles[name]; !ok {
		return settingError("profile_missing", "profile is unavailable", apperror.CategoryConfig)
	}
	after := config.Clone(before)
	after.DefaultProfile = name
	if err := m.Config.Commit(ctx, after); err != nil {
		return m.commitError(ctx, before, after, nil, err)
	}
	return nil
}
func (m *Manager) RemoveProfile(ctx context.Context, name string, options RemoveProfileOptions) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	after, refs, _, err := planProfileRemoval(before, name, options)
	if err != nil {
		return nil, err
	}
	if err := m.Config.Commit(ctx, after); err != nil {
		return nil, m.commitError(ctx, before, after, nil, err)
	}
	return m.deleteOrphaned(ctx, after, refs), nil
}
func (m *Manager) PreviewRemoveProfile(ctx context.Context, name string, options RemoveProfileOptions) (RemovalImpact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.load(ctx)
	if err != nil {
		return RemovalImpact{}, err
	}
	_, _, impact, err := planProfileRemoval(before, name, options)
	return impact, err
}

func (m *Manager) AddRecipient(ctx context.Context, input RecipientInput) (RecipientView, error) {
	if err := validateRecipientInput(input); err != nil {
		return RecipientView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.load(ctx)
	if err != nil {
		return RecipientView{}, err
	}
	if _, exists := before.Recipients[input.Alias]; exists {
		return RecipientView{}, settingError("recipient_exists", "recipient already exists", apperror.CategoryValidation)
	}
	userRef, err := m.stage(ctx, input.UserKey)
	if err != nil {
		return RecipientView{}, err
	}
	staged := []domain.SecretRef{userRef}
	var keyRef domain.SecretRef
	if len(input.E2EEKey) != 0 {
		keyRef, err = m.stage(ctx, input.E2EEKey)
		if err != nil {
			if cleanupErr := m.rollback(staged); cleanupErr != nil {
				return RecipientView{}, cleanupErr
			}
			return RecipientView{}, err
		}
		staged = append(staged, keyRef)
	}
	after := config.Clone(before)
	after.Recipients[input.Alias] = config.Recipient{Alias: input.Alias, UserKeyRef: userRef, E2EEKeyRef: keyRef, Kind: input.Kind, Devices: append([]string(nil), input.Devices...), Validated: input.Validated}
	if err := m.commitStaged(ctx, before, after, staged); err != nil {
		return RecipientView{}, err
	}
	return m.recipientView(ctx, after.Recipients[input.Alias]), nil
}
func (m *Manager) Recipients(ctx context.Context) ([]RecipientView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(value.Recipients))
	for name := range value.Recipients {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]RecipientView, 0, len(names))
	for _, name := range names {
		result = append(result, m.recipientView(ctx, value.Recipients[name]))
	}
	return result, nil
}
func (m *Manager) Recipient(ctx context.Context, alias string) (RecipientView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load(ctx)
	if err != nil {
		return RecipientView{}, err
	}
	recipient, ok := value.Recipients[alias]
	if !ok {
		return RecipientView{}, settingError("recipient_missing", "recipient is unavailable", apperror.CategoryConfig)
	}
	return m.recipientView(ctx, recipient), nil
}
func (m *Manager) RemoveRecipient(ctx context.Context, alias string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	after, refs, _, err := planRecipientRemoval(before, alias)
	if err != nil {
		return nil, err
	}
	if err := m.Config.Commit(ctx, after); err != nil {
		return nil, m.commitError(ctx, before, after, nil, err)
	}
	return m.deleteOrphaned(ctx, after, refs), nil
}
func (m *Manager) PreviewRemoveRecipient(ctx context.Context, alias string) (RemovalImpact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.load(ctx)
	if err != nil {
		return RemovalImpact{}, err
	}
	_, _, impact, err := planRecipientRemoval(before, alias)
	return impact, err
}

// SelectedProfileToken returns an owned copy of the selected profile token.
// An empty name selects the configured default. Callers must wipe the returned
// bytes after use and must not retain them beyond the authenticated operation.
func (m *Manager) SelectedProfileToken(ctx context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = value.DefaultProfile
	}
	profile, ok := value.Profiles[name]
	if !ok {
		return nil, settingError("profile_missing", "profile is unavailable", apperror.CategoryConfig)
	}
	token, err := m.Secrets.Get(ctx, profile.TokenRef)
	if err != nil {
		return nil, settingError("secret_store_unavailable", "secret store is unavailable", apperror.CategoryConfig)
	}
	owned := append([]byte(nil), token...)
	wipe(token)
	return owned, nil
}

// RecipientUserKey returns an owned copy of the exact recipient user key.
// Callers must wipe the returned bytes after use and must not retain them
// beyond the authenticated operation. The recipient's E2EE key is never read.
func (m *Manager) RecipientUserKey(ctx context.Context, alias string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	recipient, ok := value.Recipients[alias]
	if !ok {
		return nil, settingError("recipient_missing", "recipient is unavailable", apperror.CategoryConfig)
	}
	key, err := m.Secrets.Get(ctx, recipient.UserKeyRef)
	if err != nil {
		return nil, settingError("secret_store_unavailable", "secret store is unavailable", apperror.CategoryConfig)
	}
	owned := append([]byte(nil), key...)
	wipe(key)
	return owned, nil
}

func (m *Manager) stage(ctx context.Context, value []byte) (domain.SecretRef, error) {
	if m == nil || m.Secrets == nil {
		return "", settingError("secret_store_unavailable", "secret store is unavailable", apperror.CategoryConfig)
	}
	owned := append([]byte(nil), value...)
	ref, err := m.Secrets.Put(ctx, owned)
	wipe(owned)
	if err != nil || ref == "" {
		if ref != "" {
			if cleanupErr := m.Secrets.Delete(context.Background(), ref); cleanupErr != nil {
				return "", settingError("secret_cleanup_required", "secret cleanup is required", apperror.CategoryConfig)
			}
		}
		return "", settingError("secret_store_unavailable", "secret store is unavailable", apperror.CategoryConfig)
	}
	if m.Secrets.Check(ctx, ref) != nil {
		if cleanupErr := m.Secrets.Delete(context.Background(), ref); cleanupErr != nil {
			return "", settingError("secret_cleanup_required", "secret cleanup is required", apperror.CategoryConfig)
		}
		return "", settingError("secret_store_unavailable", "secret store is unavailable", apperror.CategoryConfig)
	}
	return ref, nil
}
func (m *Manager) commitStaged(ctx context.Context, before, after config.Config, staged []domain.SecretRef) error {
	err := m.Config.Commit(ctx, after)
	if err == nil {
		return nil
	}
	return m.commitError(ctx, before, after, staged, err)
}
func (m *Manager) commitError(ctx context.Context, before, after config.Config, staged []domain.SecretRef, commitErr error) error {
	if config.OutcomeOf(commitErr) == config.Unchanged {
		if cleanupErr := m.rollback(staged); cleanupErr != nil {
			return cleanupErr
		}
		return apperror.Wrap("configuration_commit_failed", apperror.CategoryConfig, "configuration could not be updated", commitErr)
	}
	// After a post-rename failure, only a reload can show which references are
	// currently live. If reload itself fails, retain all staged values: deleting
	// them could create dangling visible configuration.
	visible, reloadErr := m.Config.Load(ctx)
	if reloadErr == nil {
		var cleanupErr error
		for _, ref := range staged {
			if !references(visible, ref) {
				if err := m.Secrets.Delete(context.Background(), ref); err != nil && cleanupErr == nil {
					cleanupErr = err
				}
			}
		}
		if cleanupErr != nil {
			return apperror.Wrap("secret_cleanup_required", apperror.CategoryConfig, "configuration durability is unknown and secret cleanup is required", cleanupErr)
		}
	}
	_ = before
	_ = after
	if reloadErr != nil {
		commitErr = errors.Join(commitErr, reloadErr)
	}
	return apperror.Wrap("configuration_durability_unknown", apperror.CategoryConfig, "configuration update durability is unknown", commitErr)
}
func (m *Manager) rollback(refs []domain.SecretRef) error {
	seen := map[domain.SecretRef]bool{}
	failed := false
	for _, ref := range refs {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			if err := m.Secrets.Delete(context.Background(), ref); err != nil {
				failed = true
			}
		}
	}
	if failed {
		return settingError("secret_cleanup_required", "secret cleanup is required", apperror.CategoryConfig)
	}
	return nil
}
func (m *Manager) deleteOrphaned(ctx context.Context, after config.Config, refs []domain.SecretRef) []string {
	seen := map[domain.SecretRef]bool{}
	failed := false
	for _, ref := range refs {
		if ref == "" || seen[ref] || references(after, ref) {
			continue
		}
		seen[ref] = true
		if err := m.Secrets.Delete(ctx, ref); err != nil {
			failed = true
		}
	}
	if failed {
		return []string{"configuration changed; remove obsolete secret manually"}
	}
	return nil
}
func references(value config.Config, wanted domain.SecretRef) bool {
	if wanted == "" {
		return false
	}
	for _, profile := range value.Profiles {
		if profile.TokenRef == wanted {
			return true
		}
	}
	for _, recipient := range value.Recipients {
		if recipient.UserKeyRef == wanted || recipient.E2EEKeyRef == wanted {
			return true
		}
	}
	return false
}
func planProfileRemoval(before config.Config, name string, options RemoveProfileOptions) (config.Config, []domain.SecretRef, RemovalImpact, error) {
	if options.ReplaceWith != "" && options.Force {
		return config.Config{}, nil, RemovalImpact{}, settingError("profile_removal_options_conflict", "profile removal options cannot be combined", apperror.CategoryUsage)
	}
	profile, ok := before.Profiles[name]
	if !ok {
		return config.Config{}, nil, RemovalImpact{}, settingError("profile_missing", "profile is unavailable", apperror.CategoryConfig)
	}
	if options.ReplaceWith != "" {
		_, exists := before.Profiles[options.ReplaceWith]
		if options.ReplaceWith == name || !exists {
			return config.Config{}, nil, RemovalImpact{}, settingError("profile_replacement_invalid", "profile replacement is invalid", apperror.CategoryValidation)
		}
	}
	isDefault := before.DefaultProfile == name
	if isDefault && options.ReplaceWith == "" && !options.Force {
		return config.Config{}, nil, RemovalImpact{}, settingError("default_profile_replacement_required", "removing the default profile requires a replacement or force", apperror.CategoryValidation)
	}
	after := config.Clone(before)
	delete(after.Profiles, name)
	if isDefault {
		after.DefaultProfile = options.ReplaceWith
	} else {
		options.ReplaceWith = ""
	}
	refs := []domain.SecretRef{profile.TokenRef}
	return after, refs, removalImpact(after, refs, isDefault, options.ReplaceWith, isDefault && options.ReplaceWith == ""), nil
}
func planRecipientRemoval(before config.Config, alias string) (config.Config, []domain.SecretRef, RemovalImpact, error) {
	recipient, ok := before.Recipients[alias]
	if !ok {
		return config.Config{}, nil, RemovalImpact{}, settingError("recipient_missing", "recipient is unavailable", apperror.CategoryConfig)
	}
	after := config.Clone(before)
	delete(after.Recipients, alias)
	refs := []domain.SecretRef{recipient.UserKeyRef, recipient.E2EEKeyRef}
	return after, refs, removalImpact(after, refs, false, "", false), nil
}
func removalImpact(after config.Config, refs []domain.SecretRef, isDefault bool, replacement string, replacementRequired bool) RemovalImpact {
	consumers := secretConsumers(after, refs)
	obsolete := 0
	seen := map[domain.SecretRef]bool{}
	for _, ref := range refs {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			if !references(after, ref) {
				obsolete++
			}
		}
	}
	return RemovalImpact{Consumers: consumers, DefaultProfile: isDefault, ReplacementProfile: replacement, ReplacementRequired: replacementRequired, ObsoleteSecretCount: obsolete}
}
func secretConsumers(value config.Config, refs []domain.SecretRef) []SecretConsumer {
	wanted := map[domain.SecretRef]bool{}
	for _, ref := range refs {
		if ref != "" {
			wanted[ref] = true
		}
	}
	var consumers []SecretConsumer
	for name, profile := range value.Profiles {
		if wanted[profile.TokenRef] {
			consumers = append(consumers, SecretConsumer{"profile", name, "token"})
		}
	}
	for alias, recipient := range value.Recipients {
		if wanted[recipient.UserKeyRef] {
			consumers = append(consumers, SecretConsumer{"recipient", alias, "user_key"})
		}
		if wanted[recipient.E2EEKeyRef] {
			consumers = append(consumers, SecretConsumer{"recipient", alias, "e2ee_key"})
		}
	}
	sort.Slice(consumers, func(i, j int) bool {
		if consumers[i].Resource != consumers[j].Resource {
			return consumers[i].Resource < consumers[j].Resource
		}
		if consumers[i].Name != consumers[j].Name {
			return consumers[i].Name < consumers[j].Name
		}
		return consumers[i].Field < consumers[j].Field
	})
	return consumers
}

func (m *Manager) load(ctx context.Context) (config.Config, error) {
	if m == nil || m.Config == nil || m.Secrets == nil {
		return config.Config{}, settingError("configuration_unavailable", "configuration is unavailable", apperror.CategoryConfig)
	}
	value, err := m.Config.Load(ctx)
	if err != nil {
		return config.Config{}, settingError("configuration_unavailable", "configuration is unavailable", apperror.CategoryConfig)
	}
	return value, nil
}
func (m *Manager) profileView(ctx context.Context, value config.Config, profile config.Profile) ProfileView {
	return ProfileView{Name: profile.Name, Default: value.DefaultProfile == profile.Name, Defaults: copyDefaults(profile.Defaults), TokenResolves: m.Secrets.Check(ctx, profile.TokenRef) == nil}
}
func (m *Manager) recipientView(ctx context.Context, recipient config.Recipient) RecipientView {
	view := RecipientView{Alias: recipient.Alias, Kind: recipient.Kind, Validated: recipient.Validated, Devices: append([]string(nil), recipient.Devices...), UserKeyResolves: m.Secrets.Check(ctx, recipient.UserKeyRef) == nil}
	if recipient.E2EEKeyRef != "" {
		view.E2EEKeyResolves = m.Secrets.Check(ctx, recipient.E2EEKeyRef) == nil
	}
	return view
}
func validateProfileInput(input ProfileInput) error {
	if !domain.IsSafeName(input.Name) || domain.ValidateCredentials(domain.Credentials{Token: string(input.Token), UserKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}) != nil {
		return settingError("profile_invalid", "profile input is invalid", apperror.CategoryValidation)
	}
	if err := domain.ValidateSavedPriority(input.Defaults.Priority); err != nil {
		return settingError("profile_invalid", "profile input is invalid", apperror.CategoryValidation)
	}
	if err := domain.ValidateMessage(domain.Message{Body: "x", Title: input.Defaults.Title, URL: input.Defaults.URL, URLTitle: input.Defaults.URLTitle, Sound: input.Defaults.Sound, Devices: input.Defaults.Devices, Priority: input.Defaults.Priority, TTL: input.Defaults.TTL}); err != nil {
		return settingError("profile_invalid", "profile input is invalid", apperror.CategoryValidation)
	}
	return nil
}
func validateRecipientInput(input RecipientInput) error {
	if !domain.IsSafeName(input.Alias) || domain.ValidateCredentials(domain.Credentials{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserKey: string(input.UserKey)}) != nil {
		return settingError("recipient_invalid", "recipient input is invalid", apperror.CategoryValidation)
	}
	if len(input.E2EEKey) > 0 && domain.ValidateE2EEKey(input.E2EEKey) != nil {
		return settingError("recipient_invalid", "recipient input is invalid", apperror.CategoryValidation)
	}
	if input.Kind != domain.KindUnknown && input.Kind != domain.KindUser && input.Kind != domain.KindGroup && input.Kind != domain.KindTeamGroup {
		return settingError("recipient_invalid", "recipient input is invalid", apperror.CategoryValidation)
	}
	if input.Kind == domain.KindGroup && len(input.Devices) > 0 {
		return settingError("recipient_invalid", "recipient input is invalid", apperror.CategoryValidation)
	}
	for _, device := range input.Devices {
		if domain.ValidateDevice(device) != nil {
			return settingError("recipient_invalid", "recipient input is invalid", apperror.CategoryValidation)
		}
	}
	return nil
}
func validateSetupInput(input SetupInput) error {
	if len(input.Recipients) == 0 || validateProfileInput(input.Profile) != nil {
		return settingError("setup_invalid", "setup input is invalid", apperror.CategoryValidation)
	}
	if config.Validate(config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: input.Backend, Profiles: map[string]config.Profile{}, Recipients: map[string]config.Recipient{}}) != nil {
		return settingError("setup_invalid", "setup input is invalid", apperror.CategoryValidation)
	}
	seen := map[string]bool{}
	for _, recipient := range input.Recipients {
		if seen[recipient.Alias] || validateRecipientInput(recipient) != nil {
			return settingError("setup_invalid", "setup input is invalid", apperror.CategoryValidation)
		}
		seen[recipient.Alias] = true
	}
	return nil
}
func copyDefaults(value config.MessageDefaults) config.MessageDefaults {
	value.Devices = append([]string(nil), value.Devices...)
	return value
}
func copyBackend(value config.Backend) config.Backend {
	metadata := make(map[string]string, len(value.Metadata))
	for key, item := range value.Metadata {
		metadata[key] = item
	}
	value.Metadata = metadata
	return value
}
func settingError(code, message, category string) error { return apperror.New(code, category, message) }

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
