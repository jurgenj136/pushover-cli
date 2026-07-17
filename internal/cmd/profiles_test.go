package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

func TestExecuteProfilesListJSON(t *testing.T) {
	service := &adminSettingsFake{profiles: []settings.ProfileView{{Name: "work", Default: true}}}
	deps := Dependencies{Admin: AdminDependencies{Settings: service}}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"--json", "profiles", "list"}, testIO(&stdout, &stderr), deps); got != 0 {
		t.Fatalf("exit = %d, stderr = %q", got, stderr.String())
	}
	if want := `"command":"profiles list"`; !bytes.Contains(stdout.Bytes(), []byte(want)) {
		t.Fatalf("stdout = %q, want %s", stdout.String(), want)
	}
}

func TestProfilesOutputProbe(t *testing.T) {
	var out bytes.Buffer
	err := (output.JSONRenderer{}).Result(&out, output.ProfilesResult{Meta: output.Meta{Command: "profiles list", Status: "success"}, Rows: []output.ProfileRow{{Name: "work", Default: true}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProfilesGrammarBuilds(t *testing.T) {
	var cli rootCommand
	if _, err := kong.New(&cli); err != nil {
		t.Fatal(err)
	}
}

func TestPreScannedNestedCommandIgnoresGlobalFlagValues(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--profile", "recipients", "profiles", "list"}, "profiles list"},
		{[]string{"--timeout=1s", "recipients", "validate", "ops"}, "recipients validate"},
		{[]string{"profiles"}, "profiles list"},
		{[]string{"recipients", "--"}, "recipients list"},
	} {
		if got := preScannedCommand(test.args); got != test.want {
			t.Fatalf("preScannedCommand(%q) = %q, want %q", test.args, got, test.want)
		}
	}
}

func TestProfilesAddPlainRejectsBeforePromptOrMutation(t *testing.T) {
	service := &adminSettingsFake{}
	prompter := &adminPrompterFake{answers: [][]byte{[]byte("secret")}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--plain", "profiles", "add", "work"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter})
	if got != 2 || prompter.reads != 0 || service.addProfiles != 0 {
		t.Fatalf("exit=%d prompt=%d adds=%d stderr=%q", got, prompter.reads, service.addProfiles, stderr.String())
	}
}

func TestProfilesRemoveSummarizesConsumersBeforeConfirming(t *testing.T) {
	service := &adminSettingsFake{profileImpact: settings.RemovalImpact{Consumers: []settings.SecretConsumer{{Resource: "recipient", Name: "ops", Field: "user_key"}}}}
	prompter := &adminPrompterFake{onConfirm: func() {
		if got, want := service.calls, []string{"preview-profile"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("calls before confirmation = %v, want %v", got, want)
		}
	}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"profiles", "remove", "work"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter})
	if got != 0 || !prompter.confirmed || service.removeProfiles != 1 {
		t.Fatalf("exit=%d confirmed=%t removals=%d stderr=%q", got, prompter.confirmed, service.removeProfiles, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Reference: recipient ops user_key.")) {
		t.Fatalf("summary omitted consumer: %q", stderr.String())
	}
}

func TestProfilesRemoveYesAndConflictAvoidUnnecessaryWork(t *testing.T) {
	service := &adminSettingsFake{}
	prompter := &adminPrompterFake{}
	deps := Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"profiles", "remove", "work", "--replace-with", "other", "--force"}, testIO(&stdout, &stderr), deps); got != 2 || service.removeProfiles != 0 || prompter.confirmed {
		t.Fatalf("conflict exit=%d removals=%d confirmed=%t", got, service.removeProfiles, prompter.confirmed)
	}
	stdout.Reset()
	stderr.Reset()
	if got := Execute(context.Background(), []string{"profiles", "remove", "work", "--yes"}, testIO(&stdout, &stderr), deps); got != 0 || service.removeProfiles != 1 || prompter.confirmed {
		t.Fatalf("yes exit=%d removals=%d confirmed=%t", got, service.removeProfiles, prompter.confirmed)
	}
}

func TestProfilesInvalidDefaultAndPartialEnvironmentHaveNoPrompt(t *testing.T) {
	service := &adminSettingsFake{}
	prompter := &adminPrompterFake{answers: [][]byte{[]byte("secret")}}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"profiles", "add", "work", "--priority", "1"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 4 || prompter.reads != 0 {
		t.Fatalf("invalid default exit=%d prompts=%d", got, prompter.reads)
	}
	deps := Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter, lookupEnv: func(name string) (string, bool) { return "token", name == "PUSHOVER_TOKEN" }}
	if got := Execute(context.Background(), []string{"profiles", "list"}, testIO(&stdout, &stderr), deps); got != 2 {
		t.Fatalf("partial env exit=%d", got)
	}
}

func TestProfilesAddCapturesSafeDefaultsWipesTokenAndStartsOneScope(t *testing.T) {
	token := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	service := &adminSettingsFake{}
	prompter := &adminPrompterFake{answers: [][]byte{token}}
	spy := &contextSpy{}
	var stdout, stderr bytes.Buffer
	args := []string{"--json", "profiles", "add", "work", "--title", "Status", "--sound", "pushover", "--device", "phone", "--priority=-1", "--ttl", "1m"}
	got := Execute(context.Background(), args, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter, Operations: testOperations(spy)})
	if got != 0 || service.addProfiles != 1 || spy.calls != 1 {
		t.Fatalf("exit=%d adds=%d scopes=%d stderr=%q", got, service.addProfiles, spy.calls, stderr.String())
	}
	if input := service.profileInput; input.Name != "work" || input.Defaults.Title != "Status" || input.Defaults.Sound != "pushover" || input.Defaults.Priority != -1 || input.Defaults.TTL != time.Minute || len(input.Defaults.Devices) != 1 {
		t.Fatalf("profile input = %#v", input)
	}
	if service.profileTokenAtAdd != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || !allZero(token) || !allZero(service.profileInput.Token) {
		t.Fatal("profile token was not supplied then wiped")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"action":"added"`)) {
		t.Fatalf("result=%q", stdout.String())
	}
}

func TestProfilesShowUseAndSummaryFailureContracts(t *testing.T) {
	service := &adminSettingsFake{profiles: []settings.ProfileView{{Name: "work", Default: true, Defaults: config.MessageDefaults{Title: "Status", Priority: -1}}}}
	var stdout, stderr bytes.Buffer
	deps := Dependencies{Admin: AdminDependencies{Settings: service}}
	if got := Execute(context.Background(), []string{"--plain", "profiles", "show", "work"}, testIO(&stdout, &stderr), deps); got != 0 || stdout.String() != "work\ttrue\t-1\tStatus\t\n" {
		t.Fatalf("show exit=%d output=%q", got, stdout.String())
	}
	stdout.Reset()
	if got := Execute(context.Background(), []string{"--json", "profiles", "use", "work"}, testIO(&stdout, &stderr), deps); got != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"action":"selected"`)) {
		t.Fatalf("use exit=%d output=%q", got, stdout.String())
	}
	service = &adminSettingsFake{}
	streams := IO{In: bytes.NewReader(nil), Out: &stdout, Err: failingWriter{}, IsTerminal: func(uintptr) bool { return false }}
	if got := Execute(context.Background(), []string{"profiles", "remove", "work", "--yes"}, streams, Dependencies{Admin: AdminDependencies{Settings: service}}); got == 0 || service.removeProfiles != 0 {
		t.Fatalf("summary failure exit=%d removals=%d", got, service.removeProfiles)
	}
}

func TestProfilesRemoveWithoutPrompterAndJSONParseErrorDoNotMutate(t *testing.T) {
	service := &adminSettingsFake{}
	var stdout, stderr bytes.Buffer
	deps := Dependencies{Admin: AdminDependencies{Settings: service}}
	if got := Execute(context.Background(), []string{"profiles", "remove", "work"}, testIO(&stdout, &stderr), deps); got != 8 || service.removeProfiles != 0 {
		t.Fatalf("no prompter exit=%d removals=%d", got, service.removeProfiles)
	}
	stdout.Reset()
	stderr.Reset()
	if got := Execute(context.Background(), []string{"--json", "profiles", "show"}, testIO(&stdout, &stderr), deps); got != 2 || !bytes.Contains(stderr.Bytes(), []byte(`"command":"profiles show"`)) {
		t.Fatalf("parse exit=%d error=%q", got, stderr.String())
	}
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}

// adminSettingsFake is intentionally shared by command scenarios. It keeps
// tests hermetic while still exercising the public settings.Service boundary.
type adminSettingsFake struct {
	profiles   []settings.ProfileView
	recipients []settings.RecipientView
	token      []byte
	keys       map[string][]byte

	addProfiles, addRecipients                    int
	removeProfiles, removeRecipients              int
	selectedTokenCalls, userKeyCalls              int
	profileImpact                                 settings.RemovalImpact
	recipientImpact                               settings.RemovalImpact
	profileInput                                  settings.ProfileInput
	recipientInput                                settings.RecipientInput
	profileErr, recipientErr                      error
	addProfileErr, addRecipientErr                error
	removeProfileErr, removeRecipientErr          error
	profileTokenAtAdd, userKeyAtAdd, e2eeKeyAtAdd string
	warnings                                      []string
	calls                                         []string
	returnedTokens, returnedKeys                  [][]byte
}

func (f *adminSettingsFake) Bootstrap(context.Context, settings.SetupInput) (settings.SetupView, error) {
	return settings.SetupView{}, nil
}
func (f *adminSettingsFake) AddProfile(_ context.Context, input settings.ProfileInput) (settings.ProfileView, error) {
	f.calls = append(f.calls, "add-profile")
	f.addProfiles++
	f.profileInput = input
	f.profileTokenAtAdd = string(input.Token)
	if f.addProfileErr != nil {
		return settings.ProfileView{}, f.addProfileErr
	}
	return settings.ProfileView{Name: input.Name, Defaults: input.Defaults}, nil
}
func (f *adminSettingsFake) Profiles(context.Context) ([]settings.ProfileView, error) {
	f.calls = append(f.calls, "profiles")
	return append([]settings.ProfileView(nil), f.profiles...), nil
}
func (f *adminSettingsFake) Profile(_ context.Context, name string) (settings.ProfileView, error) {
	f.calls = append(f.calls, "profile")
	if f.profileErr != nil {
		return settings.ProfileView{}, f.profileErr
	}
	for _, profile := range f.profiles {
		if profile.Name == name {
			return profile, nil
		}
	}
	return settings.ProfileView{Name: name}, nil
}
func (f *adminSettingsFake) UseProfile(context.Context, string) error {
	f.calls = append(f.calls, "use-profile")
	return nil
}
func (f *adminSettingsFake) RemoveProfile(context.Context, string, settings.RemoveProfileOptions) ([]string, error) {
	f.calls = append(f.calls, "remove-profile")
	f.removeProfiles++
	return append([]string(nil), f.warnings...), f.removeProfileErr
}
func (f *adminSettingsFake) AddRecipient(_ context.Context, input settings.RecipientInput) (settings.RecipientView, error) {
	f.calls = append(f.calls, "add-recipient")
	f.addRecipients++
	f.recipientInput = input
	f.userKeyAtAdd, f.e2eeKeyAtAdd = string(input.UserKey), string(input.E2EEKey)
	if f.addRecipientErr != nil {
		return settings.RecipientView{}, f.addRecipientErr
	}
	return settings.RecipientView{Alias: input.Alias, Kind: input.Kind, Devices: input.Devices, Validated: input.Validated}, nil
}
func (f *adminSettingsFake) Recipients(context.Context) ([]settings.RecipientView, error) {
	f.calls = append(f.calls, "recipients")
	return append([]settings.RecipientView(nil), f.recipients...), nil
}
func (f *adminSettingsFake) Recipient(_ context.Context, alias string) (settings.RecipientView, error) {
	f.calls = append(f.calls, "recipient")
	if f.recipientErr != nil {
		return settings.RecipientView{}, f.recipientErr
	}
	for _, recipient := range f.recipients {
		if recipient.Alias == alias {
			return recipient, nil
		}
	}
	return settings.RecipientView{Alias: alias}, nil
}
func (f *adminSettingsFake) RemoveRecipient(context.Context, string) ([]string, error) {
	f.calls = append(f.calls, "remove-recipient")
	f.removeRecipients++
	return append([]string(nil), f.warnings...), f.removeRecipientErr
}
func (f *adminSettingsFake) PreviewRemoveProfile(context.Context, string, settings.RemoveProfileOptions) (settings.RemovalImpact, error) {
	f.calls = append(f.calls, "preview-profile")
	return f.profileImpact, nil
}
func (f *adminSettingsFake) PreviewRemoveRecipient(context.Context, string) (settings.RemovalImpact, error) {
	f.calls = append(f.calls, "preview-recipient")
	return f.recipientImpact, nil
}
func (f *adminSettingsFake) SelectedProfileToken(context.Context, string) ([]byte, error) {
	f.calls = append(f.calls, "selected-token")
	f.selectedTokenCalls++
	owned := append([]byte(nil), f.token...)
	f.returnedTokens = append(f.returnedTokens, owned)
	return owned, nil
}
func (f *adminSettingsFake) RecipientUserKey(_ context.Context, alias string) ([]byte, error) {
	f.calls = append(f.calls, "recipient-key")
	f.userKeyCalls++
	owned := append([]byte(nil), f.keys[alias]...)
	f.returnedKeys = append(f.returnedKeys, owned)
	return owned, nil
}

type contextSpy struct{ calls int }

func (s *contextSpy) WithDeadline(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	s.calls++
	return context.WithDeadline(parent, deadline)
}

func testOperations(spy *contextSpy) operation.Factory { return operation.Factory{Contexts: spy} }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

var _ settings.Service = (*adminSettingsFake)(nil)
var _ = config.MessageDefaults{}

type adminPrompterFake struct {
	answers   [][]byte
	reads     int
	confirmed bool
	err       error
	onConfirm func()
}

func (p *adminPrompterFake) Read(context.Context, Prompt) ([]byte, error) {
	p.reads++
	if p.err != nil {
		return nil, p.err
	}
	if len(p.answers) == 0 {
		return nil, errors.New("missing prompt answer")
	}
	answer := p.answers[0]
	p.answers = p.answers[1:]
	return answer, nil
}
func (p *adminPrompterFake) Confirm(context.Context, PromptSummary) error {
	p.confirmed = true
	if p.onConfirm != nil {
		p.onConfirm()
	}
	return p.err
}

type adminAPIFake struct {
	validations []pushover.RecipientValidation
	keys        []string
	tokens      []string
	devices     [][]string
	err         error
	errs        []error
}

func (*adminAPIFake) Send(context.Context, domain.TargetPayload) (pushover.SendResponse, error) {
	return pushover.SendResponse{}, errors.New("unexpected send")
}
func (a *adminAPIFake) ValidateRecipient(_ context.Context, token, key string, devices []string) (pushover.RecipientValidation, error) {
	a.tokens, a.keys = append(a.tokens, token), append(a.keys, key)
	a.devices = append(a.devices, append([]string(nil), devices...))
	if a.err != nil {
		return pushover.RecipientValidation{}, a.err
	}
	if len(a.errs) > 0 {
		err := a.errs[0]
		a.errs = a.errs[1:]
		if err != nil {
			return pushover.RecipientValidation{}, err
		}
	}
	value := pushover.RecipientValidation{Valid: true}
	if len(a.validations) > 0 {
		value, a.validations = a.validations[0], a.validations[1:]
	}
	return value, nil
}
func (*adminAPIFake) Sounds(context.Context, string) (pushover.SoundsResponse, error) {
	return pushover.SoundsResponse{}, errors.New("unexpected sounds")
}
func (*adminAPIFake) Limits(context.Context, string) (pushover.LimitsResponse, error) {
	return pushover.LimitsResponse{}, errors.New("unexpected limits")
}
