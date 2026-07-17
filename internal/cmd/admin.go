package cmd

import (
	"context"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/buildinfo"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

type adminFactory func(context.Context) (AdminDependencies, error)

// openAdmin returns injected dependencies unchanged. Production construction
// is deferred until an admin command reaches the point where it needs them.
func (d Dependencies) openAdmin(ctx context.Context) (AdminDependencies, error) {
	if hasAdminDependencies(d.Admin) {
		return d.Admin, nil
	}
	if d.admin == nil {
		return AdminDependencies{}, apperror.New("admin_unavailable", apperror.CategoryInternal, apperror.MessageInternal)
	}
	return d.admin(ctx)
}

func hasAdminDependencies(value AdminDependencies) bool {
	return value.Settings != nil || value.Config != nil || value.SecretFactory != nil || value.BackendMigrator != nil || value.API != nil || value.Build != (buildinfo.Info{})
}

// loadAdminConfig deliberately does not open the selected secret store. Auth
// status uses it to report a missing configuration without touching secrets.
func (d Dependencies) loadAdminConfig(ctx context.Context) (config.Config, error) {
	admin, err := d.openAdmin(ctx)
	if err != nil {
		return config.Config{}, err
	}
	if admin.Config == nil {
		return config.Config{}, apperror.New("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable")
	}
	return admin.Config.Load(ctx)
}

// openAdminSettings is the explicit persistence boundary. It loads selected
// safe configuration metadata before opening its secret backend, and never
// opens a store when configuration is missing or invalid.
func (d Dependencies) openAdminSettings(ctx context.Context) (settings.Service, error) {
	admin, err := d.openAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if admin.Settings != nil {
		return admin.Settings, nil
	}
	if admin.Config == nil || admin.SecretFactory == nil {
		return nil, apperror.New("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable")
	}
	selected, err := admin.Config.Load(ctx)
	if err != nil {
		return nil, err
	}
	store, err := admin.SecretFactory.Open(ctx, selected.Backend)
	if err != nil {
		return nil, err
	}
	return &settings.Manager{Config: admin.Config, Secrets: store}, nil
}

// validateAdminEnvironment detects a stale half of the ephemeral credential
// pair before an admin command loads configuration or makes a network call.
func (d Dependencies) validateAdminEnvironment() error {
	_, tokenSet := d.environment("PUSHOVER_TOKEN")
	_, userKeySet := d.environment("PUSHOVER_USER_KEY")
	if tokenSet != userKeySet {
		return apperror.New("environment_credentials_incomplete", apperror.CategoryUsage, "PUSHOVER_TOKEN and PUSHOVER_USER_KEY must be supplied together or both removed")
	}
	return nil
}

func productionAdminFactory(prompter Prompter) adminFactory {
	return func(ctx context.Context) (AdminDependencies, error) {
		if err := ctx.Err(); err != nil {
			return AdminDependencies{}, err
		}
		paths, err := config.DefaultPaths()
		if err != nil {
			return AdminDependencies{}, apperror.Wrap("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable", err)
		}
		configuration := config.NewFileStore(paths.ConfigFile)
		factory := secrets.DefaultFactory{File: secrets.AgeFactory{
			DataDir:       paths.AgeDataDir,
			Passphrase:    promptPassphrase(prompter),
			AllowIdentity: true,
		}}
		migrator := &secrets.BackendMigrator{Config: configuration, Factory: factory}
		return AdminDependencies{
			Config:          configuration,
			SecretFactory:   factory,
			BackendMigrator: migrator,
			API:             pushover.NewClient(nil),
			Build:           buildinfo.Current(),
		}, nil
	}
}

// adminSettingsForConfig is a small handler seam for paths that already
// loaded config and must now persist through its selected secret backend.
func adminSettingsForConfig(ctx context.Context, admin AdminDependencies, selected config.Config) (settings.Service, error) {
	if admin.Settings != nil {
		return admin.Settings, nil
	}
	if admin.Config == nil || admin.SecretFactory == nil {
		return nil, apperror.New("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable")
	}
	store, err := admin.SecretFactory.Open(ctx, selected.Backend)
	if err != nil {
		return nil, err
	}
	return &settings.Manager{Config: admin.Config, Secrets: store}, nil
}
