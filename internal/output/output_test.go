package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestJSONSendTypedEnvelopeAndCounts(t *testing.T) {
	q := 17
	r := SendResult{Meta: Meta{Command: "send", Status: "partial"}, Batch: domain.BatchResult{
		Command: "send", Results: []domain.TargetResult{
			{Alias: "a", Outcome: domain.Sent, Priority: 1, RequestID: "r", QuotaRemaining: &q},
			{Alias: "b", Outcome: domain.Failed, Priority: 0, Error: &domain.ErrorDetail{Code: "x", Category: "network", Message: "bad", Retryable: true}},
			{Alias: "c", Outcome: domain.Ambiguous, Priority: 0, ReceiptID: "rc", Error: &domain.ErrorDetail{Code: "y", Category: "network", Message: "maybe", Ambiguous: true}},
		},
	}}
	var out bytes.Buffer
	if err := (JSONRenderer{}).Result(&out, r); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("missing final newline: %q", out.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["schema_version"] != float64(1) || doc["command"] != "send" || doc["status"] != "partial" {
		t.Fatalf("bad envelope: %#v", doc)
	}
	result := doc["result"].(map[string]any)
	counts := result["counts"].(map[string]any)
	for _, key := range []string{"planned", "sent", "failed", "ambiguous", "skipped"} {
		if _, ok := counts[key]; !ok {
			t.Fatalf("missing count %s", key)
		}
	}
	if counts["sent"] != float64(1) || counts["failed"] != float64(1) || counts["ambiguous"] != float64(1) {
		t.Fatalf("wrong counts: %#v", counts)
	}
	if got := result["targets"].([]any)[1].(map[string]any)["error"].(map[string]any)["retryable"]; got != false {
		t.Fatalf("send retryable must normalize false, got %v", got)
	}
}

func TestTSVEscapesControlsAndRejectsUnsupported(t *testing.T) {
	r := ProfilesResult{Meta: Meta{Command: "profiles list", Status: "success"}, Rows: []ProfileRow{{Name: "n\t", Title: "x\r\ny", Sound: "s"}}}
	var out bytes.Buffer
	if err := (PlainRenderer{}).Result(&out, r); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "n\\t\tfalse\t0\tx\\r\\ny\ts\n" {
		t.Fatalf("TSV=%q", got)
	}
	if err := (PlainRenderer{}).Result(&out, InfoResult{Meta: Meta{Command: "setup", Status: "success"}}); err == nil {
		t.Fatal("expected unsupported plain command")
	}
}

func TestJSONErrorOneDocument(t *testing.T) {
	var out bytes.Buffer
	err := apperror.New("bad", apperror.CategoryUsage, "oops\x1b[31m")
	if got := (JSONRenderer{}).Error(&out, "profiles list", err); got != nil {
		t.Fatal(got)
	}
	if !strings.HasSuffix(out.String(), "\n") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("bad error output: %q", out.String())
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("control leaked: %q", out.String())
	}
}

func TestJSONAllResultTypesAndArrays(t *testing.T) {
	results := []CommandResult{
		ProfilesResult{Meta: Meta{"profiles list", "success"}}, RecipientsResult{Meta: Meta{"recipients list", "success"}},
		ValidationsResult{Meta: Meta{"recipients validate", "success"}}, SoundsResult{Meta: Meta{"sounds", "success"}},
		LimitsResult{Meta: Meta{"limits", "success"}}, AuthStatusResult{Meta: Meta{"auth status", "success"}},
		BackendResult{Meta: Meta{"auth backend", "success"}}, InfoResult{Meta: Meta{"info", "success"}},
		VersionResult{Meta: Meta{"version", "success"}}, MutationResult{Meta: Meta{"profiles add", "success"}}, CompletionResult{Meta: Meta{"completion", "success"}},
	}
	for _, result := range results {
		var out bytes.Buffer
		if err := (JSONRenderer{}).Result(&out, result); err != nil {
			t.Fatalf("%s: %v", result.CommandName(), err)
		}
		if strings.Contains(out.String(), `"rows":null`) || strings.Contains(out.String(), `"warnings":null`) {
			t.Fatalf("nil array in %s: %s", result.CommandName(), out.String())
		}
	}
}

func TestStatusPairingWritesNoPartialOutput(t *testing.T) {
	var out bytes.Buffer
	r := ProfilesResult{Meta: Meta{"profiles list", "failed"}, Rows: []ProfileRow{{Name: "secret"}}}
	if err := (JSONRenderer{}).Result(&out, r); err == nil || out.Len() != 0 {
		t.Fatalf("expected typed mismatch with no output: err=%v out=%q", err, out.String())
	}
	bad := SendResult{Meta: Meta{"profiles list", "success"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "a", Outcome: domain.Sent}}}}
	if err := (PlainRenderer{}).Result(&out, bad); err == nil || out.Len() != 0 {
		t.Fatalf("expected command mismatch with no output: err=%v out=%q", err, out.String())
	}
}

func TestHumanCompletionAndColorSafety(t *testing.T) {
	var out bytes.Buffer
	r := HumanRenderer{Options: RenderOptions{Color: true, TTY: true}}
	if err := r.Result(&out, CompletionResult{Meta: Meta{"completion", "success"}, Shell: "bash", Script: "complete\n"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "complete\n" {
		t.Fatalf("completion must be raw: %q", out.String())
	}
	out.Reset()
	if err := r.Result(&out, SendResult{Meta: Meta{"send", "success"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "a\x1b[31m", Outcome: domain.Sent}}}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b[31m") || !strings.Contains(out.String(), "\x1b[32m") {
		t.Fatalf("unsafe/color output: %q", out.String())
	}
}

func TestPlainEligibility(t *testing.T) {
	eligible := []CommandResult{
		SendResult{Meta: Meta{"send", "dry_run"}, Batch: domain.BatchResult{Command: "send", DryRun: true, Results: []domain.TargetResult{{Alias: "a", Outcome: domain.Planned}}}},
		ProfilesResult{Meta: Meta{"profiles show", "success"}}, RecipientsResult{Meta: Meta{"recipients list", "success"}}, ValidationsResult{Meta: Meta{"recipients validate", "success"}}, SoundsResult{Meta: Meta{"sounds", "success"}}, LimitsResult{Meta: Meta{"limits", "success"}}, AuthStatusResult{Meta: Meta{"auth status", "success"}}, BackendResult{Meta: Meta{"auth backend", "success"}}, InfoResult{Meta: Meta{"info", "success"}}, VersionResult{Meta: Meta{"version", "success"}},
	}
	for _, result := range eligible {
		var out bytes.Buffer
		if err := (PlainRenderer{}).Result(&out, result); err != nil {
			t.Fatalf("%s: %v", result.CommandName(), err)
		}
	}
	for _, result := range []CommandResult{MutationResult{Meta: Meta{"profiles add", "success"}}, CompletionResult{Meta: Meta{"completion", "success"}}, InfoResult{Meta: Meta{"setup", "success"}}} {
		var out bytes.Buffer
		if err := (PlainRenderer{}).Result(&out, result); err == nil || out.Len() != 0 {
			t.Fatalf("expected plain rejection for %s", result.CommandName())
		}
	}
}
