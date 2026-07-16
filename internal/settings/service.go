// Package settings owns configuration changes that also affect secret storage.
// Its public views deliberately exclude opaque secret references and values.
package settings

import (
	"context"
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

type Service interface {
	AddProfile(context.Context, ProfileInput) (ProfileView, error)
	Profiles(context.Context) ([]ProfileView, error)
	Profile(context.Context, string) (ProfileView, error)
	UseProfile(context.Context, string) error
	RemoveProfile(context.Context, string, RemoveProfileOptions) ([]string, error)
	AddRecipient(context.Context, RecipientInput) (RecipientView, error)
	Recipients(context.Context) ([]RecipientView, error)
	Recipient(context.Context, string) (RecipientView, error)
	RemoveRecipient(context.Context, string) ([]string, error)
}

type Manager struct {
	Config  config.Store
	Secrets secrets.Store
	mu      sync.Mutex
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
	if options.ReplaceWith != "" && options.Force {
		return nil, settingError("profile_removal_options_conflict", "profile removal options cannot be combined", apperror.CategoryUsage)
	}
	profile, ok := before.Profiles[name]
	if !ok {
		return nil, settingError("profile_missing", "profile is unavailable", apperror.CategoryConfig)
	}
	if options.ReplaceWith != "" {
		if options.ReplaceWith == name {
			return nil, settingError("profile_replacement_invalid", "profile replacement is invalid", apperror.CategoryValidation)
		}
		if _, ok := before.Profiles[options.ReplaceWith]; !ok {
			return nil, settingError("profile_replacement_invalid", "profile replacement is invalid", apperror.CategoryValidation)
		}
	}
	if before.DefaultProfile == name && options.ReplaceWith == "" && !options.Force {
		return nil, settingError("default_profile_replacement_required", "removing the default profile requires a replacement or force", apperror.CategoryValidation)
	}
	after := config.Clone(before)
	delete(after.Profiles, name)
	if before.DefaultProfile == name {
		after.DefaultProfile = options.ReplaceWith
	}
	if err := m.Config.Commit(ctx, after); err != nil {
		return nil, m.commitError(ctx, before, after, nil, err)
	}
	return m.deleteOrphaned(ctx, after, []domain.SecretRef{profile.TokenRef}), nil
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
	recipient, ok := before.Recipients[alias]
	if !ok {
		return nil, settingError("recipient_missing", "recipient is unavailable", apperror.CategoryConfig)
	}
	after := config.Clone(before)
	delete(after.Recipients, alias)
	if err := m.Config.Commit(ctx, after); err != nil {
		return nil, m.commitError(ctx, before, after, nil, err)
	}
	return m.deleteOrphaned(ctx, after, []domain.SecretRef{recipient.UserKeyRef, recipient.E2EEKeyRef}), nil
}

func (m *Manager) stage(ctx context.Context, value []byte) (domain.SecretRef, error) {
	if m == nil || m.Secrets == nil {
		return "", settingError("secret_store_unavailable", "secret store is unavailable", apperror.CategoryConfig)
	}
	copy := append([]byte(nil), value...)
	ref, err := m.Secrets.Put(ctx, copy)
	wipe(copy)
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
		return settingError("configuration_commit_failed", "configuration could not be updated", apperror.CategoryConfig)
	}
	// After a post-rename failure, only a reload can show which references are
	// currently live. If reload itself fails, retain all staged values: deleting
	// them could create dangling visible configuration.
	visible, err := m.Config.Load(ctx)
	if err == nil {
		cleanupFailed := false
		for _, ref := range staged {
			if !references(visible, ref) && m.Secrets.Delete(context.Background(), ref) != nil {
				cleanupFailed = true
			}
		}
		if cleanupFailed {
			return settingError("secret_cleanup_required", "configuration durability is unknown and secret cleanup is required", apperror.CategoryConfig)
		}
	}
	_ = before
	_ = after
	return settingError("configuration_durability_unknown", "configuration update durability is unknown", apperror.CategoryConfig)
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
func copyDefaults(value config.MessageDefaults) config.MessageDefaults {
	value.Devices = append([]string(nil), value.Devices...)
	return value
}
func settingError(code, message, category string) error { return apperror.New(code, category, message) }

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
