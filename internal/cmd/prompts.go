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
// request. Prompt summaries contain only safe, non-secret metadata. Context is
// checked before and after each synchronous input operation; the reader owns
// how that operation is interrupted. No prompt goroutine or caller-stream
// close is used, so a canceled call cannot keep consuming later input.
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

// TerminalPrompter is injectable for tests. Real terminal secret input uses a
// bounded, no-echo platform reader; injected readers use the same bounded line
// core without changing terminal state.
type TerminalPrompter struct {
	Input        io.Reader
	Stderr       io.Writer
	IsTTY        func(io.Reader) bool
	passwordRead func(int, int) ([]byte, error)
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
			// Terminal state and input ownership stay in this call. Do not put a
			// password read in a cancellable goroutine: abandoning it could leave
			// echo state or a later input consumer behind. Ctrl-C and EOF are
			// handled by the platform read; after it returns, cancellation
			// discards the answer before it can reach a handler.
			passwordRead := p.passwordRead
			if passwordRead == nil {
				passwordRead = readTerminalPassword
			}
			raw, err = passwordRead(int(file.Fd()), maxPromptBytes)
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
			raw, err = readPromptLine(input)
		}
	} else {
		raw, err = readPromptLine(input)
	}
	defer wipePromptBytes(raw)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
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
	raw, err := readPromptLine(input)
	defer wipePromptBytes(raw)
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
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

func readPromptLine(reader io.Reader) ([]byte, error) {
	return readBoundedLine(reader.Read, maxPromptBytes, '\n')
}

type passwordTerminal interface {
	disableEcho(int) (func() error, error)
	read(int, []byte) (int, error)
	lineTerminator() byte
}

func readTerminalPasswordWith(fd, maximum int, terminal passwordTerminal) (value []byte, err error) {
	if maximum <= 0 || terminal == nil {
		return nil, errors.New("terminal password input is unavailable")
	}
	restore, err := terminal.disableEcho(fd)
	if err != nil {
		return nil, err
	}
	defer func() {
		if restoreErr := restore(); restoreErr != nil {
			wipePromptBytes(value)
			value = nil
			err = restoreErr
		}
	}()
	value, err = readBoundedLine(func(output []byte) (int, error) {
		return terminal.read(fd, output)
	}, maximum, terminal.lineTerminator())
	if err != nil {
		wipePromptBytes(value)
		value = nil
	}
	return value, err
}

func readBoundedLine(read func([]byte) (int, error), maximum int, terminator byte) ([]byte, error) {
	if maximum <= 0 {
		return nil, errors.New("prompt input is unavailable")
	}
	line := make([]byte, 0, 64)
	var byteBuffer [1]byte
	defer wipePromptBytes(byteBuffer[:])
	tooLarge := false
	for {
		count, err := read(byteBuffer[:])
		if count > 0 {
			character := byteBuffer[0]
			if character == terminator {
				if tooLarge {
					return nil, errors.New("prompt input is too large")
				}
				return line, nil
			}
			if character == '\r' || character == '\n' {
				continue
			}
			if character == '\b' {
				if !tooLarge && len(line) > 0 {
					line[len(line)-1] = 0
					line = line[:len(line)-1]
				}
				continue
			}
			if tooLarge {
				continue
			}
			if len(line) == maximum {
				wipePromptBytes(line)
				line = nil
				tooLarge = true
				continue
			}
			line = append(line, character)
		}
		if err != nil {
			if tooLarge {
				return nil, errors.New("prompt input is too large")
			}
			return line, err
		}
		if count == 0 {
			wipePromptBytes(line)
			return nil, io.ErrNoProgress
		}
	}
}

// promptPassphrase copies an answer before wiping the prompt-owned buffer. The
// callback captures no returned bytes. Backend and age-library immutable or
// derived allocations have their own lifetimes and are outside this seam's
// wiping guarantee.
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
