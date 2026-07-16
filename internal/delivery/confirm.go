package delivery

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
	"golang.org/x/term"
)

// TerminalConfirmer owns the interactive emergency prompt. Its fields are
// injectable for tests; zero values use the process terminal streams.
type TerminalConfirmer struct {
	Input  io.Reader
	Stderr io.Writer
	IsTTY  func(io.Reader) bool
}

func (c TerminalConfirmer) Confirm(ctx context.Context, summary preflight.Confirmation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input := c.Input
	if input == nil {
		input = os.Stdin
	}
	stderr := c.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	isTTY := c.IsTTY
	if isTTY == nil {
		isTTY = terminalReader
	}
	if !isTTY(input) {
		return apperror.New("emergency_confirmation_required", apperror.CategoryConfirmation, "emergency send requires --yes without a terminal")
	}

	aliases := make([]string, len(summary.Aliases))
	for i, alias := range summary.Aliases {
		aliases[i] = apperror.Sanitize(alias)
	}
	if _, err := fmt.Fprintf(stderr, "Emergency notification to %s (priority %d, retry %s, expire %s, retries %d, encrypted %t). Send? [y/N]: ", strings.Join(aliases, ", "), summary.Priority, summary.Retry, summary.Expire, summary.EffectiveRetries, summary.Encrypted); err != nil {
		return apperror.New("emergency_confirmation_unavailable", apperror.CategoryConfirmation, "emergency confirmation could not be shown")
	}
	// Terminal reads are not cancellable. Run the one-shot read separately so
	// the CLI can honor signals/deadlines immediately. If cancellation wins,
	// the buffered goroutine exits when the terminal read eventually returns or
	// when this short-lived CLI process terminates.
	type readResult struct {
		line string
		err  error
	}
	read := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(input).ReadString('\n')
		read <- readResult{line: line, err: err}
	}()
	var line string
	var err error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-read:
		line, err = result.line, result.err
	}
	if err != nil && len(line) == 0 {
		return apperror.New("emergency_confirmation_declined", apperror.CategoryConfirmation, "emergency send was not confirmed")
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "y" && answer != "yes" {
		return apperror.New("emergency_confirmation_declined", apperror.CategoryConfirmation, "emergency send was not confirmed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func terminalReader(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
