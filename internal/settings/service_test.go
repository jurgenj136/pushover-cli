package settings

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	notFoundLoads int
	loads         int
}

func (s *settingsConfig) Load(context.Context) (config.Config, error) {
	s.loads++
	if s.loads <= s.notFoundLoads {
		return config.Config{}, config.ErrNotFound
	}
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
func (s *settingsConfig) Initialize(_ context.Context, value config.Config) error {
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
	getErr             error
	failPutAt          int
	puts, gets, checks int
	deleted            []domain.SecretRef
	getRefs            []domain.SecretRef
	getCtx             context.Context
	lastGet            []byte
	blockAfterChecks   int
	ready              chan<- struct{}
	release            <-chan struct{}
}

func (s *settingsSecrets) Get(ctx context.Context, ref domain.SecretRef) ([]byte, error) {
	s.gets++
	s.getRefs = append(s.getRefs, ref)
	s.getCtx = ctx
	if s.getErr != nil {
		return nil, s.getErr
	}
	value, ok := s.values[ref]
	if !ok {
		return nil, errors.New("secret is unavailable")
	}
	s.lastGet = append([]byte(nil), value...)
	return s.lastGet, nil
}
func (s *settingsSecrets) Put(_ context.Context, value []byte) (domain.SecretRef, error) {
	s.puts++
	ref := s.putRef
	if ref == "" {
		ref = domain.SecretRef("secret" + string(rune('a'+s.puts)))
	}
	s.values[ref] = append([]byte(nil), value...)
	if s.failPutAt == s.puts {
		return ref, errors.New("stage failed")
	}
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
	if s.blockAfterChecks != 0 && s.checks == s.blockAfterChecks {
		s.ready <- struct{}{}
		<-s.release
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

func TestBootstrapStagesAllSecretsAndCommitsOneValidInitialConfiguration(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig(), notFoundLoads: 1}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}}
	manager := &Manager{Config: configuration, Secrets: secret}

	input := SetupInput{
		Backend: config.Backend{Type: "file", Metadata: map[string]string{"identity": "local"}},
		Profile: ProfileInput{Name: "main", Token: profileToken()},
		Recipients: []RecipientInput{
			{Alias: "alice", UserKey: userKey(), Validated: true},
			{Alias: "bob", UserKey: userKey(), E2EEKey: []byte(strings.Repeat("c", 64))},
		},
	}
	view, err := manager.Bootstrap(context.Background(), input)
	if err != nil || configuration.commits != 1 || secret.puts != 4 || view.Profile.Name != "main" || len(view.Recipients) != 2 {
		t.Fatalf("view=%#v err=%v commits=%d puts=%d", view, err, configuration.commits, secret.puts)
	}
	if configuration.value.DefaultProfile != "main" || config.Validate(configuration.value) != nil {
		t.Fatalf("stored config=%#v validation=%v", configuration.value, config.Validate(configuration.value))
	}
	input.Backend.Metadata["identity"] = "changed"
	if configuration.value.Backend.Metadata["identity"] != "local" {
		t.Fatalf("backend metadata was not copied: %#v", configuration.value.Backend.Metadata)
	}
}

func TestConcurrentBootstrapAcrossManagersHasOneWinnerAndCleansLoserSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	stores := []*settingsSecrets{
		{values: map[domain.SecretRef][]byte{}, blockAfterChecks: 2, ready: ready, release: release},
		{values: map[domain.SecretRef][]byte{}, blockAfterChecks: 2, ready: ready, release: release},
	}
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range stores {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, errs[index] = (&Manager{Config: config.NewFileStore(path), Secrets: stores[index]}).Bootstrap(context.Background(), SetupInput{
				Backend:    config.Backend{Type: "native"},
				Profile:    ProfileInput{Name: "profile" + string(rune('a'+index)), Token: profileToken()},
				Recipients: []RecipientInput{{Alias: "recipient" + string(rune('a'+index)), UserKey: userKey()}},
			})
		}(index)
	}
	<-ready
	<-ready
	close(release)
	wait.Wait()

	winner, loser := -1, -1
	for index, err := range errs {
		switch {
		case err == nil:
			winner = index
		case codeOf(err) == "configuration_already_exists":
			loser = index
		default:
			t.Fatalf("unexpected bootstrap error %d: %v", index, err)
		}
	}
	if winner < 0 || loser < 0 || len(stores[winner].values) != 2 || len(stores[loser].values) != 0 || len(stores[loser].deleted) != 2 {
		t.Fatalf("winner=%d loser=%d errors=%v values=(%d,%d) deleted=(%v,%v)", winner, loser, errs, len(stores[0].values), len(stores[1].values), stores[0].deleted, stores[1].deleted)
	}
}

func TestBootstrapRejectsExistingOrInvalidInputWithoutStaging(t *testing.T) {
	for _, test := range []struct {
		name          string
		configuration *settingsConfig
		input         SetupInput
		wantCode      string
	}{
		{"existing", &settingsConfig{value: baseSettingsConfig()}, SetupInput{Backend: config.Backend{Type: "native"}, Profile: ProfileInput{Name: "main", Token: profileToken()}, Recipients: []RecipientInput{{Alias: "alice", UserKey: userKey()}}}, "configuration_already_exists"},
		{"duplicate recipient", &settingsConfig{value: baseSettingsConfig(), notFoundLoads: 1}, SetupInput{Backend: config.Backend{Type: "native"}, Profile: ProfileInput{Name: "main", Token: profileToken()}, Recipients: []RecipientInput{{Alias: "alice", UserKey: userKey()}, {Alias: "alice", UserKey: userKey()}}}, "setup_invalid"},
		{"bad backend", &settingsConfig{value: baseSettingsConfig(), notFoundLoads: 1}, SetupInput{Backend: config.Backend{Type: "nope"}, Profile: ProfileInput{Name: "main", Token: profileToken()}, Recipients: []RecipientInput{{Alias: "alice", UserKey: userKey()}}}, "setup_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}}
			_, err := (&Manager{Config: test.configuration, Secrets: secret}).Bootstrap(context.Background(), test.input)
			if codeOf(err) != test.wantCode || test.configuration.commits != 0 || secret.puts != 0 {
				t.Fatalf("err=%v commits=%d puts=%d", err, test.configuration.commits, secret.puts)
			}
		})
	}
}

func TestBootstrapRollsBackStagedSecretsOnFailure(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig(), notFoundLoads: 1, commitErr: config.NewCommitError(config.Unchanged, errors.New("no"))}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}}
	_, err := (&Manager{Config: configuration, Secrets: secret}).Bootstrap(context.Background(), SetupInput{Backend: config.Backend{Type: "native"}, Profile: ProfileInput{Name: "main", Token: profileToken()}, Recipients: []RecipientInput{{Alias: "alice", UserKey: userKey()}}})
	if codeOf(err) != "configuration_commit_failed" || configuration.commits != 1 || len(secret.deleted) != 2 || len(secret.values) != 0 {
		t.Fatalf("err=%v commits=%d deleted=%v values=%v", err, configuration.commits, secret.deleted, secret.values)
	}
}

func TestBootstrapRollsBackPreviouslyStagedSecretsWhenLaterStageFails(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig(), notFoundLoads: 1}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}, failPutAt: 3}
	_, err := (&Manager{Config: configuration, Secrets: secret}).Bootstrap(context.Background(), SetupInput{
		Backend:    config.Backend{Type: "native"},
		Profile:    ProfileInput{Name: "main", Token: profileToken()},
		Recipients: []RecipientInput{{Alias: "alice", UserKey: userKey()}, {Alias: "bob", UserKey: userKey()}},
	})
	if codeOf(err) != "secret_store_unavailable" || configuration.commits != 0 || len(secret.deleted) != 3 || len(secret.values) != 0 {
		t.Fatalf("err=%v commits=%d deleted=%v values=%v", err, configuration.commits, secret.deleted, secret.values)
	}
}

func TestBootstrapReconcilesUnknownCommitLikeOtherStagedTransactions(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig(), notFoundLoads: 1, commitErr: config.NewCommitError(config.AppliedDurabilityUnknown, errors.New("sync"))}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{}}
	_, err := (&Manager{Config: configuration, Secrets: secret}).Bootstrap(context.Background(), SetupInput{Backend: config.Backend{Type: "native"}, Profile: ProfileInput{Name: "main", Token: profileToken()}, Recipients: []RecipientInput{{Alias: "alice", UserKey: userKey()}}})
	if codeOf(err) != "configuration_durability_unknown" || len(secret.deleted) != 2 || len(secret.values) != 0 {
		t.Fatalf("err=%v deleted=%v values=%v", err, secret.deleted, secret.values)
	}
}

func TestRemovalPreviewsArePureAndDescribeSharedSecretConsumers(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.DefaultProfile = "old"
	configuration.value.Profiles = map[string]config.Profile{
		"old": {Name: "old", TokenRef: "shared"},
		"new": {Name: "new", TokenRef: "new-token"},
	}
	configuration.value.Recipients = map[string]config.Recipient{"alice": {Alias: "alice", UserKeyRef: "shared"}}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"shared": profileToken(), "new-token": profileToken()}}
	manager := &Manager{Config: configuration, Secrets: secret}

	impact, err := manager.PreviewRemoveProfile(context.Background(), "old", RemoveProfileOptions{ReplaceWith: "new"})
	if err != nil || !impact.DefaultProfile || impact.ReplacementProfile != "new" || impact.ObsoleteSecretCount != 0 || len(impact.Consumers) != 1 || impact.Consumers[0] != (SecretConsumer{Resource: "recipient", Name: "alice", Field: "user_key"}) {
		t.Fatalf("impact=%#v err=%v", impact, err)
	}
	if configuration.commits != 0 || len(secret.deleted) != 0 || secret.checks != 0 {
		t.Fatalf("preview mutated commits=%d deletes=%v checks=%d", configuration.commits, secret.deleted, secret.checks)
	}
}

func TestPreviewRemoveRecipientIsPureAndReportsConsumersAndObsoleteSecrets(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Profiles = map[string]config.Profile{"main": {Name: "main", TokenRef: "shared"}}
	configuration.value.Recipients = map[string]config.Recipient{
		"remove": {Alias: "remove", UserKeyRef: "shared", E2EEKeyRef: "orphan"},
	}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"shared": profileToken(), "orphan": []byte(strings.Repeat("c", 64))}}
	impact, err := (&Manager{Config: configuration, Secrets: secret}).PreviewRemoveRecipient(context.Background(), "remove")
	if err != nil || impact.DefaultProfile || impact.ReplacementProfile != "" || impact.ObsoleteSecretCount != 1 || len(impact.Consumers) != 1 || impact.Consumers[0] != (SecretConsumer{Resource: "profile", Name: "main", Field: "token"}) {
		t.Fatalf("impact=%#v err=%v", impact, err)
	}
	if configuration.commits != 0 || len(secret.deleted) != 0 || secret.checks != 0 {
		t.Fatalf("preview mutated commits=%d deletes=%v checks=%d", configuration.commits, secret.deleted, secret.checks)
	}
}

func TestRemovalPreviewAndMutationShareReplacementValidation(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.DefaultProfile = "old"
	configuration.value.Profiles = map[string]config.Profile{"old": {Name: "old", TokenRef: "old-token"}, "new": {Name: "new", TokenRef: "new-token"}}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"old-token": profileToken(), "new-token": profileToken()}}
	manager := &Manager{Config: configuration, Secrets: secret}
	for _, options := range []RemoveProfileOptions{{}, {ReplaceWith: "old"}, {ReplaceWith: "new", Force: true}} {
		_, previewErr := manager.PreviewRemoveProfile(context.Background(), "old", options)
		_, removeErr := manager.RemoveProfile(context.Background(), "old", options)
		if codeOf(previewErr) == "" || codeOf(previewErr) != codeOf(removeErr) {
			t.Fatalf("options=%#v preview=%v remove=%v", options, previewErr, removeErr)
		}
	}
}

func TestSelectedProfileTokenReturnsOwnedSelectedOrDefaultCopy(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.DefaultProfile = "default"
	configuration.value.Profiles = map[string]config.Profile{"default": {Name: "default", TokenRef: "default-token"}, "other": {Name: "other", TokenRef: "other-token"}}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"default-token": profileToken(), "other-token": []byte(strings.Repeat("z", 30))}}
	manager := &Manager{Config: configuration, Secrets: secret}
	for _, test := range []struct{ name, selected, want string }{{"default", "", strings.Repeat("a", 30)}, {"selected", "other", strings.Repeat("z", 30)}} {
		t.Run(test.name, func(t *testing.T) {
			token, err := manager.SelectedProfileToken(context.Background(), test.selected)
			if err != nil || string(token) != test.want {
				t.Fatalf("token=%q err=%v", token, err)
			}
			for _, value := range secret.lastGet {
				if value != 0 {
					t.Fatal("intermediate secret buffer was not wiped")
				}
			}
			token[0] = 'x'
			if string(secret.values[configuration.value.Profiles[func() string {
				if test.selected == "" {
					return "default"
				}
				return test.selected
			}()].TokenRef]) == string(token) {
				t.Fatal("token aliases secret store memory")
			}
		})
	}
	if _, err := manager.SelectedProfileToken(context.Background(), "missing"); codeOf(err) != "profile_missing" {
		t.Fatalf("err=%v", err)
	}
}

func TestRecipientUserKeyReturnsOwnedCopyAndFetchesOnlyUserKey(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Recipients = map[string]config.Recipient{
		"alice": {Alias: "alice", UserKeyRef: "user-key", E2EEKeyRef: "e2ee-key"},
	}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{
		"user-key": userKey(),
		"e2ee-key": []byte(strings.Repeat("c", 64)),
	}}
	manager := &Manager{Config: configuration, Secrets: secret}

	got, err := manager.RecipientUserKey(context.Background(), "alice")
	if err != nil || string(got) != string(userKey()) {
		t.Fatalf("key=%q err=%v", got, err)
	}
	if secret.gets != 1 || len(secret.getRefs) != 1 || secret.getRefs[0] != "user-key" || secret.checks != 0 {
		t.Fatalf("gets=%d refs=%v checks=%d", secret.gets, secret.getRefs, secret.checks)
	}
	for _, value := range secret.lastGet {
		if value != 0 {
			t.Fatal("intermediate secret buffer was not wiped")
		}
	}
	got[0] = 'x'
	if string(secret.values["user-key"]) == string(got) {
		t.Fatal("key aliases secret store memory")
	}
}

func TestRecipientUserKeyMissingAliasDoesNotReadSecrets(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Recipients = map[string]config.Recipient{
		"alice": {Alias: "alice", UserKeyRef: "user-key"},
	}
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"user-key": userKey()}}

	_, err := (&Manager{Config: configuration, Secrets: secret}).RecipientUserKey(context.Background(), "missing")
	if codeOf(err) != "recipient_missing" || secret.gets != 0 {
		t.Fatalf("err=%v gets=%d", err, secret.gets)
	}
	if strings.Contains(err.Error(), "user-key") || strings.Contains(err.Error(), "alice") {
		t.Fatalf("error exposed secret or value: %v", err)
	}
}

func TestRecipientUserKeyPropagatesContextAndReturnsTypedSecretError(t *testing.T) {
	configuration := &settingsConfig{value: baseSettingsConfig()}
	configuration.value.Recipients = map[string]config.Recipient{
		"alice": {Alias: "alice", UserKeyRef: "user-key"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	secret := &settingsSecrets{values: map[domain.SecretRef][]byte{"user-key": userKey()}, getErr: context.Canceled}

	_, err := (&Manager{Config: configuration, Secrets: secret}).RecipientUserKey(ctx, "alice")
	if codeOf(err) != "secret_store_unavailable" || secret.getCtx != ctx {
		t.Fatalf("err=%v ctx=%p want=%p", err, secret.getCtx, ctx)
	}
}
