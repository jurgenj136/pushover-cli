package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/jurgenj136/pushover-cli/internal/cmd"
	"golang.org/x/term"
)

func main() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)

	streams := cmd.IO{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		IsTerminal: func(fd uintptr) bool {
			return term.IsTerminal(int(fd))
		},
	}
	dependencies, err := cmd.NewProductionDependencies(context.Background(), streams)
	if err != nil {
		signal.Stop(signals)
		_, _ = fmt.Fprintln(os.Stderr, "error: internal error")
		os.Exit(1)
	}
	dependencies.Signals = signals
	exit := cmd.Execute(context.Background(), os.Args[1:], streams, dependencies)
	signal.Stop(signals)
	os.Exit(exit)
}
