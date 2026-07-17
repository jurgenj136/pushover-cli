package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
)

type soundsCommand struct{}
type limitsCommand struct{}
type infoCommand struct{}
type versionCommand struct{}

var supportedInfoFeatures = []string{"age-file", "e2ee", "native-secrets"}

// executeIntrospection owns the local and authenticated read-only command
// paths. Root dispatch supplies the selected stable command path so this file
// remains independent from command-tree registration.
func executeIntrospection(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context, command string) int {
	switch command {
	case "sounds", "limits":
		return executeAuthenticatedRead(ctx, streams, deps, root, parsed, command)
	case "info":
		return executeInfo(ctx, streams, deps, root, parsed)
	case "version":
		return executeVersion(ctx, streams, deps, root)
	default:
		return renderErrorWithColor(streams, deps, selectedMode(root.JSON, root.Plain), command, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), root.NoColor)
	}
}

func executeAuthenticatedRead(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context, command string) int {
	mode := selectedMode(root.JSON, root.Plain)
	runtime, admin, err := beginAuthenticatedRead(ctx, deps, root, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer runtime.scope.Close()
	if err := runtime.scope.Context().Err(); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}

	// beginAdmin already opened only the selected settings service. The API was
	// obtained lazily after parsing and scope creation, but before secret
	// material is read, so a missing implementation cannot cause needless
	// secret access.
	if admin.API == nil {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("introspection_unavailable", apperror.CategoryInternal, apperror.MessageInternal), root.NoColor)
	}

	token, err := runtime.settings.SelectedProfileToken(runtime.scope.Context(), root.Profile)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer wipePromptBytes(token)

	phase, cancel, err := runtime.scope.Phase(operation.PhaseValidation)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer cancel()

	switch command {
	case "sounds":
		response, err := admin.API.Sounds(phase, string(token))
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		rows := soundRows(response.Sounds)
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.SoundsResult{Meta: output.Meta{Command: command, Status: "success"}, Rows: rows})
	case "limits":
		response, err := admin.API.Limits(phase, string(token))
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.LimitsResult{Meta: output.Meta{Command: command, Status: "success"}, Limit: response.Limit, Remaining: response.Remaining, Reset: response.Reset})
	default:
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), root.NoColor)
	}
}

// beginAuthenticatedRead adapts beginAdmin without forcing a second invocation
// of the lazy admin factory. Production keeps its runtime graph inert until
// beginAdmin reaches its normal post-scope configuration boundary; injected
// dependencies retain the existing preference over the production factory.
func beginAuthenticatedRead(ctx context.Context, deps Dependencies, root rootCommand, parsed *kong.Context) (adminRuntime, AdminDependencies, error) {
	admin, loaded := deps.Admin, hasAdminDependencies(deps.Admin)
	runtimeDeps := deps
	runtimeDeps.admin = func(ctx context.Context) (AdminDependencies, error) {
		if loaded {
			return admin, nil
		}
		value, err := deps.openAdmin(ctx)
		if err != nil {
			return AdminDependencies{}, err
		}
		admin, loaded = value, true
		return admin, nil
	}
	runtime, err := beginAdmin(ctx, runtimeDeps, root, parsed)
	if err != nil {
		return adminRuntime{}, AdminDependencies{}, err
	}
	return runtime, admin, nil
}

func soundRows(sounds []pushover.Sound) []output.SoundRow {
	rows := make([]output.SoundRow, 0, len(sounds))
	for _, sound := range sounds {
		rows = append(rows, output.SoundRow{Name: sound.Name, Description: sound.Description})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name == rows[j].Name {
			return rows[i].Description < rows[j].Description
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func executeInfo(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context) int {
	const command = "info"
	mode := selectedMode(root.JSON, root.Plain)
	scope, admin, selected, err := beginInfo(ctx, deps, root, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer scope.Close()

	profile, backend := "", ""
	if selected.SchemaVersion != 0 {
		profile = root.Profile
		if profile == "" {
			profile = selected.DefaultProfile
		}
		backend = selected.Backend.Type
	}
	build := deps.buildInfo()
	return renderAdminResult(streams, deps, mode, root.NoColor, command, output.InfoResult{
		Meta:       output.Meta{Command: command, Status: "success"},
		ConfigPath: admin.Config.Path(),
		Profile:    profile,
		Backend:    backend,
		Version:    build.Version,
		Commit:     build.Commit,
		BuildDate:  build.Date,
		Features:   append([]string(nil), supportedInfoFeatures...),
	})
}

func beginInfo(ctx context.Context, deps Dependencies, root rootCommand, parsed *kong.Context) (operation.Scope, AdminDependencies, config.Config, error) {
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

// executeVersion intentionally bypasses the normal renderer helper because it
// reads NO_COLOR through Dependencies. Version output is an absolute
// zero-runtime-dependency path: only injected build metadata and output
// formatting are allowed.
func executeVersion(_ context.Context, streams IO, deps Dependencies, root rootCommand) int {
	build := deps.buildInfo()
	return renderVersionResult(streams, deps, selectedMode(root.JSON, root.Plain), output.VersionResult{
		Meta:      output.Meta{Command: "version", Status: "success"},
		Version:   build.Version,
		Commit:    build.Commit,
		BuildDate: build.Date,
	})
}

func renderVersionResult(streams IO, deps Dependencies, mode output.Mode, result output.CommandResult) int {
	factory := deps.Output
	if factory == nil {
		factory = output.NewFactory()
	}
	renderer, err := factory.New(mode, output.RenderOptions{})
	if err != nil {
		return renderVersionError(streams, mode, asAppError(err))
	}
	var encoded bytes.Buffer
	if err := renderer.Result(&encoded, result); err != nil {
		return renderVersionError(streams, mode, asAppError(err))
	}
	if err := commitOutput(streams.Out, encoded.Bytes()); err != nil {
		return renderVersionError(streams, mode, asAppError(err))
	}
	return apperror.Exit(nil, nil)
}

func renderVersionError(streams IO, mode output.Mode, err *apperror.Error) int {
	renderer, rendererErr := output.NewFactory().New(mode, output.RenderOptions{})
	if rendererErr != nil {
		_, _ = fmt.Fprintln(streams.Err, "error: internal error")
		return apperror.ExitInternal
	}
	var encoded bytes.Buffer
	if writeErr := renderer.Error(&encoded, "version", err); writeErr != nil || commitOutput(streams.Err, encoded.Bytes()) != nil {
		_, _ = fmt.Fprintln(streams.Err, "error: internal error")
		return apperror.ExitInternal
	}
	return apperror.Exit(err, nil)
}
