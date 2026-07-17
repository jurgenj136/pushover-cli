package cmd

import (
	"context"
	"errors"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

type authCommand struct {
	Status  authStatusCommand  `cmd:"" help:"Report selected authentication status."`
	Backend authBackendCommand `cmd:"" help:"Report or migrate the secret backend. Run 'pushover auth backend' to report it."`
}

type authStatusCommand struct{}
type authBackendCommand struct {
	Show authBackendShowCommand `cmd:"" default:"1" hidden:""`
	Set  authBackendSetCommand  `cmd:"" help:"Migrate secrets to a backend. Run 'pushover auth backend' to report it."`
}
type authBackendShowCommand struct{}
type authBackendSetCommand struct {
	Destination string `arg:"" enum:"native,file" help:"Destination secret backend."`
}

func executeAuth(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context, command string) int {
	mode := selectedMode(root.JSON, root.Plain)
	if command == "auth backend set" {
		if exit, rejected := rejectPlainMutation(streams, deps, root, command); rejected {
			return exit
		}
		return executeAuthBackendSet(ctx, streams, deps, root, parsed)
	}
	runtime, admin, selected, err := beginAuth(ctx, deps, root, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer runtime.Close()
	switch command {
	case "auth backend":
		if selected.SchemaVersion == 0 {
			return renderErrorWithColor(streams, deps, mode, command, apperror.New("configuration_missing", apperror.CategoryConfig, "configuration is unavailable"), root.NoColor)
		}
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.BackendResult{Meta: output.Meta{Command: command, Status: "success"}, Backend: selected.Backend.Type})
	case "auth status":
		return executeAuthStatus(streams, deps, root, runtime, admin, selected)
	default:
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), root.NoColor)
	}
}

func beginAuth(ctx context.Context, deps Dependencies, root rootCommand, parsed *kong.Context) (operation.Scope, AdminDependencies, config.Config, error) {
	if err := deps.validateAdminEnvironment(); err != nil {
		return operation.Scope{}, AdminDependencies{}, config.Config{}, err
	}
	timeout, err := parseTimeout(root.Timeout, flagSeen(parsed, "timeout"))
	if err != nil {
		return operation.Scope{}, AdminDependencies{}, config.Config{}, err
	}
	scope, err := startScope(ctx, timeout, deps)
	if err != nil {
		return operation.Scope{}, AdminDependencies{}, config.Config{}, err
	}
	fail := func(err error) (operation.Scope, AdminDependencies, config.Config, error) {
		scope.Close()
		return operation.Scope{}, AdminDependencies{}, config.Config{}, err
	}
	if err := scope.Context().Err(); err != nil {
		return fail(err)
	}
	admin, err := deps.openAdmin(scope.Context())
	if err != nil {
		return fail(err)
	}
	if admin.Config == nil {
		return fail(apperror.New("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable"))
	}
	selected, err := admin.Config.Load(scope.Context())
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return scope, admin, config.Config{}, nil
		}
		return fail(err)
	}
	return scope, admin, selected, nil
}

func executeAuthStatus(streams IO, deps Dependencies, root rootCommand, runtime operation.Scope, admin AdminDependencies, selected config.Config) int {
	const command = "auth status"
	mode := selectedMode(root.JSON, root.Plain)
	if selected.SchemaVersion == 0 {
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.AuthStatusResult{Meta: output.Meta{Command: command, Status: "success"}, Configured: false})
	}
	profile := root.Profile
	if profile == "" {
		profile = selected.DefaultProfile
	}
	service, err := adminSettingsForConfig(runtime.Context(), admin, selected)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	view, err := service.Profile(runtime.Context(), profile)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	return renderAdminResult(streams, deps, mode, root.NoColor, command, output.AuthStatusResult{Meta: output.Meta{Command: command, Status: "success"}, Profile: profile, Backend: selected.Backend.Type, Configured: true, TokenResolves: view.TokenResolves})
}

func executeAuthBackendSet(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context) int {
	const command = "auth backend set"
	mode := selectedMode(root.JSON, root.Plain)
	if err := deps.validateAdminEnvironment(); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	timeout, err := parseTimeout(root.Timeout, flagSeen(parsed, "timeout"))
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	scope, err := startScope(ctx, timeout, deps)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer scope.Close()
	if err := scope.Context().Err(); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	admin, err := deps.openAdmin(scope.Context())
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	migrator := admin.BackendMigrator
	if migrator == nil {
		if admin.Config == nil || admin.SecretFactory == nil {
			return renderErrorWithColor(streams, deps, mode, command, apperror.New("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable"), root.NoColor)
		}
		migrator = &secrets.BackendMigrator{Config: admin.Config, Factory: admin.SecretFactory}
	}
	destination := root.Auth.Backend.Set.Destination
	result, err := migrator.Migrate(scope.Context(), authDestination(destination))
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	var warnings []string
	if result.CleanupWarning != nil {
		warnings = []string{apperror.SafeDetail(result.CleanupWarning).Message}
		writeWarnings(streams, warnings)
	}
	return renderAdminResult(streams, deps, mode, root.NoColor, command, mutation(command, "backend", destination, "migrated", warnings))
}

func authDestination(raw string) config.Backend {
	if raw == "file" {
		return config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}
	}
	return config.Backend{Type: "native"}
}
