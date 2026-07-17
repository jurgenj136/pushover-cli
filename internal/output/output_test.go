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

func TestErrorRenderersAcceptIncompleteParentCommandPaths(t *testing.T) {
	for _, command := range []string{"profiles", "recipients"} {
		for name, renderer := range map[string]Renderer{
			"human": HumanRenderer{},
			"json":  JSONRenderer{},
			"plain": PlainRenderer{},
		} {
			t.Run(name+"/"+command, func(t *testing.T) {
				var rendered bytes.Buffer
				if err := renderer.Error(&rendered, command, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line")); err != nil {
					t.Fatalf("render parent command error: %v", err)
				}
				if rendered.Len() == 0 {
					t.Fatal("renderer produced no error output")
				}
			})
		}
	}
}

func TestJSONAllResultTypesAndArrays(t *testing.T) {
	results := []CommandResult{
		ProfilesResult{Meta: Meta{"profiles list", "success"}}, RecipientsResult{Meta: Meta{"recipients list", "success"}},
		ValidationsResult{Meta: Meta{"recipients validate", "success"}}, SoundsResult{Meta: Meta{"sounds", "success"}},
		LimitsResult{Meta: Meta{"limits", "success"}}, AuthStatusResult{Meta: Meta{"auth status", "success"}},
		BackendResult{Meta: Meta{"auth backend", "success"}}, InfoResult{Meta: Meta{"info", "success"}, Features: []string{"age-file"}},
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

func TestInfoFeaturesAreStableAcrossRenderers(t *testing.T) {
	// Features are intentionally strict output data: non-empty lowercase
	// identifiers, sorted lexicographically, and unique. This keeps all three
	// formats deterministic and makes the TSV comma-list unambiguous.
	result := InfoResult{
		Meta:       Meta{"info", "success"},
		ConfigPath: "/config/pushover.yaml",
		Profile:    "default",
		Backend:    "native",
		Version:    "1.0.0",
		Commit:     "abc",
		BuildDate:  "2026-07-17",
		Features:   []string{"age-file", "e2ee", "native-secrets"},
	}
	var jsonOut bytes.Buffer
	if err := (JSONRenderer{}).Result(&jsonOut, result); err != nil {
		t.Fatal(err)
	}
	if got := jsonOut.String(); !strings.Contains(got, `"features":["age-file","e2ee","native-secrets"]`) {
		t.Fatalf("JSON features missing or unstable: %q", got)
	}
	var humanOut bytes.Buffer
	if err := (HumanRenderer{}).Result(&humanOut, result); err != nil {
		t.Fatal(err)
	}
	if got, want := humanOut.String(), "config_path=/config/pushover.yaml profile=default backend=native version=1.0.0 commit=abc build_date=2026-07-17 features=age-file,e2ee,native-secrets\n"; got != want {
		t.Fatalf("human info = %q, want %q", got, want)
	}
	var plainOut bytes.Buffer
	if err := (PlainRenderer{}).Result(&plainOut, result); err != nil {
		t.Fatal(err)
	}
	if got, want := plainOut.String(), "/config/pushover.yaml\tdefault\tnative\t1.0.0\tabc\t2026-07-17\tage-file,e2ee,native-secrets\n"; got != want {
		t.Fatalf("plain info = %q, want %q", got, want)
	}
}

func TestInfoFeaturesRejectInvalidValues(t *testing.T) {
	base := InfoResult{Meta: Meta{"info", "success"}, Features: []string{"age-file", "e2ee"}}
	cases := map[string][]string{
		"nil":       nil,
		"empty":     {},
		"uppercase": {"Age-file"},
		"control":   {"age\nfile"},
		"duplicate": {"age-file", "age-file"},
		"unsorted":  {"e2ee", "age-file"},
		"comma":     {"age,file"},
	}
	for name, features := range cases {
		t.Run(name, func(t *testing.T) {
			result := base
			result.Features = features
			var out bytes.Buffer
			if err := (JSONRenderer{}).Result(&out, result); err == nil || out.Len() != 0 {
				t.Fatalf("expected invalid features to be rejected without output: err=%v output=%q", err, out.String())
			}
		})
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
		ProfilesResult{Meta: Meta{"profiles list", "success"}}, ProfilesResult{Meta: Meta{"profiles show", "success"}}, RecipientsResult{Meta: Meta{"recipients list", "success"}}, RecipientsResult{Meta: Meta{"recipients show", "success"}}, ValidationsResult{Meta: Meta{"recipients validate", "success"}}, SoundsResult{Meta: Meta{"sounds", "success"}}, LimitsResult{Meta: Meta{"limits", "success"}}, AuthStatusResult{Meta: Meta{"auth status", "success"}}, BackendResult{Meta: Meta{"auth backend", "success"}}, InfoResult{Meta: Meta{"info", "success"}, Features: []string{"age-file"}}, VersionResult{Meta: Meta{"version", "success"}},
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

func TestSendRowValidationRejectsIncoherentPublicResults(t *testing.T) {
	validError := &domain.ErrorDetail{Code: "network_failure", Category: apperror.CategoryNetwork, Message: "request failed"}
	quota := 1
	cases := map[string]domain.TargetResult{
		"priority below range":      {Alias: "a", Outcome: domain.Sent, Priority: -3},
		"priority above range":      {Alias: "a", Outcome: domain.Sent, Priority: 3},
		"sent error":                {Alias: "a", Outcome: domain.Sent, Error: validError},
		"planned error":             {Alias: "a", Outcome: domain.Planned, Error: validError},
		"skipped error":             {Alias: "a", Outcome: domain.Skipped, Error: validError},
		"failed missing error":      {Alias: "a", Outcome: domain.Failed},
		"failed empty error code":   {Alias: "a", Outcome: domain.Failed, Error: &domain.ErrorDetail{Category: apperror.CategoryNetwork, Message: "failed"}},
		"failed ambiguous error":    {Alias: "a", Outcome: domain.Failed, Error: &domain.ErrorDetail{Code: "network_failure", Category: apperror.CategoryNetwork, Message: "failed", Ambiguous: true}},
		"ambiguous missing error":   {Alias: "a", Outcome: domain.Ambiguous},
		"ambiguous definite error":  {Alias: "a", Outcome: domain.Ambiguous, Error: validError},
		"unknown category":          {Alias: "a", Outcome: domain.Failed, Error: &domain.ErrorDetail{Code: "bad", Category: "other", Message: "failed"}},
		"negative quota":            {Alias: "a", Outcome: domain.Sent, QuotaRemaining: intPtr(-1)},
		"planned response metadata": {Alias: "a", Outcome: domain.Planned, RequestID: "request"},
		"skipped response metadata": {Alias: "a", Outcome: domain.Skipped, QuotaRemaining: &quota},
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			batch := domain.BatchResult{Command: "send", Results: []domain.TargetResult{target}}
			status := "success"
			if target.Outcome == domain.Planned {
				batch.DryRun, status = true, "dry_run"
			}
			if target.Outcome == domain.Failed {
				status = "failed"
			}
			if target.Outcome == domain.Ambiguous {
				status = "ambiguous"
			}
			if target.Outcome == domain.Skipped {
				batch.Interrupted, status = true, "interrupted"
			}
			var out bytes.Buffer
			if err := (JSONRenderer{}).Result(&out, SendResult{Meta: Meta{"send", status}, Batch: batch}); err == nil || out.Len() != 0 {
				t.Fatalf("expected rejection without output: err=%v output=%q", err, out.String())
			}
		})
	}
}

func intPtr(value int) *int { return &value }

func TestSendDTOAmbiguityComesFromOutcome(t *testing.T) {
	dto := sendDTO(domain.BatchResult{Results: []domain.TargetResult{{Outcome: domain.Ambiguous, Error: &domain.ErrorDetail{Ambiguous: false}}}})
	if dto.Targets[0].Error == nil || !dto.Targets[0].Error.Ambiguous {
		t.Fatal("ambiguous outcome must render ambiguous error defensively")
	}
}
