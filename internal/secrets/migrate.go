package secrets

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

// BackendMigrator performs the three-phase secret backend transition: stage
// and verify destination values, atomically commit config, then erase source.
type BackendMigrator struct {
	Config  config.Store
	Factory Factory
}
type MigrationResult struct{ CleanupWarning *apperror.Error }

func (m BackendMigrator) Migrate(ctx context.Context, destination config.Backend) (MigrationResult, error) {
	if err := ctx.Err(); err != nil {
		return MigrationResult{}, err
	}
	if m.Config == nil || m.Factory == nil {
		return MigrationResult{}, unavailable(errors.New("migration is not configured"))
	}
	before, err := m.Config.Load(ctx)
	if err != nil {
		return MigrationResult{}, unavailable(err)
	}
	if sameBackend(before.Backend, destination) {
		return MigrationResult{}, nil
	}
	source, err := m.Factory.Open(ctx, before.Backend)
	if err != nil {
		return MigrationResult{}, err
	}
	target, err := m.Factory.Open(ctx, destination)
	if err != nil {
		return MigrationResult{}, err
	}
	references := configRefs(before)
	staged := make([]domain.SecretRef, 0, len(references))
	replacement := make(map[domain.SecretRef]domain.SecretRef, len(references))
	rollback := func() error {
		var cleanupErr error
		for _, ref := range staged {
			if err := target.Delete(context.Background(), ref); err != nil && cleanupErr == nil {
				cleanupErr = err
			}
		}
		return cleanupErr
	}
	for _, oldRef := range references {
		value, err := source.Get(ctx, oldRef)
		if err != nil {
			if cleanupErr := rollback(); cleanupErr != nil {
				return MigrationResult{}, unavailable(cleanupErr)
			}
			return MigrationResult{}, unavailable(err)
		}
		expected := copyBytes(value)
		newRef, putErr := target.Put(ctx, value)
		wipe(value)
		if putErr != nil {
			// A non-empty ref with an error is a post-rename durability outcome:
			// it may exist, but cannot be safely used for a migration. Track it
			// before rollback so it never becomes an untracked orphan.
			if newRef != "" {
				staged = append(staged, newRef)
			}
			if newRef != "" {
				if check, getErr := target.Get(ctx, newRef); getErr == nil {
					wipe(check)
				}
			}
			wipe(expected)
			if cleanupErr := rollback(); cleanupErr != nil {
				return MigrationResult{}, unavailable(cleanupErr)
			}
			return MigrationResult{}, putErr
		}
		check, err := target.Get(ctx, newRef)
		if err != nil || !bytes.Equal(check, expected) {
			wipe(expected)
			wipe(check)
			if cleanupErr := rollback(); cleanupErr != nil {
				return MigrationResult{}, unavailable(cleanupErr)
			}
			return MigrationResult{}, unavailable(errors.New("destination verification failed"))
		}
		wipe(expected)
		wipe(check)
		staged = append(staged, newRef)
		replacement[oldRef] = newRef
	}
	after := replaceRefs(before, destination, replacement)
	err = m.Config.Commit(ctx, after)
	if err != nil {
		if config.OutcomeOf(err) == config.Unchanged {
			if cleanupErr := rollback(); cleanupErr != nil {
				return MigrationResult{}, unavailable(cleanupErr)
			}
			return MigrationResult{}, unavailable(err)
		}
		visible, loadErr := m.Config.Load(ctx)
		if loadErr != nil {
			return MigrationResult{}, unavailable(errors.New("configuration state is ambiguous"))
		}
		if equivalentMigration(visible, after) {
			return m.cleanup(ctx, source, references)
		}
		if equivalentMigration(visible, before) {
			if cleanupErr := rollback(); cleanupErr != nil {
				return MigrationResult{}, unavailable(cleanupErr)
			}
			return MigrationResult{}, unavailable(err)
		}
		return MigrationResult{}, unavailable(errors.New("configuration state is ambiguous"))
	}
	return m.cleanup(ctx, source, references)
}

func (m BackendMigrator) cleanup(ctx context.Context, source Store, refs []domain.SecretRef) (MigrationResult, error) {
	for _, ref := range refs {
		if err := source.Delete(ctx, ref); err != nil {
			return MigrationResult{CleanupWarning: apperror.Wrap("secret_cleanup_required", apperror.CategoryConfig, "new secret backend is active; remove old secrets manually", err)}, nil
		}
	}
	return MigrationResult{}, nil
}

func configRefs(value config.Config) []domain.SecretRef {
	seen := map[domain.SecretRef]bool{}
	refs := []domain.SecretRef{}
	add := func(ref domain.SecretRef) {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	for _, profile := range value.Profiles {
		add(profile.TokenRef)
	}
	for _, recipient := range value.Recipients {
		add(recipient.UserKeyRef)
		add(recipient.E2EEKeyRef)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i] < refs[j] })
	return refs
}
func replaceRefs(value config.Config, backend config.Backend, replacements map[domain.SecretRef]domain.SecretRef) config.Config {
	value.Backend = backend
	value.Profiles = cloneProfiles(value.Profiles)
	value.Recipients = cloneRecipients(value.Recipients)
	for name, profile := range value.Profiles {
		profile.TokenRef = replacements[profile.TokenRef]
		value.Profiles[name] = profile
	}
	for name, recipient := range value.Recipients {
		recipient.UserKeyRef = replacements[recipient.UserKeyRef]
		if recipient.E2EEKeyRef != "" {
			recipient.E2EEKeyRef = replacements[recipient.E2EEKeyRef]
		}
		value.Recipients[name] = recipient
	}
	return value
}
func cloneProfiles(input map[string]config.Profile) map[string]config.Profile {
	output := make(map[string]config.Profile, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func cloneRecipients(input map[string]config.Recipient) map[string]config.Recipient {
	output := make(map[string]config.Recipient, len(input))
	for key, value := range input {
		value.Devices = append([]string(nil), value.Devices...)
		output[key] = value
	}
	return output
}
func equivalentMigration(left, right config.Config) bool {
	if left.Backend.Type != right.Backend.Type || len(left.Backend.Metadata) != len(right.Backend.Metadata) {
		return false
	}
	for key, value := range right.Backend.Metadata {
		if left.Backend.Metadata[key] != value {
			return false
		}
	}
	if len(left.Profiles) != len(right.Profiles) || len(left.Recipients) != len(right.Recipients) {
		return false
	}
	for key, expected := range right.Profiles {
		actual, ok := left.Profiles[key]
		if !ok || actual.TokenRef != expected.TokenRef {
			return false
		}
	}
	for key, expected := range right.Recipients {
		actual, ok := left.Recipients[key]
		if !ok || actual.UserKeyRef != expected.UserKeyRef || actual.E2EEKeyRef != expected.E2EEKeyRef {
			return false
		}
	}
	return true
}
func sameBackend(left, right config.Backend) bool {
	if left.Type != right.Type || len(left.Metadata) != len(right.Metadata) {
		return false
	}
	for key, value := range left.Metadata {
		if right.Metadata[key] != value {
			return false
		}
	}
	return true
}
