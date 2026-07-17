package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/delivery"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
)

func TestExecuteRootHelpNeedsNoRuntimeDependencies(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Execute(context.Background(), []string{"--help"}, testIO(&stdout, &stderr), Dependencies{})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage:") || !strings.Contains(stdout.String(), "send") || !strings.Contains(stdout.String(), "completion") {
		t.Fatalf("root help did not include Task 11 command tree: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestExecuteHelpAndCompletionStayLazy(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runtimeCalls := 0
	deps := Dependencies{runtime: func(context.Context, operation.Scope) (preflight.Builder, delivery.Runner, error) {
		runtimeCalls++
		return preflight.Builder{}, delivery.Runner{}, nil
	}}

	if got := Execute(context.Background(), []string{"--help"}, testIO(&stdout, &stderr), deps); got != 0 {
		t.Fatalf("help exit = %d", got)
	}
	stdout.Reset()
	stderr.Reset()
	if got := Execute(context.Background(), []string{"completion", "bash"}, testIO(&stdout, &stderr), deps); got != 0 {
		t.Fatalf("completion exit = %d; stderr=%q", got, stderr.String())
	}
	if runtimeCalls != 0 {
		t.Fatalf("help or completion initialized runtime %d times", runtimeCalls)
	}
	if !strings.Contains(stdout.String(), "complete -F _pushover pushover") {
		t.Fatalf("unexpected bash completion: %q", stdout.String())
	}
}

func TestExecuteRequiresExplicitSendCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Execute(context.Background(), []string{"message"}, testIO(&stdout, &stderr), Dependencies{})

	if exit != 2 {
		t.Fatalf("exit = %d, want usage error 2", exit)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("stderr = %q, want safe usage diagnostic", stderr.String())
	}
}

func TestAsAppErrorDistinguishesCancellationFromDeadline(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		wantCode    string
		wantMessage string
	}{
		{name: "cancellation", err: context.Canceled, wantCode: "operation_canceled", wantMessage: "operation canceled"},
		{name: "deadline", err: context.DeadlineExceeded, wantCode: "operation_timeout", wantMessage: "operation deadline exceeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := asAppError(test.err)
			if got.Code != test.wantCode || got.Category != apperror.CategoryNetwork || got.Message != test.wantMessage {
				t.Fatalf("asAppError(%v) = %#v, want %s/%s", test.err, got, test.wantCode, test.wantMessage)
			}
		})
	}
}

func TestPreScannedModeStopsAtEndOfFlags(t *testing.T) {
	if got := preScannedMode([]string{"send", "--", "--json"}); got != output.Human {
		t.Fatalf("mode after -- = %q, want human", got)
	}
	if got := preScannedMode([]string{"--json", "send", "--", "--plain"}); got != output.JSON {
		t.Fatalf("mode before -- = %q, want json", got)
	}
}

func TestPreScannedNoColorUsesEffectiveBooleanBeforeEndOfFlags(t *testing.T) {
	if !preScannedNoColor([]string{"--no-color", "send", "--", "--no-color=false"}) {
		t.Fatal("expected --no-color before -- to apply")
	}
	if preScannedNoColor([]string{"--no-color", "--no-color=false", "send"}) {
		t.Fatal("expected explicit false to disable pre-scanned no-color")
	}
}

func TestExecuteNoColorAppliesToAllErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "root", args: []string{"--no-color"}},
		{name: "parse", args: []string{"--no-color", "unknown"}},
		{name: "completion", args: []string{"--no-color", "--plain", "completion", "bash"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			factory := &recordingFactory{renderer: noOpRenderer{}}
			deps := Dependencies{Output: factory, lookupEnv: func(string) (string, bool) { return "", false }}
			stdout, stderr := &fdBuffer{fd: 1}, &fdBuffer{fd: 2}
			streams := IO{In: strings.NewReader(""), Out: stdout, Err: stderr, IsTerminal: func(uintptr) bool { return true }}
			if got := Execute(context.Background(), test.args, streams, deps); got != 2 {
				t.Fatalf("exit = %d, want usage error", got)
			}
			if len(factory.options) != 1 || !factory.options[0].TTY || factory.options[0].Color {
				t.Fatalf("renderer options = %#v, want no color on TTY", factory.options)
			}
		})
	}
}
