package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/buildinfo"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

func TestNewProductionDependenciesLeavesAdminRuntimeInert(t *testing.T) {
	deps, err := NewProductionDependencies(context.Background(), IO{})
	if err != nil {
		t.Fatal(err)
	}
	if deps.Admin.Settings != nil || deps.Admin.Config != nil || deps.Admin.SecretFactory != nil || deps.Admin.BackendMigrator != nil || deps.Admin.API != nil || deps.Admin.Build != (buildinfo.Info{}) {
		t.Fatalf("NewProductionDependencies initialized admin dependencies: %#v", deps.Admin)
	}
	if deps.admin == nil {
		t.Fatal("NewProductionDependencies did not install an inert admin factory")
	}
}

func TestOpenAdminPrefersInjectedDependencies(t *testing.T) {
	store := &adminStore{}
	want := AdminDependencies{Config: store, Build: buildinfo.Info{Version: "test"}}
	calls := 0
	deps := Dependencies{Admin: want, admin: func(context.Context) (AdminDependencies, error) {
		calls++
		return AdminDependencies{}, errors.New("production factory must not run")
	}}

	got, err := deps.openAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Config != want.Config || got.Build != want.Build {
		t.Fatalf("admin dependencies = %#v, want injected %#v", got, want)
	}
	if calls != 0 {
		t.Fatalf("production factory called %d times", calls)
	}
}

func TestOpenAdminSettingsDoesNotOpenSecretsWhenConfigurationIsMissing(t *testing.T) {
	store := &adminStore{loadErr: config.ErrNotFound}
	factory := &adminSecretFactory{}
	deps := Dependencies{Admin: AdminDependencies{Config: store, SecretFactory: factory}}

	_, err := deps.openAdminSettings(context.Background())
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("err = %v, want missing configuration", err)
	}
	if factory.opens != 0 {
		t.Fatalf("secret factory opened %d stores for missing config", factory.opens)
	}
}

func TestValidateAdminEnvironmentRejectsPartialCredentialsWithoutValues(t *testing.T) {
	secret := "do-not-disclose"
	deps := Dependencies{lookupEnv: func(name string) (string, bool) {
		return secret, name == "PUSHOVER_TOKEN"
	}}

	err := deps.validateAdminEnvironment()
	if err == nil {
		t.Fatal("partial credentials were accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked credential: %q", err)
	}
	if !strings.Contains(err.Error(), "together") {
		t.Fatalf("error is not actionable: %q", err)
	}
}

type adminStore struct {
	value   config.Config
	loadErr error
}

func (s *adminStore) Load(context.Context) (config.Config, error) { return s.value, s.loadErr }
func (*adminStore) Commit(context.Context, config.Config) error {
	return errors.New("unexpected config write")
}
func (*adminStore) Path() string { return "memory" }

type adminSecretFactory struct{ opens int }

func (f *adminSecretFactory) Open(context.Context, config.Backend) (secrets.Store, error) {
	f.opens++
	return secrets.NewMemoryStore(), nil
}
