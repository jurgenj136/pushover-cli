package cmd

import "io"

// terminalInput adapts the injected IO contract to delivery's confirmer. The
// confirmation prompt is interactive only when stdin is a terminal; stderr is
// solely where the already-sanitized prompt is rendered.
func terminalInput(streams IO) func(io.Reader) bool {
	return func(reader io.Reader) bool {
		if value, ok := reader.(interface{ Fd() uintptr }); ok {
			return streams.IsTerminal(value.Fd())
		}
		return streams.IsTerminal(0)
	}
}
