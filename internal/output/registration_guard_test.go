package output

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type outputRegistration struct {
	command        string
	result         CommandResult
	knownPairing   bool
	hasHumanRows   bool
	plainSupported bool
}

func TestOutputResultRegistration(t *testing.T) {
	for _, registration := range outputRegistrations() {
		if registration.result == nil {
			continue
		}
		t.Run(registration.command, func(t *testing.T) {
			pointer := resultPointer(registration.result)
			normalized := normalizeResult(pointer)
			if got, want := reflect.TypeOf(normalized), reflect.TypeOf(registration.result); got != want || !reflect.DeepEqual(normalized, registration.result) {
				t.Fatalf("normalizeResult(%T) = %#v, want %T %#v", pointer, normalized, registration.result, registration.result)
			}
			if got := knownPairing(registration.command, normalized); got != registration.knownPairing {
				t.Fatalf("knownPairing(%q, %T) = %t, want %t", registration.command, normalized, got, registration.knownPairing)
			}
			if payload := jsonPayload(pointer); payload == nil {
				t.Fatalf("jsonPayload(%T) = nil", pointer)
			}
			assertResultRenders(t, "json", JSONRenderer{}, pointer)
			assertResultRenders(t, "human", HumanRenderer{}, pointer)

			rows := humanRows(normalized)
			if got := rows != nil; got != registration.hasHumanRows {
				t.Fatalf("humanRows(%T) returned rows=%t, want %t", normalized, got, registration.hasHumanRows)
			}

			var plain bytes.Buffer
			err := (PlainRenderer{}).Result(&plain, pointer)
			if registration.plainSupported {
				if err != nil || plain.Len() == 0 {
					t.Fatalf("plain result: err=%v output=%q", err, plain.String())
				}
				return
			}
			if err == nil || plain.Len() != 0 {
				t.Fatalf("plain result unexpectedly supported: err=%v output=%q", err, plain.String())
			}
		})
	}
}

func TestKnownCommandsRenderErrorsInEveryMode(t *testing.T) {
	seen := make(map[string]struct{})
	for _, registration := range outputRegistrations() {
		if _, duplicate := seen[registration.command]; duplicate {
			t.Fatalf("duplicate command registration %q", registration.command)
		}
		seen[registration.command] = struct{}{}
		t.Run(registration.command, func(t *testing.T) {
			if !knownCommand(registration.command) {
				t.Fatalf("knownCommand(%q) = false", registration.command)
			}
			for _, renderer := range []struct {
				name     string
				renderer Renderer
			}{
				{name: "human", renderer: HumanRenderer{}},
				{name: "json", renderer: JSONRenderer{}},
				{name: "plain", renderer: PlainRenderer{}},
			} {
				t.Run(renderer.name, func(t *testing.T) {
					var rendered bytes.Buffer
					err := renderer.renderer.Error(&rendered, registration.command, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"))
					if err != nil || rendered.Len() == 0 {
						t.Fatalf("error render: err=%v output=%q", err, rendered.String())
					}
				})
			}
		})
	}
}

func assertResultRenders(t *testing.T, name string, renderer Renderer, result CommandResult) {
	t.Helper()
	var rendered bytes.Buffer
	if err := renderer.Result(&rendered, result); err != nil || rendered.Len() == 0 {
		t.Fatalf("%s result: err=%v output=%q", name, err, rendered.String())
	}
}

func resultPointer(result CommandResult) CommandResult {
	value := reflect.ValueOf(result)
	pointer := reflect.New(value.Type())
	pointer.Elem().Set(value)
	return pointer.Interface().(CommandResult)
}

func outputRegistrations() []outputRegistration {
	return []outputRegistration{
		{command: "send", result: SendResult{Meta: Meta{Command: "send", Status: "success"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Sent}}}}, plainSupported: true},
		{command: "setup", result: mutationResult("setup"), knownPairing: true, hasHumanRows: true},

		{command: "profiles"},
		{command: "profiles list", result: ProfilesResult{Meta: Meta{Command: "profiles list", Status: "success"}, Rows: []ProfileRow{{Name: "default"}}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "profiles show", result: ProfilesResult{Meta: Meta{Command: "profiles show", Status: "success"}, Rows: []ProfileRow{{Name: "default"}}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "profiles add", result: mutationResult("profiles add"), knownPairing: true, hasHumanRows: true},
		{command: "profiles use", result: mutationResult("profiles use"), knownPairing: true, hasHumanRows: true},
		{command: "profiles remove", result: mutationResult("profiles remove"), knownPairing: true, hasHumanRows: true},

		{command: "recipients"},
		{command: "recipients list", result: RecipientsResult{Meta: Meta{Command: "recipients list", Status: "success"}, Rows: []RecipientRow{{Alias: "ops"}}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "recipients show", result: RecipientsResult{Meta: Meta{Command: "recipients show", Status: "success"}, Rows: []RecipientRow{{Alias: "ops"}}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "recipients validate", result: ValidationsResult{Meta: Meta{Command: "recipients validate", Status: "success"}, Rows: []ValidationRow{{Alias: "ops"}}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "recipients add", result: mutationResult("recipients add"), knownPairing: true, hasHumanRows: true},
		{command: "recipients remove", result: mutationResult("recipients remove"), knownPairing: true, hasHumanRows: true},

		{command: "sounds", result: SoundsResult{Meta: Meta{Command: "sounds", Status: "success"}, Rows: []SoundRow{{Name: "pushover"}}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "limits", result: LimitsResult{Meta: Meta{Command: "limits", Status: "success"}}, knownPairing: true, hasHumanRows: true, plainSupported: true},

		{command: "auth"},
		{command: "auth status", result: AuthStatusResult{Meta: Meta{Command: "auth status", Status: "success"}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "auth backend", result: BackendResult{Meta: Meta{Command: "auth backend", Status: "success"}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "auth backend set", result: mutationResult("auth backend set"), knownPairing: true, hasHumanRows: true},

		{command: "info", result: InfoResult{Meta: Meta{Command: "info", Status: "success"}, Features: []string{"age-file"}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "version", result: VersionResult{Meta: Meta{Command: "version", Status: "success"}}, knownPairing: true, hasHumanRows: true, plainSupported: true},
		{command: "completion", result: CompletionResult{Meta: Meta{Command: "completion", Status: "success"}, Shell: "bash", Script: "complete\n"}, knownPairing: true},
	}
}

func mutationResult(command string) MutationResult {
	return MutationResult{Meta: Meta{Command: command, Status: "success"}, Resource: "profile", Name: "default", Action: "updated"}
}
