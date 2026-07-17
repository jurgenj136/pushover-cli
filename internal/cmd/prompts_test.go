package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
)

func TestTerminalPrompterRequiresTTYAndKeepsSecretOutOfPromptOutput(t *testing.T) {
	secret := "never-render-this"
	reader := &promptReadMustNotHappen{}
	_, err := (TerminalPrompter{Input: reader, Stderr: io.Discard}).Read(context.Background(), Prompt{Label: "Application token", Secret: true})
	if !apperror.Is(err, apperror.CategoryConfirmation) {
		t.Fatalf("err = %v, want typed terminal error", err)
	}
	if reader.reads != 0 {
		t.Fatalf("non-terminal prompt read stdin %d times", reader.reads)
	}

	var output bytes.Buffer
	prompt := TerminalPrompter{Input: strings.NewReader(secret + "\n"), Stderr: &output, IsTTY: func(io.Reader) bool { return true }}
	answer, err := prompt.Read(context.Background(), Prompt{Label: "Application token", Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	defer wipePromptBytes(answer)
	if string(answer) != secret {
		t.Fatalf("answer = %q, want secret", answer)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatalf("secret appeared in prompt output: %q", output.String())
	}
}

func TestTerminalPrompterReturnsOwnedSecretBufferThatCanBeWiped(t *testing.T) {
	source := []byte("copy-me\n")
	prompt := TerminalPrompter{Input: bytes.NewReader(source), Stderr: io.Discard, IsTTY: func(io.Reader) bool { return true }}
	answer, err := prompt.Read(context.Background(), Prompt{Label: "Secret", Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	answer[0] = 'X'
	if source[0] != 'c' {
		t.Fatal("prompt returned a buffer shared with input")
	}
	wipePromptBytes(answer)
	for _, value := range answer {
		if value != 0 {
			t.Fatalf("answer was not wiped: %q", answer)
		}
	}
}

func TestTerminalPrompterUsesPasswordReaderForSecretOnFileInput(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	calls := 0
	var output bytes.Buffer
	prompt := TerminalPrompter{
		Input: file, Stderr: &output, IsTTY: func(io.Reader) bool { return true },
		passwordRead: func(fd int) ([]byte, error) {
			calls++
			if fd != int(file.Fd()) {
				t.Fatalf("password reader fd = %d, want stdin fd %d", fd, file.Fd())
			}
			return []byte("not-echoed"), nil
		},
	}
	answer, err := prompt.Read(context.Background(), Prompt{Label: "Secret", Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	defer wipePromptBytes(answer)
	if calls != 1 {
		t.Fatalf("password reader called %d times", calls)
	}
	if strings.Contains(output.String(), "not-echoed") {
		t.Fatalf("password reader value appeared in output: %q", output.String())
	}
}

func TestTerminalPrompterWipesOversizedPasswordReaderResult(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	secret := bytes.Repeat([]byte("x"), maxPromptBytes+1)
	prompt := TerminalPrompter{
		Input: file, Stderr: io.Discard, IsTTY: func(io.Reader) bool { return true },
		passwordRead: func(int) ([]byte, error) {
			return secret, nil
		},
	}
	_, err = prompt.Read(context.Background(), Prompt{Label: "Secret", Secret: true})
	if !apperror.Is(err, apperror.CategoryConfirmation) {
		t.Fatalf("err = %v, want bounded prompt error", err)
	}
	for _, value := range secret {
		if value != 0 {
			t.Fatal("oversized password reader result was not wiped")
		}
	}
}

func TestPromptPassphraseCopiesAndWipesPromptOwnership(t *testing.T) {
	prompt := &recordingPrompter{answer: []byte("passphrase")}
	answer, err := promptPassphrase(prompt)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer wipePromptBytes(answer)
	if string(answer) != "passphrase" {
		t.Fatalf("answer = %q", answer)
	}
	for _, value := range prompt.answer {
		if value != 0 {
			t.Fatal("prompt-owned passphrase was not wiped")
		}
	}
}

func TestTerminalPrompterRejectsEOFAndOversizedInputWithoutLeakingSecret(t *testing.T) {
	secret := "do-not-disclose"
	tests := []io.Reader{
		strings.NewReader(secret),
		strings.NewReader(strings.Repeat("x", maxPromptBytes+1)),
	}
	for _, input := range tests {
		var output bytes.Buffer
		_, err := (TerminalPrompter{Input: input, Stderr: &output, IsTTY: func(io.Reader) bool { return true }}).Read(context.Background(), Prompt{Label: "Secret", Secret: true})
		if !apperror.Is(err, apperror.CategoryConfirmation) {
			t.Fatalf("err = %v, want typed prompt failure", err)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(output.String(), secret) {
			t.Fatalf("prompt failure leaked secret: err=%q output=%q", err, output.String())
		}
	}
}

func TestTerminalPrompterBlockingReadObservesCancellation(t *testing.T) {
	reader := &promptBlockingReader{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := (TerminalPrompter{Input: reader, Stderr: io.Discard, IsTTY: func(io.Reader) bool { return true }}).Read(ctx, Prompt{Label: "Secret", Secret: true})
		result <- err
	}()
	<-reader.started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocking prompt read ignored cancellation")
	}
	close(reader.release)
}

func TestTerminalPrompterConfirmationRendersSanitizedSummary(t *testing.T) {
	var output bytes.Buffer
	err := (TerminalPrompter{Input: strings.NewReader("yes\n"), Stderr: &output, IsTTY: func(io.Reader) bool { return true }}).Confirm(context.Background(), PromptSummary{Action: "Remove profile", Subjects: []string{"ops\nspoof"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "ops\nspoof") {
		t.Fatalf("summary was not sanitized: %q", output.String())
	}
	if !strings.Contains(output.String(), "Remove profile") || !strings.Contains(output.String(), "ops spoof") {
		t.Fatalf("safe summary missing from output: %q", output.String())
	}
}

type promptReadMustNotHappen struct{ reads int }

func (r *promptReadMustNotHappen) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("stdin must not be read")
}

type promptBlockingReader struct {
	started chan struct{}
	release chan struct{}
}

type recordingPrompter struct{ answer []byte }

func (p *recordingPrompter) Read(context.Context, Prompt) ([]byte, error) { return p.answer, nil }
func (*recordingPrompter) Confirm(context.Context, PromptSummary) error   { return nil }

func (r *promptBlockingReader) Read([]byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-r.release
	return 0, io.EOF
}
