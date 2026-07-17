package cmd

import (
	"bytes"
	"context"
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

type adminRuntime struct {
	settings settings.Service
	scope    operation.Scope
}

func beginAdmin(ctx context.Context, deps Dependencies, root rootCommand, parsed *kong.Context) (adminRuntime, error) {
	if err := deps.validateAdminEnvironment(); err != nil {
		return adminRuntime{}, err
	}
	timeout, err := parseTimeout(root.Timeout, flagSeen(parsed, "timeout"))
	if err != nil {
		return adminRuntime{}, err
	}
	scope, err := startScope(ctx, timeout, deps)
	if err != nil {
		return adminRuntime{}, err
	}
	if err := scope.Context().Err(); err != nil {
		scope.Close()
		return adminRuntime{}, err
	}
	service, err := deps.openAdminSettings(scope.Context())
	if err != nil {
		scope.Close()
		return adminRuntime{}, err
	}
	return adminRuntime{settings: service, scope: scope}, nil
}

func renderAdminResult(streams IO, deps Dependencies, mode output.Mode, noColor bool, command string, result output.CommandResult) int {
	renderer, err := newRenderer(streams, deps, mode, noColor, streams.Out)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), noColor)
	}
	var encoded bytes.Buffer
	if err := renderer.Result(&encoded, result); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), noColor)
	}
	if err := commitOutput(streams.Out, encoded.Bytes()); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), noColor)
	}
	return apperror.Exit(nil, nil)
}

func adminPrompter(deps Dependencies) (Prompter, error) {
	if deps.Prompter == nil {
		return nil, apperror.New("prompt_unavailable", apperror.CategoryConfirmation, "interactive prompt is unavailable")
	}
	return deps.Prompter, nil
}

func writeWarnings(streams IO, warnings []string) {
	for _, warning := range warnings {
		if value := apperror.Sanitize(warning); value != "" {
			_, _ = fmt.Fprintln(streams.Err, "warning: "+value)
		}
	}
}

// writeRemovalSummary is deliberately fallible: destructive work must not
// proceed unless its consequences reached stderr before confirmation.
func writeRemovalSummary(streams IO, impact settings.RemovalImpact) error {
	parts := []string{"Removal affects " + fmt.Sprint(impact.ObsoleteSecretCount) + " obsolete secret(s)."}
	if impact.DefaultProfile {
		parts = append(parts, "The selected default profile will change.")
	}
	if impact.ReplacementProfile != "" {
		parts = append(parts, "Replacement: "+apperror.Sanitize(impact.ReplacementProfile)+".")
	}
	for _, part := range parts {
		if _, err := fmt.Fprintln(streams.Err, apperror.Sanitize(part)); err != nil {
			return err
		}
	}
	for _, consumer := range impact.Consumers {
		line := fmt.Sprintf("Reference: %s %s %s.", consumer.Resource, consumer.Name, consumer.Field)
		if _, err := fmt.Fprintln(streams.Err, apperror.Sanitize(line)); err != nil {
			return err
		}
	}
	return nil
}

func rejectPlainMutation(streams IO, deps Dependencies, root rootCommand, command string) (int, bool) {
	if !root.Plain {
		return 0, false
	}
	return renderErrorWithColor(streams, deps, output.Plain, command, apperror.New("plain_unsupported", apperror.CategoryUsage, "plain output is unsupported for this command"), root.NoColor), true
}
