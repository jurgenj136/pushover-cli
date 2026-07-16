package settings

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type settingsConfig struct {
	value         config.Config
	commitErr     error
	commits       int
	applyOnCommit bool
	loadErr       error
	failLoadAfter int
	loads         int
}

func (s *settingsConfig) Load(context.Context) (config.Config, error) {
	s.loads++
	if s.loadErr != nil && s.loads > s.failLoadAfter {
		return config.Config{}, s.loadErr
	}
	return config.Clone(s.value), nil
}
func (s *settingsConfig) Commit(_ context.Context, value config.Config) error {
	s.commits++
	if s.applyOnCommit || s.commitErr == nil {
		s.value = config.Clone(value)
	}
	return s.commitErr
}
func (s *settingsConfig) Path() string { return "" }

type settingsSecrets struct {
	values             map[domain.SecretRef][]byte
	putRef             domain.SecretRef
	putErr, deleteErr  error
	puts, gets, checks int
	deleted            []domain.SecretRef
}

func (s *settingsSecrets) Get(context.Context, domain.SecretRef) ([]byte, error) {
	s.gets++
	return nil, errors.New("Get must not be called")
}
func (s *settingsSecrets) Put(_ context.Context, value []byte) (domain.SecretRef, error) {
	s.puts++
	ref := s.putRef
	if ref == "" {
		ref = domain.SecretRef("secret" + string(rune('a'+s.puts)))
	}
	s.values[ref] = append([]byte(nil), value...)
	return ref, s.putErr
}
func (s *settingsSecrets) Delete(_ context.Context, ref domain.SecretRef) error {
	s.deleted = append(s.deleted, ref)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.values, ref)
	return nil
}
func (s *settingsSecrets) Check(_ context.Context, ref domain.SecretRef) error {
	s.checks++
	if _, ok := s.values[ref]; !ok {
		return errors.New("missing")
	}
	return nil
}
func baseSettingsConfig() config.Config {
	return config.Config{SchemaVersion: 1, Backend: config.Backend{Type: "native"}, Profiles: map[string]config.Profile{}, Recipients: map[string]config.Recipient{}}
}
func profileToken() []byte { return []byte(strings.Repeat("a", 30)) }
func userKey() []byte      { return []byte(strings.Repeat("b", 30)) }
func codeOf(err error) string {
	var typed *apperror.Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

func TestAddProfileStagesThenCommitsAndViewsCheckOnly(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}}
	manager := &Manager{Config: configuration, Secrets: secret}
	view, err := manager.AddProfile(context.Background(), ProfileInput{Name: "z", Token: profileToken()})
	if err != nil || !view.TokenResolves || configuration.commits != 1 || secret.puts != 1 || secret.gets != 0 {
		t.Fatalf("view=%#v err=%v commits=%d puts=%d gets=%d", view, err, configuration.commits, secret.puts, secret.gets)
	}
	profiles, err := manager.Profiles(context.Background())
	if err != nil || len(profiles) != 1 || secret.gets != 0 {
		t.Fatalf("profiles=%#v err=%v gets=%d", profiles, err, secret.gets)
	}
}
func TestAddProfileRollsBackOnlyDefiniteConfigFailure(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig(), commitErr: config.NewCommitError(config.Unchanged, errors.New("no"))}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}}
	_, err := (&Manager{Config: configuration, Secrets: secret}).AddProfile(context.Background(), ProfileInput{Name: "p", Token: profileToken()})
	if codeOf(err) != "configuration_commit_failed" || len(secret.deleted) != 1 || len(secret.values) != 0 {
		t.Fatalf("err=%v deleted=%v values=%v", err, secret.deleted, secret.values)
	}
}
func TestStagedSecretUncertaintyDoesNotCommit(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}, putRef: "secret", putErr: errors.New("durability")}
	_, err := (&Manager{Config: configuration, Secrets: secret}).AddProfile(context.Background(), ProfileInput{Name: "p", Token: profileToken()})
	if codeOf(err) != "secret_store_unavailable" || configuration.commits != 0 || len(secret.deleted) != 1 {
		t.Fatalf("err=%v commits=%d deleted=%v", err, configuration.commits, secret.deleted)
	}
}
func TestRollbackDeleteFailureIsSurfaced(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig(), commitErr: config.NewCommitError(config.Unchanged, errors.New("no"))}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}, deleteErr: errors.New("offline")}
	_, err := (&Manager{Config: configuration, Secrets: secret}).AddProfile(context.Background(), ProfileInput{Name: "p", Token: profileToken()})
	if codeOf(err) != "secret_cleanup_required" {
		t.Fatalf("err=%v", err)
	}
}
func TestRollbackAttemptsEveryStagedSecret(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig(), commitErr: config.NewCommitError(config.Unchanged, errors.New("no"))}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}, deleteErr: errors.New("offline")}
	_, err := (&Manager{Config: configuration, Secrets: secret}).AddRecipient(context.Background(), RecipientInput{Alias: "r", UserKey: userKey(), E2EEKey: []byte(strings.Repeat("c", 64))})
	if codeOf(err) != "secret_cleanup_required" || len(secret.deleted) != 2 {
		t.Fatalf("err=%v deleted=%v", err, secret.deleted)
	}
}
func TestUnknownConfigCommitReconcilesStagedSecretsConservatively(t *testing.T) {
	unknown := config.NewCommitError(config.AppliedDurabilityUnknown, errors.New("flush"))
	for _, test := range []struct {
		name       string
		apply      bool
		loadErr    error
		deleteErr  error
		wantCode   string
		wantDelete int
		wantValues int
	}{
		{"visible after retains", true, nil, nil, "configuration_durability_unknown", 0, 1},
		{"visible before cleans", false, nil, nil, "configuration_durability_unknown", 1, 0},
		{"reload failure retains", false, errors.New("reload"), nil, "configuration_durability_unknown", 0, 1},
		{"cleanup failure reported", false, nil, errors.New("cleanup"), "secret_cleanup_required", 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuration := &settingsConfig{value: baseSettingsConfig(), commitErr: unknown, applyOnCommit: test.apply, loadErr: test.loadErr, failLoadAfter: 1}
			secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}, deleteErr: test.deleteErr}
			_, err := (&Manager{Config: configuration, Secrets: secret}).AddProfile(context.Background(), ProfileInput{Name: "p", Token: profileToken()})
			if codeOf(err) != test.wantCode || len(secret.deleted) != test.wantDelete || len(secret.values) != test.wantValues {
				t.Fatalf("err=%v deleted=%v values=%v", err, secret.deleted, secret.values)
			}
		})
	}
}
func TestRemoveRecipientCommitsBeforeCleanupAndKeepsSharedReferences(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Recipients = map[string]config.Recipient{"a": {Alias: "a", UserKeyRef: "shared"}, "b": {Alias: "b", UserKeyRef: "shared"}}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"shared": userKey()}}
	warnings, err := (&Manager{Config: configuration, Secrets: secret}).RemoveRecipient(context.Background(), "a")
	if err != nil || warnings != nil || len(secret.deleted) != 0 || configuration.commits != 1 {
		t.Fatalf("warnings=%v err=%v deleted=%v commits=%d", warnings, err, secret.deleted, configuration.commits)
	}
	_, err = (&Manager{Config: configuration, Secrets: secret}).RemoveRecipient(context.Background(), "b")
	if err != nil || len(secret.deleted) != 1 {
		t.Fatalf("err=%v deleted=%v", err, secret.deleted)
	}
}
func TestViewsSortAndDoNotExposeReferences(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Profiles = map[string]config.Profile{"z": {Name: "z", TokenRef: "token-z"}, "a": {Name: "a", TokenRef: "token-a"}}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"token-z": profileToken(), "token-a": profileToken()}}
	views, err := (&Manager{Config: configuration, Secrets: secret}).Profiles(context.Background())
	if err != nil || len(views) != 2 || !sort.StringsAreSorted([]string{views[0].Name, views[1].Name}) || secret.gets != 0 {
		t.Fatalf("views=%#v err=%v gets=%d", views, err, secret.gets)
	}
}
func TestRecipientViewsAreSortedAndUseChecks(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Recipients = map[string]config.Recipient{
		"z": {Alias: "z", UserKeyRef: "user-z", E2EEKeyRef: "key-z", Kind: domain.KindTeamGroup, Validated: true, Devices: []string{"phone"}},
		"a": {Alias: "a", UserKeyRef: "user-a", Kind: domain.KindUnknown},
	}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"user-z": userKey(), "key-z": []byte(strings.Repeat("c", 64)), "user-a": userKey()}}
	views, err := (&Manager{Config: configuration, Secrets: secret}).Recipients(context.Background())
	if err != nil || len(views) != 2 || views[0].Alias != "a" || !views[1].UserKeyResolves || !views[1].E2EEKeyResolves || views[1].Kind != domain.KindTeamGroup || secret.gets != 0 {
		t.Fatalf("views=%#v err=%v gets=%d", views, err, secret.gets)
	}
}
func TestRemoveDefaultProfileRequiresReplacementOrForce(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.DefaultProfile = "old"
	configuration.value.Profiles = map[string]config.Profile{"old": {Name: "old", TokenRef: "old-secret"}, "new": {Name: "new", TokenRef: "new-secret"}}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"old-secret": profileToken(), "new-secret": profileToken()}}
	manager := &Manager{Config: configuration, Secrets: secret}
	if _, err := manager.RemoveProfile(context.Background(), "old", RemoveProfileOptions{}); codeOf(err) != "default_profile_replacement_required" {
		t.Fatalf("err=%v", err)
	}
	if _, err := manager.RemoveProfile(context.Background(), "old", RemoveProfileOptions{ReplaceWith: "old"}); codeOf(err) != "profile_replacement_invalid" {
		t.Fatalf("err=%v", err)
	}
	if _, err := manager.RemoveProfile(context.Background(), "old", RemoveProfileOptions{ReplaceWith: "new"}); err != nil || configuration.value.DefaultProfile != "new" {
		t.Fatalf("err=%v default=%q", err, configuration.value.DefaultProfile)
	}
	configuration.value.DefaultProfile = "new"
	if _, err := manager.RemoveProfile(context.Background(), "new", RemoveProfileOptions{Force: true}); err != nil || configuration.value.DefaultProfile != "" {
		t.Fatalf("force err=%v default=%q", err, configuration.value.DefaultProfile)
	}
	if _, err := manager.RemoveProfile(context.Background(), "old", RemoveProfileOptions{ReplaceWith: "new", Force: true}); codeOf(err) != "profile_removal_options_conflict" {
		t.Fatalf("conflict err=%v", err)
	}
}
func TestDeleteFailureWarnsAfterConfigurationCommit(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Recipients = map[string]config.Recipient{"r": {Alias: "r", UserKeyRef: "secret"}}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"secret": userKey()}, deleteErr: errors.New("offline")}
	warnings, err := (&Manager{Config: configuration, Secrets: secret}).RemoveRecipient(context.Background(), "r")
	if err != nil || len(warnings) != 1 || warnings[0] != "configuration changed; remove obsolete secret manually" {
		t.Fatalf("warnings=%v err=%v", warnings, err)
	}
	if _, exists := configuration.value.Recipients["r"]; exists {
		t.Fatal("recipient was not removed before warning")
	}
}
