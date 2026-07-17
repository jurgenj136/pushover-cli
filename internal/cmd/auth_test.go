package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

func TestAuthStatusMissingConfigurationSucceedsWithoutOpeningSecrets(t *testing.T) {
	store := &authConfigStore{loadErr: config.ErrNotFound}
	factory := &authSecretFactory{}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--json", "auth", "status"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store, SecretFactory: factory}})
	if got != 0 || factory.opens != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"command":"auth status"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"configured":false`)) || !bytes.Contains(stdout.Bytes(), []byte(`"token_resolves":false`)) {
		t.Fatalf("exit=%d opens=%d stdout=%q stderr=%q", got, factory.opens, stdout.String(), stderr.String())
	}
}

func TestAuthStatusSelectsProfileAfterOpeningConfiguredBackendWithoutReadingSecret(t *testing.T) {
	store, factory := configuredAuthRuntime(t)
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--plain", "--profile", "other", "auth", "status"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store, SecretFactory: factory}})
	if got != 0 || factory.opens != 1 || !strings.HasPrefix(stdout.String(), "other\tnative\ttrue\ttrue\n") {
		t.Fatalf("exit=%d opens=%d output=%q stderr=%q", got, factory.opens, stdout.String(), stderr.String())
	}
	if factory.gets != 0 || factory.checks != 1 {
		t.Fatalf("secret access gets=%d checks=%d", factory.gets, factory.checks)
	}
}

func TestAuthBackendUsesOnlySafeConfigurationAndStructuredOutputs(t *testing.T) {
	store := &authConfigStore{value: authConfig()}
	factory := &authSecretFactory{}
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"--plain", "auth", "backend"}, want: "native\n"},
		{args: []string{"--json", "auth", "backend"}, want: `"result":{"backend":"native"}`},
	} {
		var stdout, stderr bytes.Buffer
		if got := Execute(context.Background(), test.args, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store, SecretFactory: factory}}); got != 0 || !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("args=%q exit=%d stdout=%q stderr=%q", test.args, got, stdout.String(), stderr.String())
		}
	}
	if factory.opens != 0 {
		t.Fatalf("backend inspection opened %d secret stores", factory.opens)
	}
}

func TestAuthBackendSetNormalizesDestinationAndMigratesOnce(t *testing.T) {
	store, factory := configuredAuthRuntime(t)
	migrator := &secrets.BackendMigrator{Config: store, Factory: factory}
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"--json", "auth", "backend", "set", "file"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{BackendMigrator: migrator}})
	if got != 0 || store.commits != 1 || store.value.Backend.Type != "file" || store.value.Backend.Metadata["mode"] != "passphrase" || !bytes.Contains(stdout.Bytes(), []byte(`"command":"auth backend set"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"resource":"backend"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"name":"file"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"action":"migrated"`)) {
		t.Fatalf("exit=%d commits=%d backend=%#v stdout=%q stderr=%q", got, store.commits, store.value.Backend, stdout.String(), stderr.String())
	}
	if factory.opens != 2 {
		t.Fatalf("migration factory opens=%d, want 2", factory.opens)
	}
}

func TestAuthBackendSetReportsCleanupWarningAsSuccessfulWarning(t *testing.T) {
	store, factory := configuredAuthRuntime(t)
	factory.source.deleteErr = errors.New("keychain unavailable")
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"auth", "backend", "set", "file"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{BackendMigrator: &secrets.BackendMigrator{Config: store, Factory: factory}}})
	if got != 0 || !strings.Contains(stderr.String(), "warning:") || strings.Contains(stderr.String(), setupToken) || !strings.Contains(stdout.String(), "backend file migrated") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
}

func TestAuthBackendSetRejectsPlainEnvironmentAndDeadlineBeforeMigration(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  context.Context
		args []string
		deps func(*authConfigStore, *authSecretFactory) Dependencies
		want int
	}{
		{name: "plain", ctx: context.Background(), args: []string{"--plain", "auth", "backend", "set", "native"}, deps: func(s *authConfigStore, f *authSecretFactory) Dependencies {
			return Dependencies{Admin: AdminDependencies{BackendMigrator: &secrets.BackendMigrator{Config: s, Factory: f}}}
		}, want: 2},
		{name: "environment", ctx: context.Background(), args: []string{"auth", "backend", "set", "native"}, deps: func(s *authConfigStore, f *authSecretFactory) Dependencies {
			return Dependencies{Admin: AdminDependencies{BackendMigrator: &secrets.BackendMigrator{Config: s, Factory: f}}, lookupEnv: func(name string) (string, bool) { return "secret", name == "PUSHOVER_TOKEN" }}
		}, want: 2},
		{name: "deadline", ctx: expiredAuthContext(), args: []string{"auth", "backend", "set", "native"}, deps: func(s *authConfigStore, f *authSecretFactory) Dependencies {
			return Dependencies{Admin: AdminDependencies{BackendMigrator: &secrets.BackendMigrator{Config: s, Factory: f}}}
		}, want: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, factory := configuredAuthRuntime(t)
			var stdout, stderr bytes.Buffer
			got := Execute(test.ctx, test.args, testIO(&stdout, &stderr), test.deps(store, factory))
			if got != test.want || store.commits != 0 || factory.opens != 0 {
				t.Fatalf("exit=%d commits=%d opens=%d stdout=%q stderr=%q", got, store.commits, factory.opens, stdout.String(), stderr.String())
			}
		})
	}
}

func TestAuthCommandsRejectPartialEnvironmentBeforeScopeOrConfiguration(t *testing.T) {
	for _, args := range [][]string{{"auth", "status"}, {"auth", "backend"}} {
		store := &authConfigStore{value: authConfig()}
		spy := &contextSpy{}
		var stdout, stderr bytes.Buffer
		got := Execute(context.Background(), args, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store}, Operations: testOperations(spy), lookupEnv: func(name string) (string, bool) { return "secret", name == "PUSHOVER_USER_KEY" }})
		if got != 2 || store.loads != 0 || spy.calls != 0 {
			t.Fatalf("args=%q exit=%d loads=%d scopes=%d", args, got, store.loads, spy.calls)
		}
	}
}

func TestAuthGrammarAndJSONErrorsUseExactPaths(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"--json", "auth"}, want: `"command":"auth"`},
		{args: []string{"--json", "auth", "backend", "set"}, want: `"command":"auth backend set"`},
		{args: []string{"--json", "auth", "backend", "set", "unknown"}, want: `"command":"auth backend set"`},
		{args: []string{"--json", "auth", "backend", "set", "native", "extra"}, want: `"command":"auth backend set"`},
		{args: []string{"--json", "auth", "backend", "migrate", "native"}, want: `"command":"auth backend"`},
		{args: []string{"--json", "auth", "backend", "show"}, want: `"command":"auth backend"`},
		{args: []string{"--json", "--plain", "auth", "backend", "set", "file"}, want: `"command":"auth backend set"`},
	} {
		var stdout, stderr bytes.Buffer
		if got := Execute(context.Background(), test.args, testIO(&stdout, &stderr), Dependencies{}); got != 2 || !bytes.Contains(stderr.Bytes(), []byte(test.want)) {
			t.Fatalf("args=%q exit=%d stderr=%q", test.args, got, stderr.String())
		}
	}
}

func TestAuthBackendGrammarAcceptsOnlyBareOrSetDestination(t *testing.T) {
	for _, args := range [][]string{{"auth", "backend"}, {"auth", "backend", "set", "native"}, {"auth", "backend", "set", "file"}} {
		var cli rootCommand
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.Parse(args); err != nil {
			t.Fatalf("approved args %q rejected: %v", args, err)
		}
	}
	for _, args := range [][]string{{"auth", "backend", "native"}, {"auth", "backend", "migrate", "file"}, {"auth", "backend", "set"}, {"auth", "backend", "set", "file", "extra"}} {
		var cli rootCommand
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.Parse(args); err == nil {
			t.Fatalf("unsupported args %q parsed successfully", args)
		}
	}
	var stdout, stderr bytes.Buffer
	if got := Execute(context.Background(), []string{"auth", "backend", "--help"}, testIO(&stdout, &stderr), Dependencies{}); got != 0 || !strings.Contains(stdout.String(), "set") || strings.Contains(stdout.String(), "show") {
		t.Fatalf("backend help exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
}

func expiredAuthContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

type authConfigStore struct {
	value   config.Config
	loadErr error
	loads   int
	commits int
}

func (s *authConfigStore) Load(context.Context) (config.Config, error) {
	s.loads++
	return s.value, s.loadErr
}
func (s *authConfigStore) Commit(_ context.Context, value config.Config) error {
	s.commits++
	s.value = value
	return nil
}
func (*authConfigStore) Path() string { return "memory" }

type authSecretFactory struct {
	source, target *authStore
	opens          int
	gets, checks   int
}

func (f *authSecretFactory) Open(_ context.Context, backend config.Backend) (secrets.Store, error) {
	f.opens++
	switch backend.Type {
	case "native":
		return f.source, nil
	case "file":
		return f.target, nil
	default:
		return nil, errors.New("unsupported backend")
	}
}

type authStore struct {
	store     *secrets.MemoryStore
	deleteErr error
	factory   *authSecretFactory
}

func (s *authStore) Get(ctx context.Context, ref domain.SecretRef) ([]byte, error) {
	if s.factory != nil {
		s.factory.gets++
	}
	return s.store.Get(ctx, ref)
}
func (s *authStore) Put(ctx context.Context, value []byte) (domain.SecretRef, error) {
	return s.store.Put(ctx, value)
}
func (s *authStore) Delete(ctx context.Context, ref domain.SecretRef) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.store.Delete(ctx, ref)
}
func (s *authStore) Check(ctx context.Context, ref domain.SecretRef) error {
	if s.factory != nil {
		s.factory.checks++
	}
	return s.store.Check(ctx, ref)
}

func configuredAuthRuntime(t *testing.T) (*authConfigStore, *authSecretFactory) {
	t.Helper()
	factory := &authSecretFactory{}
	factory.source = &authStore{store: secrets.NewMemoryStore(), factory: factory}
	factory.target = &authStore{store: secrets.NewMemoryStore(), factory: factory}
	profileRef, err := factory.source.Put(context.Background(), []byte(setupToken))
	if err != nil {
		t.Fatal(err)
	}
	otherRef, err := factory.source.Put(context.Background(), []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if err != nil {
		t.Fatal(err)
	}
	value := authConfig()
	value.Profiles["default"] = config.Profile{Name: "default", TokenRef: profileRef}
	value.Profiles["other"] = config.Profile{Name: "other", TokenRef: otherRef}
	return &authConfigStore{value: value}, factory
}

func authConfig() config.Config {
	return config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: config.Backend{Type: "native"}, DefaultProfile: "default", Profiles: map[string]config.Profile{}, Recipients: map[string]config.Recipient{}}
}
