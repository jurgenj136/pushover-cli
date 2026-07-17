package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"golang.org/x/term"
)

const maxPromptBytes = 4096

// Prompter owns the interactive boundary. Read returns a caller-owned byte
// slice; callers must wipe it as soon as they have copied it into a service
// request. Prompt summaries contain only safe, non-secret metadata.
type Prompter interface {
	Read(context.Context, Prompt) ([]byte, error)
	Confirm(context.Context, PromptSummary) error
}

type Prompt struct {
	Label  string
	Secret bool
}

// PromptSummary intentionally has no generic values map. Callers may render
// only an action and safe subject identifiers; secret values never belong here.
type PromptSummary struct {
	Action   string
	Subjects []string
}

// TerminalPrompter is injectable for tests. Real terminal secret input uses
// term.ReadPassword; non-file readers use the bounded hermetic reader below.
type TerminalPrompter struct {
	Input        io.Reader
	Stderr       io.Writer
	IsTTY        func(io.Reader) bool
	passwordRead func(int) ([]byte, error)
}

func (p TerminalPrompter) Read(ctx context.Context, request Prompt) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input, stderr, isTTY := p.streams()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !isTTY(input) {
		return nil, promptError("prompt_terminal_required", "interactive input requires a terminal")
	}
	label := apperror.Sanitize(request.Label)
	if label == "" {
		label = "Value"
	}
	if _, err := fmt.Fprintf(stderr, "%s: ", label); err != nil {
		return nil, promptError("prompt_unavailable", "interactive prompt could not be shown")
	}

	var raw []byte
	var err error
	if request.Secret {
		if file, ok := input.(*os.File); ok {
			// ReadPassword manages echo state itself. Do not put it in a
			// cancellable goroutine: abandoning that read could leave terminal
			// state or a later input consumer behind. Ctrl-C and EOF are handled
			// by the terminal read; after it returns, a canceled context discards
			// the answer before it can reach a handler.
			passwordRead := p.passwordRead
			if passwordRead == nil {
				passwordRead = term.ReadPassword
			}
			raw, err = passwordRead(int(file.Fd()))
			if _, writeErr := fmt.Fprintln(stderr); writeErr != nil && err == nil {
				err = writeErr
			}
			if len(raw) > maxPromptBytes {
				wipePromptBytes(raw)
				raw = nil
				err = errors.New("prompt input is too large")
			}
			if err == nil && ctx.Err() != nil {
				err = ctx.Err()
			}
		} else {
			raw, err = awaitPromptRead(ctx, func() ([]byte, error) { return readPromptLine(input) })
		}
	} else {
		raw, err = awaitPromptRead(ctx, func() ([]byte, error) { return readPromptLine(input) })
	}
	defer wipePromptBytes(raw)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, promptError("prompt_input_unavailable", "interactive input was not completed")
	}
	return append([]byte(nil), raw...), nil
}

func (p TerminalPrompter) Confirm(ctx context.Context, summary PromptSummary) error {
	if ctx == nil {
		ctx = context.Background()
	}
	input, stderr, isTTY := p.streams()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !isTTY(input) {
		return promptError("confirmation_required", "confirmation requires --yes without a terminal")
	}
	action := apperror.Sanitize(summary.Action)
	if action == "" {
		action = "Continue"
	}
	subjects := make([]string, 0, len(summary.Subjects))
	for _, subject := range summary.Subjects {
		if safe := apperror.Sanitize(subject); safe != "" {
			subjects = append(subjects, safe)
		}
	}
	if len(subjects) != 0 {
		action += " (" + strings.Join(subjects, ", ") + ")"
	}
	if _, err := fmt.Fprintf(stderr, "%s? [y/N]: ", action); err != nil {
		return promptError("confirmation_unavailable", "confirmation could not be shown")
	}
	raw, err := awaitPromptRead(ctx, func() ([]byte, error) { return readPromptLine(input) })
	defer wipePromptBytes(raw)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return promptError("confirmation_declined", "operation was not confirmed")
	}
	answer := strings.ToLower(strings.TrimSpace(string(raw)))
	if answer != "y" && answer != "yes" {
		return promptError("confirmation_declined", "operation was not confirmed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (p TerminalPrompter) streams() (io.Reader, io.Writer, func(io.Reader) bool) {
	input := p.Input
	if input == nil {
		input = os.Stdin
	}
	stderr := p.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	isTTY := p.IsTTY
	if isTTY == nil {
		isTTY = promptTerminal
	}
	return input, stderr, isTTY
}

func promptTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

type promptReadResult struct {
	value []byte
	err   error
}

// awaitPromptRead does not close caller-owned stdin. An unbuffered result and
// abandonment channel ensure a secret returned after cancellation is wiped by
// the reader goroutine rather than left queued in a channel buffer.
func awaitPromptRead(ctx context.Context, read func() ([]byte, error)) ([]byte, error) {
	result := make(chan promptReadResult)
	abandon := make(chan struct{})
	go func() {
		value, err := read()
		select {
		case result <- promptReadResult{value: value, err: err}:
		case <-abandon:
			wipePromptBytes(value)
		}
	}()
	select {
	case <-ctx.Done():
		close(abandon)
		return nil, ctx.Err()
	case value := <-result:
		return value.value, value.err
	}
}

func readPromptLine(reader io.Reader) ([]byte, error) {
	line := make([]byte, 0, 64)
	var byteBuffer [1]byte
	defer wipePromptBytes(byteBuffer[:])
	for {
		count, err := reader.Read(byteBuffer[:])
		if count > 0 {
			if byteBuffer[0] == '\n' {
				return line, nil
			}
			if len(line) == maxPromptBytes {
				wipePromptBytes(line)
				return nil, errors.New("prompt input is too large")
			}
			line = append(line, byteBuffer[0])
		}
		if err != nil {
			return line, err
		}
		if count == 0 {
			wipePromptBytes(line)
			return nil, io.ErrNoProgress
		}
	}
}

// promptPassphrase copies an answer before wiping the prompt's ownership.
// AgeStore wipes the returned copy after deriving its credentials, so the
// callback retains no secret between operations.
func promptPassphrase(prompter Prompter) func(context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		if prompter == nil {
			return nil, promptError("prompt_unavailable", "interactive prompt is unavailable")
		}
		answer, err := prompter.Read(ctx, Prompt{Label: "Secret-store passphrase", Secret: true})
		if err != nil {
			return nil, err
		}
		owned := append([]byte(nil), answer...)
		wipePromptBytes(answer)
		return owned, nil
	}
}

func wipePromptBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func promptError(code, message string) error {
	return apperror.New(code, apperror.CategoryConfirmation, message)
}
