package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestGolden(t *testing.T) {
	results := []CommandResult{
		SendResult{Meta: Meta{"send", "success"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Sent, Priority: 0, RequestID: "req-1"}}}},
		SendResult{Meta: Meta{"send", "partial"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Sent, Priority: 0}, {Alias: "backup", Outcome: domain.Failed, Priority: 1, Error: &domain.ErrorDetail{Code: "network", Category: "network", Message: "request failed"}}}}},
		SendResult{Meta: Meta{"send", "failed"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Failed, Priority: 0, Error: &domain.ErrorDetail{Code: "validation", Category: "validation", Message: "invalid target"}}}}},
		SendResult{Meta: Meta{"send", "ambiguous"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Ambiguous, Priority: 0, Error: &domain.ErrorDetail{Code: "timeout", Category: "network", Message: "unknown", Ambiguous: true}}}}},
		SendResult{Meta: Meta{"send", "interrupted"}, Batch: domain.BatchResult{Command: "send", Interrupted: true, Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Skipped, Priority: 0}}}},
		SendResult{Meta: Meta{"send", "dry_run"}, Batch: domain.BatchResult{Command: "send", DryRun: true, Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Planned, Priority: 0}}}},
		ProfilesResult{Meta: Meta{"profiles list", "success"}, Rows: []ProfileRow{{Name: "default", Default: true, Priority: 0, Title: "Ops", Sound: "pushover"}}},
		RecipientsResult{Meta: Meta{"recipients list", "success"}, Rows: []RecipientRow{{Alias: "ops", Kind: domain.KindUser, Validated: true, Devices: []string{"phone"}}}},
		ValidationsResult{Meta: Meta{"recipients validate", "success"}, Rows: []ValidationRow{{Alias: "ops", Valid: true, Devices: []string{}, Licenses: []string{}, ErrorCode: ""}}},
		SoundsResult{Meta: Meta{"sounds", "success"}, Rows: []SoundRow{{Name: "pushover", Description: "Default sound"}}},
		LimitsResult{Meta: Meta{"limits", "success"}, Limit: 10000, Remaining: 9999, Reset: 1700000000},
		AuthStatusResult{Meta: Meta{"auth status", "success"}, Profile: "default", Backend: "native", Configured: true, TokenResolves: true},
		BackendResult{Meta: Meta{"auth backend", "success"}, Backend: "native"},
		InfoResult{Meta: Meta{"info", "success"}, ConfigPath: "/config/pushover.yaml", Profile: "default", Backend: "native", Version: "1.0.0", Commit: "abc", BuildDate: "2026-07-17"},
		VersionResult{Meta: Meta{"version", "success"}, Version: "1.0.0", Commit: "abc", BuildDate: "2026-07-17"},
		MutationResult{Meta: Meta{"profiles add", "success"}, Resource: "profile", Name: "default", Action: "added", Warnings: []string{}},
		CompletionResult{Meta: Meta{"completion", "success"}, Shell: "bash", Script: "complete -W 'send info' pushover\n"},
	}
	for _, result := range results {
		goldenResult(t, result)
	}
	for _, command := range []string{"send", "profiles list", "recipients list", "recipients validate", "sounds", "limits", "auth status", "auth backend", "info", "version", "profiles add", "completion"} {
		goldenError(t, command)
	}
	goldenPlain(t, ProfilesResult{Meta: Meta{"profiles list", "success"}, Rows: []ProfileRow{{Name: "n\t", Title: "x\r\n", Sound: "s"}}})
	goldenHuman(t, CompletionResult{Meta: Meta{"completion", "success"}, Shell: "bash", Script: "complete\n"}, "completion-human")
	goldenHuman(t, SendResult{Meta: Meta{"send", "success"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Sent, Priority: 0}}}}, "send-human-color")
}

func goldenResult(t *testing.T, result CommandResult) {
	t.Helper()
	var got bytes.Buffer
	if err := (JSONRenderer{}).Result(&got, result); err != nil {
		t.Fatal(err)
	}
	golden(t, strings.ReplaceAll(result.CommandName(), " ", "-")+"-json-"+result.StatusName(), got.Bytes())
}
func goldenError(t *testing.T, command string) {
	t.Helper()
	var got bytes.Buffer
	if err := (JSONRenderer{}).Error(&got, command, apperror.New("example", apperror.CategoryUsage, "invalid command")); err != nil {
		t.Fatal(err)
	}
	golden(t, strings.ReplaceAll(command, " ", "-")+"-json-error", got.Bytes())
}
func goldenPlain(t *testing.T, result CommandResult) {
	t.Helper()
	var got bytes.Buffer
	if err := (PlainRenderer{}).Result(&got, result); err != nil {
		t.Fatal(err)
	}
	golden(t, "profiles-list-plain-controls", got.Bytes())
}
func goldenHuman(t *testing.T, result CommandResult, name string) {
	t.Helper()
	var got bytes.Buffer
	if err := (HumanRenderer{Options: RenderOptions{Color: true, TTY: true}}).Result(&got, result); err != nil {
		t.Fatal(err)
	}
	golden(t, name, got.Bytes())
}
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s mismatch\nwant %q\ngot %q", name, want, got)
	}
}
