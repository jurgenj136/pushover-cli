package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

const (
	setupToken   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	setupUserKey = "111111111111111111111111111111"
	setupE2EEKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestSetupPlainAndPartialEnvironmentRejectBeforeScopeOrPrompt(t *testing.T) {
	service := &setupSettingsFake{}
	prompter := &setupPrompter{answers: setupAnswers("native", "work", setupToken, "", "ops", "", "", setupUserKey, "", "offline", "")}
	spy := &contextSpy{}
	var stdout, stderr bytes.Buffer
	deps := Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter, Operations: testOperations(spy)}
	if got := Execute(context.Background(), []string{"--plain", "setup"}, testIO(&stdout, &stderr), deps); got != 2 || prompter.reads != 0 || service.bootstraps != 0 || spy.calls != 0 {
		t.Fatalf("plain exit=%d prompts=%d bootstraps=%d scopes=%d", got, prompter.reads, service.bootstraps, spy.calls)
	}
	deps.lookupEnv = func(name string) (string, bool) { return "secret", name == "PUSHOVER_TOKEN" }
	if got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), deps); got != 2 || prompter.reads != 0 || service.bootstraps != 0 || spy.calls != 0 {
		t.Fatalf("environment exit=%d prompts=%d bootstraps=%d scopes=%d", got, prompter.reads, service.bootstraps, spy.calls)
	}
}

func TestSetupExistingConfigurationFailsBeforePrompt(t *testing.T) {
	service := &setupSettingsFake{}
	prompter := &setupPrompter{answers: setupAnswers("native")}
	store := &setupConfigStore{value: configuredSetupConfig()}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, Config: store}, Prompter: prompter})
	if got != 3 || prompter.reads != 0 || service.bootstraps != 0 || store.loads != 1 {
		t.Fatalf("exit=%d prompts=%d bootstraps=%d loads=%d stderr=%q", got, prompter.reads, service.bootstraps, store.loads, stderr.String())
	}
}

func TestSetupValidatesSelectionsBeforeNetworkOrBootstrap(t *testing.T) {
	for _, test := range []struct {
		name    string
		answers [][]byte
	}{
		{name: "backend", answers: setupAnswers("other")},
		{name: "profile", answers: setupAnswers("native", "bad\nprofile")},
		{name: "priority", answers: setupAnswers("native", "work", setupToken, "1")},
		{name: "alias", answers: setupAnswers("native", "work", setupToken, "", "bad\nalias")},
		{name: "kind", answers: setupAnswers("native", "work", setupToken, "", "ops", "other")},
		{name: "group device", answers: setupAnswers("native", "work", setupToken, "", "ops", "group", "phone")},
		{name: "device", answers: setupAnswers("native", "work", setupToken, "", "ops", "user", "bad device")},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &setupSettingsFake{}
			api := &adminAPIFake{}
			prompter := &setupPrompter{answers: test.answers}
			var stdout, stderr bytes.Buffer
			got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
			if got != 4 || len(api.keys) != 0 || service.bootstraps != 0 {
				t.Fatalf("exit=%d api=%d bootstraps=%d stderr=%q", got, len(api.keys), service.bootstraps, stderr.String())
			}
		})
	}
}

func TestSetupValidatesPromptedSecretsBeforeNetworkAndWipesThem(t *testing.T) {
	for _, test := range []struct {
		name    string
		answers [][]byte
		secrets []int
	}{
		{name: "token", answers: setupAnswers("native", "work", "short"), secrets: []int{2}},
		{name: "user key", answers: setupAnswers("native", "work", setupToken, "", "ops", "", "", "short"), secrets: []int{2, 7}},
		{name: "e2ee key", answers: setupAnswers("native", "work", setupToken, "", "ops", "", "", setupUserKey, "not-hex"), secrets: []int{2, 7, 8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &setupSettingsFake{}
			api := &adminAPIFake{}
			prompter := &setupPrompter{answers: test.answers}
			var stdout, stderr bytes.Buffer
			got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
			if got != 4 || len(api.keys) != 0 || service.bootstraps != 0 {
				t.Fatalf("exit=%d api=%d bootstraps=%d", got, len(api.keys), service.bootstraps)
			}
			for _, index := range test.secrets {
				answer := test.answers[index]
				if !allZero(answer) {
					t.Fatalf("secret answer was not wiped: %q", answer)
				}
			}
		})
	}
}

func TestSetupOnlineValidationMustSucceedBeforeBootstrap(t *testing.T) {
	for _, test := range []struct {
		name string
		api  *adminAPIFake
		want int
	}{
		{name: "false", api: &adminAPIFake{validations: []pushover.RecipientValidation{{Valid: false}}}, want: 4},
		{name: "error", api: &adminAPIFake{err: errors.New("network unavailable")}, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &setupSettingsFake{}
			prompter := &setupPrompter{answers: setupAnswers("native", "work", setupToken, "", "ops", "", "", setupUserKey, "", "", "")}
			var stdout, stderr bytes.Buffer
			got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: test.api}, Prompter: prompter})
			if got != test.want || len(test.api.keys) != 1 || service.bootstraps != 0 {
				t.Fatalf("exit=%d api=%d bootstraps=%d", got, len(test.api.keys), service.bootstraps)
			}
		})
	}
}

func TestSetupOfflineStagesUnvalidatedRecipientsWithoutAPI(t *testing.T) {
	service := &setupSettingsFake{}
	api := &adminAPIFake{}
	prompter := &setupPrompter{answers: setupAnswers("file", "work", setupToken, "-1", "ops", "team-group", "phone,watch", setupUserKey, setupE2EEKey, "offline", "")}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--json", "setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
	if got != 0 || len(api.keys) != 0 || service.bootstraps != 1 {
		t.Fatalf("exit=%d api=%d bootstraps=%d stderr=%q", got, len(api.keys), service.bootstraps, stderr.String())
	}
	if input := service.input; !reflect.DeepEqual(input.Backend, config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}) || input.Profile.Name != "work" || input.Profile.Defaults.Priority != -1 || len(input.Recipients) != 1 || input.Recipients[0].Validated || input.Recipients[0].Kind != domain.KindTeamGroup || !reflect.DeepEqual(input.Recipients[0].Devices, []string{"phone", "watch"}) {
		t.Fatalf("bootstrap input = %#v", input)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"resource":"configuration"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"action":"initialized"`)) {
		t.Fatalf("result=%q", stdout.String())
	}
}

func TestSetupOnlinePreservesRecipientOrderAndUsesValidationPhase(t *testing.T) {
	service := &setupSettingsFake{}
	api := &adminAPIFake{validations: []pushover.RecipientValidation{{Valid: true}, {Valid: true}}}
	prompter := &setupPrompter{answers: setupAnswers(
		"native", "work", setupToken, "", "first", "", "phone", setupUserKey, "", "online",
		"second", "", "watch", "222222222222222222222222222222", "", "", "",
	)}
	spy := &contextSpy{}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter, Operations: testOperations(spy)})
	if got != 0 || service.bootstraps != 1 || spy.calls != 3 {
		t.Fatalf("exit=%d bootstraps=%d scopes=%d", got, service.bootstraps, spy.calls)
	}
	if got, want := api.keys, []string{setupUserKey, "222222222222222222222222222222"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("validation order=%q want=%q", got, want)
	}
	if got, want := api.devices, [][]string{{"phone"}, {"watch"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("validation devices=%q want=%q", got, want)
	}
	if got, want := []string{service.input.Recipients[0].Alias, service.input.Recipients[1].Alias}, []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bootstrap order=%q want=%q", got, want)
	}
}

func TestSetupRejectsDuplicateAliasesBeforeValidationOrBootstrap(t *testing.T) {
	service := &setupSettingsFake{}
	api := &adminAPIFake{}
	prompter := &setupPrompter{answers: setupAnswers(
		"native", "work", setupToken, "", "ops", "", "", setupUserKey, "", "offline",
		"ops",
	)}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
	if got != 4 || len(api.keys) != 0 || service.bootstraps != 0 {
		t.Fatalf("exit=%d api=%d bootstraps=%d stderr=%q", got, len(api.keys), service.bootstraps, stderr.String())
	}
}

func TestSetupUsesStablePromptOrderAndProductionManager(t *testing.T) {
	store := &setupConfigStore{loadErr: config.ErrNotFound}
	factory := &setupSecretFactory{}
	prompter := &setupPrompter{answers: setupAnswers("file", "work", setupToken, "", "ops", "", "", setupUserKey, "", "offline", "")}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store, SecretFactory: factory}, Prompter: prompter})
	if got != 0 || factory.opens != 1 || !reflect.DeepEqual(factory.backend, config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}) || store.initializes != 1 {
		t.Fatalf("exit=%d opens=%d backend=%#v initializes=%d stderr=%q", got, factory.opens, factory.backend, store.initializes, stderr.String())
	}
	want := []Prompt{
		{Label: setupBackendLabel}, {Label: setupProfileLabel}, {Label: setupTokenLabel, Secret: true}, {Label: setupPriorityLabel},
		{Label: setupAliasLabel}, {Label: setupKindLabel}, {Label: setupDevicesLabel}, {Label: setupUserKeyLabel, Secret: true}, {Label: setupE2EEKeyLabel, Secret: true}, {Label: setupValidationModeLabel}, {Label: setupAliasLabel},
	}
	if !reflect.DeepEqual(prompter.labels, want) {
		t.Fatalf("prompt order=%#v want=%#v", prompter.labels, want)
	}
}

func TestSetupSummaryConfirmationAndBootstrapFailureAreSideEffectSafe(t *testing.T) {
	for _, test := range []struct {
		name       string
		errWriter  bool
		confirmErr error
		bootErr    error
		want       int
	}{
		{name: "summary", errWriter: true, want: 1},
		{name: "declined", confirmErr: promptError("confirmation_declined", "operation was not confirmed"), want: 8},
		{name: "bootstrap", bootErr: errors.New("write failed"), want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &setupSettingsFake{bootstrapErr: test.bootErr}
			prompter := &setupPrompter{answers: setupAnswers("native", "work", setupToken, "", "ops", "", "", setupUserKey, "", "offline", ""), confirmErr: test.confirmErr}
			var stdout bytes.Buffer
			var stderr io.Writer = &bytes.Buffer{}
			if test.errWriter {
				stderr = failingWriter{}
			}
			got := Execute(context.Background(), []string{"setup"}, IO{In: bytes.NewReader(nil), Out: &stdout, Err: stderr, IsTerminal: func(uintptr) bool { return false }}, Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter})
			wantBootstraps := 0
			if test.bootErr != nil {
				wantBootstraps = 1
			}
			if got != test.want || service.bootstraps != wantBootstraps {
				t.Fatalf("exit=%d bootstraps=%d want=%d", got, service.bootstraps, wantBootstraps)
			}
		})
	}
}

func TestSetupSummaryIsCompleteSafeAndOrdered(t *testing.T) {
	token := []byte(setupToken)
	firstUser := []byte(setupUserKey)
	firstE2EE := []byte(setupE2EEKey)
	secondUser := []byte("222222222222222222222222222222")
	service := &setupSettingsFake{}
	prompter := &setupPrompter{answers: [][]byte{
		[]byte("file"), []byte("work"), token, []byte("-1"),
		[]byte("first"), []byte(""), []byte("phone"), firstUser, firstE2EE, []byte("offline"),
		[]byte("second"), []byte("team-group"), []byte("watch,tablet"), secondUser, []byte(""), []byte("offline"),
		[]byte(""),
	}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter})
	want := "backend=file\n" +
		"profile=work\n" +
		"default_priority=-1\n" +
		"recipients=2\n" +
		"recipient[0].alias=first\n" +
		"recipient[0].kind=unknown\n" +
		"recipient[0].devices=phone\n" +
		"recipient[0].validated=false\n" +
		"recipient[0].e2ee_configured=true\n" +
		"recipient[1].alias=second\n" +
		"recipient[1].kind=team-group\n" +
		"recipient[1].devices=watch,tablet\n" +
		"recipient[1].validated=false\n" +
		"recipient[1].e2ee_configured=false\n"
	if got != 0 || service.bootstraps != 1 || !prompter.confirmed || stderr.String() != want {
		t.Fatalf("exit=%d bootstraps=%d confirmed=%t\nsummary=%q\nwant=%q", got, service.bootstraps, prompter.confirmed, stderr.String(), want)
	}
	for _, secret := range [][]byte{token, firstUser, firstE2EE, secondUser} {
		if !allZero(secret) {
			t.Fatal("summary path did not wipe a retained secret prompt")
		}
	}
	for _, secret := range []string{setupToken, setupUserKey, setupE2EEKey, "222222222222222222222222222222"} {
		if strings.Contains(stderr.String(), secret) {
			t.Fatalf("summary leaked secret %q", secret)
		}
	}
}

func TestSetupRequiresPrompterAndWipesAllSecretsAfterSuccess(t *testing.T) {
	service := &setupSettingsFake{}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}}); got != 8 || service.bootstraps != 0 {
		t.Fatalf("no prompter exit=%d bootstraps=%d", got, service.bootstraps)
	}
	token, user, key := []byte(setupToken), []byte(setupUserKey), []byte(setupE2EEKey)
	prompter := &setupPrompter{answers: [][]byte{[]byte("native"), []byte("work"), token, []byte(""), []byte("ops"), []byte(""), []byte(""), user, key, []byte("offline"), []byte("")}}
	stdout.Reset()
	stderr.Reset()
	if got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 0 || service.bootstraps != 1 || !allZero(token) || !allZero(user) || !allZero(key) || !allZero(service.input.Profile.Token) || !allZero(service.input.Recipients[0].UserKey) || !allZero(service.input.Recipients[0].E2EEKey) {
		t.Fatalf("exit=%d bootstraps=%d token=%t user=%t e2ee=%t", got, service.bootstraps, allZero(token), allZero(user), allZero(key))
	}
	if got := stderr.String(); strings.Contains(got, setupToken) || strings.Contains(got, setupUserKey) || strings.Contains(got, setupE2EEKey) || !strings.Contains(got, "backend=native") || !strings.Contains(got, "profile=work") || !strings.Contains(got, "default_priority=0") || !strings.Contains(got, "recipient[0].alias=ops") || !strings.Contains(got, "recipients=1") {
		t.Fatalf("unsafe or incomplete summary: %q", got)
	}
}

func TestSetupDeadlineAndEOFPreventBootstrap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &setupSettingsFake{}
	prompter := &setupPrompter{answers: setupAnswers("native")}
	var stdout, stderr bytes.Buffer
	if got := Execute(ctx, []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 6 || prompter.reads != 0 || service.bootstraps != 0 {
		t.Fatalf("deadline exit=%d prompts=%d bootstraps=%d", got, prompter.reads, service.bootstraps)
	}
	service = &setupSettingsFake{}
	prompter = &setupPrompter{readErr: io.EOF}
	stdout.Reset()
	stderr.Reset()
	if got := Execute(context.Background(), []string{"setup"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 1 || service.bootstraps != 0 {
		t.Fatalf("EOF exit=%d bootstraps=%d", got, service.bootstraps)
	}
}

func setupAnswers(values ...string) [][]byte {
	answers := make([][]byte, 0, len(values))
	for _, value := range values {
		answers = append(answers, []byte(value))
	}
	return answers
}

type setupSettingsFake struct {
	adminSettingsFake
	bootstraps   int
	input        settings.SetupInput
	bootstrapErr error
}

func (f *setupSettingsFake) Bootstrap(_ context.Context, input settings.SetupInput) (settings.SetupView, error) {
	f.bootstraps++
	f.input = input
	if f.bootstrapErr != nil {
		return settings.SetupView{}, f.bootstrapErr
	}
	return settings.SetupView{}, nil
}

type setupConfigStore struct {
	value       config.Config
	loadErr     error
	loads       int
	initializes int
}

func (s *setupConfigStore) Load(context.Context) (config.Config, error) {
	s.loads++
	return s.value, s.loadErr
}
func (*setupConfigStore) Commit(context.Context, config.Config) error {
	return errors.New("unexpected config write")
}
func (*setupConfigStore) Path() string { return "memory" }
func (s *setupConfigStore) Initialize(_ context.Context, value config.Config) error {
	s.initializes++
	s.value, s.loadErr = value, nil
	return nil
}

type setupSecretFactory struct {
	opens   int
	backend config.Backend
	store   *secrets.MemoryStore
}

func (f *setupSecretFactory) Open(_ context.Context, backend config.Backend) (secrets.Store, error) {
	f.opens++
	f.backend = backend
	if f.store == nil {
		f.store = secrets.NewMemoryStore()
	}
	return f.store, nil
}

func configuredSetupConfig() config.Config {
	return config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: config.Backend{Type: "native"}, Profiles: map[string]config.Profile{}, Recipients: map[string]config.Recipient{}}
}

type setupPrompter struct {
	answers    [][]byte
	labels     []Prompt
	reads      int
	readErr    error
	confirmErr error
	confirmed  bool
}

func (p *setupPrompter) Read(_ context.Context, prompt Prompt) ([]byte, error) {
	p.labels = append(p.labels, prompt)
	p.reads++
	if p.readErr != nil {
		return nil, p.readErr
	}
	if len(p.answers) == 0 {
		return nil, errors.New("missing prompt answer")
	}
	answer := p.answers[0]
	p.answers = p.answers[1:]
	return answer, nil
}

func (p *setupPrompter) Confirm(context.Context, PromptSummary) error {
	p.confirmed = true
	return p.confirmErr
}

var _ settings.Service = (*setupSettingsFake)(nil)
