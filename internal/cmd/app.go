package cmd

import (
	"context"
	"os"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/attachment"
	"github.com/jurgenj136/pushover-cli/internal/buildinfo"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/delivery"
	"github.com/jurgenj136/pushover-cli/internal/e2ee"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/resolver"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

// AdminDependencies is the injectable runtime graph for setup, profile,
// recipient, auth, and introspection commands. Production values are made
// lazily so help, completion, and version remain side-effect-free.
type AdminDependencies struct {
	Settings        settings.Service
	Config          config.Store
	SecretFactory   secrets.Factory
	BackendMigrator *secrets.BackendMigrator
	API             pushover.Client
	Build           buildinfo.Info
}

// Dependencies are the command's runtime graph. Tests may inject concrete
// preflight and delivery implementations; production construction remains
// intentionally lazy for help and completion.
type Dependencies struct {
	Preflight  *preflight.Builder
	Delivery   *delivery.Runner
	Output     output.Factory
	Operations operation.Factory
	Signals    <-chan os.Signal
	Admin      AdminDependencies
	Prompter   Prompter

	// runtime and lookupEnv deliberately remain package-private. They allow
	// production construction to be genuinely lazy and keep tests hermetic
	// without changing the public dependency contract.
	runtime   runtimeFactory
	admin     adminFactory
	lookupEnv func(string) (string, bool)
}

type runtimeFactory func(context.Context, operation.Scope) (preflight.Builder, delivery.Runner, error)

// NewProductionDependencies creates only inert factories. In particular it
// does not resolve paths, load configuration, open a secret store, or create a
// transport. Those effects are reserved for a parsed and structurally valid
// command that actually needs to send.
func NewProductionDependencies(_ context.Context, streams IO) (Dependencies, error) {
	streams = normalizeIO(streams)
	prompter := TerminalPrompter{Input: streams.In, Stderr: streams.Err, IsTTY: terminalInput(streams)}
	return Dependencies{
		Output:     output.NewFactory(),
		Operations: operation.Factory{},
		runtime:    productionRuntime,
		Prompter:   prompter,
		admin:      productionAdminFactory(prompter),
		lookupEnv:  os.LookupEnv,
	}, nil
}

func (d Dependencies) openRuntime(ctx context.Context, scope operation.Scope) (preflight.Builder, delivery.Runner, error) {
	if d.Preflight != nil || d.Delivery != nil {
		var builder preflight.Builder
		var runner delivery.Runner
		if d.Preflight != nil {
			builder = *d.Preflight
		}
		if d.Delivery != nil {
			runner = *d.Delivery
		}
		return builder, runner, nil
	}
	if d.runtime == nil {
		return preflight.Builder{}, delivery.Runner{}, nil
	}
	return d.runtime(ctx, scope)
}

func (d Dependencies) openSendRuntime(ctx context.Context, scope operation.Scope, request preflight.Request) (preflight.Builder, delivery.Runner, error) {
	if d.Preflight == nil && d.Delivery == nil && suppliedCredentials(request.Resolve) {
		builder := ephemeralRuntime(scope, request.DryRun)
		return builder, delivery.Runner{Client: builder.Validator, Scope: scope}, nil
	}
	return d.openRuntime(ctx, scope)
}

func suppliedCredentials(input resolver.Input) bool {
	return input.EnvTokenSet || input.EnvUserKeySet || input.Credentials != nil
}

func ephemeralRuntime(scope operation.Scope, dryRun bool) preflight.Builder {
	builder := preflight.Builder{
		Resolver:    &resolver.Resolver{},
		Attachments: attachment.RemoteLoader{Scope: scope},
		Encryptor:   e2ee.Encryptor{},
		Scope:       scope,
	}
	if !dryRun {
		builder.Validator = pushover.NewClient(nil)
	}
	return builder
}

func (d Dependencies) environment(name string) (string, bool) {
	if d.lookupEnv != nil {
		return d.lookupEnv(name)
	}
	return os.LookupEnv(name)
}

func productionRuntime(ctx context.Context, scope operation.Scope) (preflight.Builder, delivery.Runner, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return preflight.Builder{}, delivery.Runner{}, apperror.Wrap("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable", err)
	}
	configuration := config.NewFileStore(paths.ConfigFile)
	value, err := configuration.Load(ctx)
	if err != nil {
		return preflight.Builder{}, delivery.Runner{}, apperror.Wrap("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable", err)
	}
	store, err := (secrets.DefaultFactory{
		File: secrets.AgeFactory{DataDir: paths.AgeDataDir, AllowIdentity: true},
	}).Open(ctx, value.Backend)
	if err != nil {
		return preflight.Builder{}, delivery.Runner{}, err
	}
	client := pushover.NewClient(nil)
	builder := preflight.Builder{
		Resolver:    &resolver.Resolver{Config: configuration, Secrets: store},
		Attachments: attachment.RemoteLoader{Scope: scope},
		Validator:   client,
		Encryptor:   e2ee.Encryptor{},
		Scope:       scope,
	}
	runner := delivery.Runner{Client: client, Scope: scope}
	return builder, runner, nil
}
