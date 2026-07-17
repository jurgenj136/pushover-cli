package cmd

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

func TestExecuteRecipientsListPlain(t *testing.T) {
	service := &adminSettingsFake{recipients: []settings.RecipientView{{Alias: "ops", Kind: domain.KindUser, Validated: true, Devices: []string{"phone"}}}}
	deps := Dependencies{Admin: AdminDependencies{Settings: service}}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"--plain", "recipients", "list"}, testIO(&stdout, &stderr), deps); got != 0 {
		t.Fatalf("exit = %d, stderr = %q", got, stderr.String())
	}
	if got, want := stdout.String(), "ops\tuser\ttrue\tphone\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRecipientsAddOfflineSkipsAPIAndWipesAnswers(t *testing.T) {
	userKey := []byte("111111111111111111111111111111")
	e2eeKey := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	service := &adminSettingsFake{}
	api := &adminAPIFake{}
	prompter := &adminPrompterFake{answers: [][]byte{userKey, e2eeKey}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"recipients", "add", "ops", "--offline", "--kind", "team-group"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
	if got != 0 || service.addRecipients != 1 || len(api.keys) != 0 {
		t.Fatalf("exit=%d adds=%d api=%d stderr=%q", got, service.addRecipients, len(api.keys), stderr.String())
	}
	for _, value := range append(userKey, e2eeKey...) {
		if value != 0 {
			t.Fatalf("prompt secret was not wiped: %q", value)
		}
	}
}

func TestRecipientsAddAllowsEmptyOptionalE2EEKey(t *testing.T) {
	userKey := []byte("111111111111111111111111111111")
	service := &adminSettingsFake{}
	prompter := &adminPrompterFake{answers: [][]byte{userKey, {}}}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"recipients", "add", "ops", "--offline"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 0 || service.addRecipients != 1 || len(service.recipientInput.E2EEKey) != 0 || !allZero(userKey) {
		t.Fatalf("exit=%d adds=%d e2ee=%d", got, service.addRecipients, len(service.recipientInput.E2EEKey))
	}
}

func TestRecipientsAddPreservesExplicitAndUnknownKindsOffline(t *testing.T) {
	for _, test := range []struct {
		kind string
		want domain.TargetKind
	}{
		{"", domain.KindUnknown}, {"user", domain.KindUser}, {"group", domain.KindGroup}, {"team-group", domain.KindTeamGroup},
	} {
		service := &adminSettingsFake{}
		prompter := &adminPrompterFake{answers: [][]byte{[]byte("111111111111111111111111111111"), nil}}
		args := []string{"recipients", "add", "ops", "--offline"}
		if test.kind != "" {
			args = append(args, "--kind", test.kind)
		}
		var stdout, stderr bytes.Buffer
		if got := Execute(context.Background(), args, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 0 || service.recipientInput.Kind != test.want || service.recipientInput.Validated {
			t.Fatalf("kind=%q exit=%d input=%#v", test.kind, got, service.recipientInput)
		}
	}
}

func TestRecipientsAddOnlineInvalidResponseDoesNotMutate(t *testing.T) {
	service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	api := &adminAPIFake{validations: []pushover.RecipientValidation{{Valid: false}}}
	prompter := &adminPrompterFake{answers: [][]byte{[]byte("111111111111111111111111111111"), nil}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"recipients", "add", "ops"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
	if got != 4 || service.addRecipients != 0 || len(api.keys) != 1 {
		t.Fatalf("exit=%d adds=%d api=%d stderr=%q", got, service.addRecipients, len(api.keys), stderr.String())
	}
}

func TestRecipientsAddOnlineAPIErrorDoesNotMutate(t *testing.T) {
	service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	api := &adminAPIFake{err: errors.New("network down")}
	prompter := &adminPrompterFake{answers: [][]byte{[]byte("111111111111111111111111111111"), nil}}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"recipients", "add", "ops"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter}); got != 1 || service.addRecipients != 0 {
		t.Fatalf("exit=%d adds=%d", got, service.addRecipients)
	}
}

func TestRecipientsAddOnlinePersistsValidatedInputAndUsesOneScope(t *testing.T) {
	userKey := []byte("111111111111111111111111111111")
	e2eeKey := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	api := &adminAPIFake{validations: []pushover.RecipientValidation{{Valid: true}}}
	prompter := &adminPrompterFake{answers: [][]byte{userKey, e2eeKey}}
	spy := &contextSpy{}
	var stdout, stderr bytes.Buffer
	args := []string{"recipients", "add", "ops", "--kind", "team-group", "--device", "phone"}
	got := Execute(context.Background(), args, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter, Operations: testOperations(spy)})
	if got != 0 || service.addRecipients != 1 || !service.recipientInput.Validated || service.recipientInput.Kind != domain.KindTeamGroup || spy.calls != 2 {
		t.Fatalf("exit=%d adds=%d validated=%t kind=%q scope-calls=%d", got, service.addRecipients, service.recipientInput.Validated, service.recipientInput.Kind, spy.calls)
	}
	if service.userKeyAtAdd != "111111111111111111111111111111" || service.e2eeKeyAtAdd == "" || !allZero(userKey) || !allZero(e2eeKey) || !allZero(service.recipientInput.UserKey) || !allZero(service.recipientInput.E2EEKey) {
		t.Fatal("recipient secrets were not supplied then wiped")
	}
	if got := service.recipientInput.Devices; !reflect.DeepEqual(got, []string{"phone"}) {
		t.Fatalf("devices=%v", got)
	}
}

func TestRecipientsAddRejectsUnsafeKindAndGroupDevicesBeforePrompt(t *testing.T) {
	for _, args := range [][]string{
		{"recipients", "add", "bad\nname", "--offline"},
		{"recipients", "add", "ops", "--kind", "invalid", "--offline"},
		{"recipients", "add", "ops", "--kind", "group", "--device", "phone", "--offline"},
	} {
		service := &adminSettingsFake{}
		prompter := &adminPrompterFake{answers: [][]byte{[]byte("key")}}
		var stdout, stderr bytes.Buffer
		if got := Execute(context.Background(), args, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 4 || prompter.reads != 0 || service.addRecipients != 0 {
			t.Fatalf("args=%q exit=%d prompts=%d adds=%d", args, got, prompter.reads, service.addRecipients)
		}
	}
}

func TestRecipientsAddRejectsMalformedDeviceBeforePromptOrNetwork(t *testing.T) {
	service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	api := &adminAPIFake{}
	prompter := &adminPrompterFake{answers: [][]byte{[]byte("unused")}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"recipients", "add", "ops", "--device", "bad device"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
	if got != 4 || prompter.reads != 0 || len(api.keys) != 0 || service.addRecipients != 0 {
		t.Fatalf("exit=%d prompts=%d api=%d adds=%d", got, prompter.reads, len(api.keys), service.addRecipients)
	}
}

func TestRecipientsAddRejectsMalformedPromptSecretsBeforeNetworkAndWipes(t *testing.T) {
	validUser := "111111111111111111111111111111"
	validE2EE := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name string
		user []byte
		e2ee []byte
	}{
		{name: "user key", user: []byte("short"), e2ee: []byte(validE2EE)},
		{name: "e2ee key", user: []byte(validUser), e2ee: []byte("not-hex")},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
			api := &adminAPIFake{}
			prompter := &adminPrompterFake{answers: [][]byte{test.user, test.e2ee}}
			var stdout, stderr bytes.Buffer
			got := Execute(context.Background(), []string{"recipients", "add", "ops"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Prompter: prompter})
			if got != 4 || len(api.keys) != 0 || service.addRecipients != 0 || !allZero(test.user) || !allZero(test.e2ee) {
				t.Fatalf("exit=%d api=%d adds=%d user-wiped=%t e2ee-wiped=%t", got, len(api.keys), service.addRecipients, allZero(test.user), allZero(test.e2ee))
			}
		})
	}
}

func TestRecipientsValidateUsesOneTokenAndPreservesAliasOrder(t *testing.T) {
	service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), keys: map[string][]byte{
		"first":  []byte("111111111111111111111111111111"),
		"second": []byte("222222222222222222222222222222"),
	}, recipients: []settings.RecipientView{{Alias: "first", Devices: []string{"phone"}}, {Alias: "second", Devices: []string{"watch"}}}}
	api := &adminAPIFake{validations: []pushover.RecipientValidation{{Valid: true, Devices: []string{"phone"}}, {Valid: true, Devices: []string{"watch"}}}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--json", "recipients", "validate", "first", "second"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}})
	if got != 0 || service.selectedTokenCalls != 1 || service.userKeyCalls != 2 {
		t.Fatalf("exit=%d tokens=%d keys=%d stderr=%q", got, service.selectedTokenCalls, service.userKeyCalls, stderr.String())
	}
	if got, want := api.keys, []string{"111111111111111111111111111111", "222222222222222222222222222222"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("API key order = %#v, want %#v", got, want)
	}
	if got, want := api.devices, [][]string{{"phone"}, {"watch"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("API devices = %#v, want %#v", got, want)
	}
	if first, second := bytes.Index(stdout.Bytes(), []byte(`"alias":"first"`)), bytes.Index(stdout.Bytes(), []byte(`"alias":"second"`)); first < 0 || second < first {
		t.Fatalf("result order = %q", stdout.String())
	}
}

func TestRecipientsValidateDuplicateAliasesRejectsBeforeCredentialRead(t *testing.T) {
	service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	api := &adminAPIFake{}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"recipients", "validate", "ops", "ops"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}})
	if got != 2 || service.selectedTokenCalls != 0 || service.userKeyCalls != 0 || len(api.keys) != 0 {
		t.Fatalf("exit=%d tokens=%d keys=%d api=%d", got, service.selectedTokenCalls, service.userKeyCalls, len(api.keys))
	}
}

func TestRecipientsValidateRowsKeepGoingAndWipeCredentials(t *testing.T) {
	service := &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), keys: map[string][]byte{
		"first": []byte("111111111111111111111111111111"), "second": []byte("222222222222222222222222222222"),
	}, recipients: []settings.RecipientView{{Alias: "first"}, {Alias: "second"}}}
	api := &adminAPIFake{errs: []error{errors.New("bad first"), nil}, validations: []pushover.RecipientValidation{{Valid: true}}}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--json", "recipients", "validate", "first", "second"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}})
	if got != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"error_code":"internal"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"alias":"second","valid":true`)) {
		t.Fatalf("exit=%d output=%q", got, stdout.String())
	}
	for _, value := range append(service.returnedTokens, service.returnedKeys...) {
		if !allZero(value) {
			t.Fatal("returned credential was not wiped")
		}
	}
}

func TestRecipientsPartialEnvironmentRejectsBeforeScopeOrPrompt(t *testing.T) {
	service := &adminSettingsFake{}
	prompter := &adminPrompterFake{answers: [][]byte{[]byte("key")}}
	spy := &contextSpy{}
	var stdout, stderr bytes.Buffer
	deps := Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter, Operations: testOperations(spy), lookupEnv: func(name string) (string, bool) { return "token", name == "PUSHOVER_TOKEN" }}
	if got := Execute(context.Background(), []string{"recipients", "add", "ops", "--offline"}, testIO(&stdout, &stderr), deps); got != 2 || spy.calls != 0 || prompter.reads != 0 {
		t.Fatalf("exit=%d scope=%d prompts=%d", got, spy.calls, prompter.reads)
	}
}

func TestRecipientsAdminCommandsIgnoreCompleteEphemeralEnvironment(t *testing.T) {
	storedToken := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	service := &adminSettingsFake{token: []byte(storedToken), keys: map[string][]byte{"ops": []byte("111111111111111111111111111111")}, recipients: []settings.RecipientView{{Alias: "ops"}}}
	api := &adminAPIFake{}
	var stdout, stderr bytes.Buffer
	lookup := func(name string) (string, bool) {
		switch name {
		case "PUSHOVER_TOKEN":
			return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", true
		case "PUSHOVER_USER_KEY":
			return "cccccccccccccccccccccccccccccc", true
		default:
			return "", false
		}
	}
	if got := Execute(context.Background(), []string{"recipients", "validate", "ops"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, lookupEnv: lookup}); got != 0 || len(api.tokens) != 1 || api.tokens[0] != storedToken {
		t.Fatalf("exit=%d tokens=%q", got, api.tokens)
	}
}

func TestRecipientsPlainMutationAndDeadlineAreSideEffectSafe(t *testing.T) {
	service := &adminSettingsFake{}
	prompter := &adminPrompterFake{answers: [][]byte{[]byte("key")}}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"--plain", "recipients", "add", "ops", "--offline"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter}); got != 2 || prompter.reads != 0 || service.addRecipients != 0 {
		t.Fatalf("plain exit=%d prompts=%d adds=%d", got, prompter.reads, service.addRecipients)
	}
	service = &adminSettingsFake{token: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), keys: map[string][]byte{"ops": []byte("111111111111111111111111111111")}, recipients: []settings.RecipientView{{Alias: "ops"}}}
	api := &adminAPIFake{err: context.DeadlineExceeded}
	stdout.Reset()
	stderr.Reset()
	if got := Execute(context.Background(), []string{"recipients", "validate", "ops"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}}); got != 6 || stdout.Len() != 0 {
		t.Fatalf("deadline exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
}

func TestRecipientsRemoveConfirmationAndWarnings(t *testing.T) {
	service := &adminSettingsFake{warnings: []string{"configuration changed; remove obsolete secret manually"}}
	prompter := &adminPrompterFake{}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--json", "recipients", "remove", "ops"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}, Prompter: prompter})
	if got != 0 || !prompter.confirmed || service.removeRecipients != 1 || !bytes.Contains(stdout.Bytes(), []byte(`"warnings":["configuration changed; remove obsolete secret manually"]`)) || !bytes.Contains(stderr.Bytes(), []byte("warning:")) {
		t.Fatalf("exit=%d confirmed=%t removes=%d stdout=%q stderr=%q", got, prompter.confirmed, service.removeRecipients, stdout.String(), stderr.String())
	}
}
