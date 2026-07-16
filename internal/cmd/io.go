package cmd

import (
	"io"
	"os"
)

// IO contains the process streams used by the command. Keeping these streams
// injected makes command execution deterministic in tests and embedders.
type IO struct {
	In         io.Reader
	Out, Err   io.Writer
	IsTerminal func(uintptr) bool
}

func normalizeIO(value IO) IO {
	if value.In == nil {
		value.In = os.Stdin
	}
	if value.Out == nil {
		value.Out = os.Stdout
	}
	if value.Err == nil {
		value.Err = os.Stderr
	}
	if value.IsTerminal == nil {
		value.IsTerminal = func(uintptr) bool { return false }
	}
	return value
}
