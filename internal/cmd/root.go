package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
)

type rootCommand struct {
	Profile string `name:"profile" help:"Select a configured application profile."`
	JSON    bool   `name:"json" help:"Emit machine-readable JSON."`
	Plain   bool   `name:"plain" help:"Emit stable tab-separated output."`
	NoColor bool   `name:"no-color" help:"Disable color output."`
	Timeout string `name:"timeout" help:"Total operation timeout (1s through 15m)."`

	Setup      setupCommand      `cmd:"" help:"Interactively configure Pushover."`
	Send       sendCommand       `cmd:"" help:"Send a message."`
	Profiles   profilesCommand   `cmd:"" help:"Manage application profiles."`
	Recipients recipientsCommand `cmd:"" help:"Manage recipient aliases."`
	Sounds     soundsCommand     `cmd:"" help:"List supported Pushover sounds."`
	Limits     limitsCommand     `cmd:"" help:"Report the current Pushover API limits."`
	Auth       authCommand       `cmd:"" help:"Inspect or migrate authentication storage."`
	Info       infoCommand       `cmd:"" help:"Report local configuration and build information."`
	Version    versionCommand    `cmd:"" help:"Print version information."`
	Completion completionCommand `cmd:"" help:"Print shell completion."`
}

type completionCommand struct {
	Shell string `arg:"" enum:"bash,zsh,fish" help:"Shell name."`
}

// Execute parses and executes one CLI invocation, returning a semantic exit
// code. Kong's process-exit callback is captured, never allowed to terminate
// an embedding process.
func Execute(ctx context.Context, args []string, streams IO, deps Dependencies) int {
	streams = normalizeIO(streams)
	var cli rootCommand
	var capturedExit *int
	parser, err := kong.New(&cli,
		kong.Name("pushover"),
		kong.Description("Send outbound Pushover messages."),
		kong.Writers(streams.Out, streams.Err),
		kong.Exit(func(code int) { capturedExit = &code }),
	)
	if err != nil {
		return renderError(streams, deps, output.Human, "send", apperror.New("parser_unavailable", apperror.CategoryInternal, apperror.MessageInternal), preScannedNoColor(args))
	}
	parsed, parseErr := parser.Parse(args)
	if capturedExit != nil {
		return *capturedExit
	}
	if parseErr != nil {
		return renderParseError(streams, deps, args, parseErr)
	}
	if parsed == nil {
		return renderError(streams, deps, preScannedMode(args), "send", apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), preScannedNoColor(args))
	}
	parsedCommand := selectedCommand(parsed)
	selected := parsedCommand
	if selected == "auth backend show" {
		selected = "auth backend"
	}
	mode := selectedMode(cli.JSON, cli.Plain)
	if cli.JSON && cli.Plain {
		return renderError(streams, deps, mode, selected, apperror.New("output_mode_conflict", apperror.CategoryUsage, "--json and --plain cannot be combined"), cli.NoColor)
	}
	if parsedCommand == "auth backend show" && explicitAuthBackendShow(args) {
		return renderError(streams, deps, mode, selected, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), cli.NoColor)
	}
	if selected == "" {
		return renderError(streams, deps, mode, "send", apperror.New("command_required", apperror.CategoryUsage, "a command is required (try 'send')"), cli.NoColor)
	}
	switch selected {
	case "send":
		return executeSend(ctx, streams, deps, cli, parsed)
	case "setup":
		return executeSetup(ctx, streams, deps, cli, parsed)
	case "profiles add", "profiles list", "profiles show", "profiles use", "profiles remove":
		return executeProfiles(ctx, streams, deps, cli, parsed, selected)
	case "recipients add", "recipients list", "recipients show", "recipients validate", "recipients remove":
		return executeRecipients(ctx, streams, deps, cli, parsed, selected)
	case "auth status", "auth backend", "auth backend set":
		return executeAuth(ctx, streams, deps, cli, parsed, selected)
	case "sounds", "limits", "info":
		return executeIntrospection(ctx, streams, deps, cli, parsed, selected)
	case "version":
		return executeVersion(ctx, streams, deps, cli)
	case "completion":
		return executeCompletion(streams, deps, mode, cli.Completion, cli.NoColor)
	default:
		return renderError(streams, deps, mode, "send", apperror.New("command_required", apperror.CategoryUsage, "a command is required"), cli.NoColor)
	}
}

func explicitAuthBackendShow(args []string) bool {
	words := make([]string, 0, 3)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--profile" || arg == "--timeout" {
			index++
			continue
		}
		if arg == "--" || strings.HasPrefix(arg, "--profile=") || strings.HasPrefix(arg, "--timeout=") || strings.HasPrefix(arg, "-") {
			continue
		}
		words = append(words, arg)
	}
	return len(words) == 3 && words[0] == "auth" && words[1] == "backend" && words[2] == "show"
}

func selectedCommand(ctx *kong.Context) string {
	if ctx == nil || ctx.Selected() == nil {
		return ""
	}
	var command []string
	for _, part := range strings.Fields(ctx.Command()) {
		if strings.HasPrefix(part, "<") {
			break
		}
		command = append(command, part)
	}
	return strings.Join(command, " ")
}

func selectedMode(jsonMode, plainMode bool) output.Mode {
	if jsonMode {
		return output.JSON
	}
	if plainMode {
		return output.Plain
	}
	return output.Human
}

func preScannedMode(args []string) output.Mode {
	jsonMode, plainMode := false, false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if value, ok := scannedBool(arg, "--json"); ok {
			jsonMode = value
		}
		if value, ok := scannedBool(arg, "--plain"); ok {
			plainMode = value
		}
	}
	return selectedMode(jsonMode, plainMode)
}

func preScannedNoColor(args []string) bool {
	noColor := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if value, ok := scannedBool(arg, "--no-color"); ok {
			noColor = value
		}
	}
	return noColor
}

func scannedBool(arg, flag string) (bool, bool) {
	if arg == flag {
		return true, true
	}
	if !strings.HasPrefix(arg, flag+"=") {
		return false, false
	}
	value, err := strconv.ParseBool(strings.TrimPrefix(arg, flag+"="))
	return value, err == nil
}

func renderParseError(streams IO, deps Dependencies, args []string, _ error) int {
	return renderError(streams, deps, preScannedMode(args), preScannedCommand(args), apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), preScannedNoColor(args))
}

// preScannedCommand is intentionally conservative. Parse failures do not
// provide a resolved command path, so only actual command tokens are used;
// values consumed by global flags are never mistaken for commands.
func preScannedCommand(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if arg == "--profile" || arg == "--timeout" {
			index++
			continue
		}
		if strings.HasPrefix(arg, "--profile=") || strings.HasPrefix(arg, "--timeout=") || strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "send", "setup", "sounds", "limits", "info", "version", "completion":
			return arg
		case "auth":
			if index+1 < len(args) {
				switch args[index+1] {
				case "status":
					return "auth status"
				case "backend":
					if index+2 < len(args) && args[index+2] == "set" {
						return "auth backend set"
					}
					return "auth backend"
				}
			}
			return "auth"
		case "profiles":
			if index+1 < len(args) {
				switch args[index+1] {
				case "add", "list", "show", "use", "remove":
					return "profiles " + args[index+1]
				}
			}
			return "profiles"
		case "recipients":
			if index+1 < len(args) {
				switch args[index+1] {
				case "add", "list", "show", "validate", "remove":
					return "recipients " + args[index+1]
				}
			}
			return "recipients"
		}
	}
	return "send"
}

func renderError(streams IO, deps Dependencies, mode output.Mode, command string, err *apperror.Error, noColor bool) int {
	return renderErrorWithColor(streams, deps, mode, command, err, noColor)
}

func renderErrorWithColor(streams IO, deps Dependencies, mode output.Mode, command string, err *apperror.Error, noColor bool) int {
	renderer, rendererErr := newRenderer(streams, deps, mode, noColor, streams.Err)
	if rendererErr != nil {
		_, _ = fmt.Fprintln(streams.Err, "error: internal error")
		return apperror.ExitInternal
	}
	if err == nil {
		err = apperror.New(apperror.CodeInternal, apperror.CategoryInternal, apperror.MessageInternal)
	}
	if command == "" {
		command = "send"
	}
	var encoded bytes.Buffer
	if writeErr := renderer.Error(&encoded, command, err); writeErr != nil || commitOutput(streams.Err, encoded.Bytes()) != nil {
		_, _ = fmt.Fprintln(streams.Err, "error: internal error")
		return apperror.ExitInternal
	}
	return apperror.Exit(err, nil)
}

func newRenderer(streams IO, deps Dependencies, mode output.Mode, noColor bool, target io.Writer) (output.Renderer, error) {
	factory := deps.Output
	if factory == nil {
		factory = output.NewFactory()
	}
	tty := terminalFor(streams, target)
	_, noColorEnvironment := deps.environment("NO_COLOR")
	color := tty && !noColor && !noColorEnvironment
	return factory.New(mode, output.RenderOptions{TTY: tty, Color: color})
}

func terminalFor(streams IO, target io.Writer) bool {
	if value, ok := target.(interface{ Fd() uintptr }); ok {
		return streams.IsTerminal(value.Fd())
	}
	return streams.IsTerminal(0)
}

func commitOutput(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}

func startScope(ctx context.Context, timeout *time.Duration, deps Dependencies) (operation.Scope, error) {
	return deps.Operations.Start(ctx, timeout)
}

func parseTimeout(raw string, present bool) (*time.Duration, error) {
	if !present {
		return nil, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return nil, apperror.New("invalid_timeout", apperror.CategoryUsage, "timeout must be between 1 second and 15 minutes")
	}
	return &value, nil
}

func asAppError(err error) *apperror.Error {
	var typed *apperror.Error
	if errors.As(err, &typed) && typed != nil {
		return typed
	}
	if errors.Is(err, context.Canceled) {
		return apperror.New("operation_canceled", apperror.CategoryNetwork, "operation canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apperror.New("operation_timeout", apperror.CategoryNetwork, "operation deadline exceeded")
	}
	return apperror.New(apperror.CodeInternal, apperror.CategoryInternal, apperror.MessageInternal)
}
