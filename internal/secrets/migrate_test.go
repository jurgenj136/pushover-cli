package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type configFake struct {
	value     config.Config
	commitErr error
}

func (f *configFake) Load(context.Context) (config.Config, error) { return f.value, nil }
func (f *configFake) Commit(_ context.Context, value config.Config) error {
	if f.commitErr == nil {
		f.value = value
	}
	return f.commitErr
}
func (f *configFake) Path() string { return "test" }

type factoryFake map[string]Store

func (f factoryFake) Open(_ context.Context, backend config.Backend) (Store, error) {
	store := f[backend.Type]
	if store == nil {
		return nil, errors.New("no store")
	}
	return store, nil
}

type durabilityPutStore struct{ *MemoryStore }

func (s durabilityPutStore) Put(ctx context.Context, value []byte) (domain.SecretRef, error) {
	ref, err := s.MemoryStore.Put(ctx, value)
	if err != nil {
		return ref, err
	}
	return ref, &ageDurabilityError{cause: errors.New("directory sync")}
}

func migrationConfig(token, user domain.SecretRef) config.Config {
	return config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: config.Backend{Type: "native"}, Profiles: map[string]config.Profile{"main": {Name: "main", TokenRef: token}}, Recipients: map[string]config.Recipient{"ops": {Alias: "ops", UserKeyRef: user, Kind: domain.KindUser}}}
}
func TestMigrationCopiesThenSwitchesThenCleans(t *testing.T) {
	old, target := NewMemoryStore(), NewMemoryStore()
	token, _ := old.Put(context.Background(), []byte("token"))
	user, _ := old.Put(context.Background(), []byte("user"))
	cfg := &configFake{value: migrationConfig(token, user)}
	result, err := (BackendMigrator{Config: cfg, Factory: factoryFake{"native": old, "file": target}}).Migrate(context.Background(), config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}})
	if err != nil || result.CleanupWarning != nil || cfg.value.Backend.Type != "file" {
		t.Fatalf("Migrate=%#v %v %#v", result, err, cfg.value.Backend)
	}
	if _, err := old.Get(context.Background(), token); err == nil {
		t.Fatal("old secret remained")
	}
	if value, err := target.Get(context.Background(), cfg.value.Profiles["main"].TokenRef); err != nil || string(value) != "token" {
		t.Fatalf("new token %q %v", value, err)
	}
}
func TestMigrationCommitFailureRollsBackStagedSecrets(t *testing.T) {
	old, target := NewMemoryStore(), NewMemoryStore()
	token, _ := old.Put(context.Background(), []byte("token"))
	user, _ := old.Put(context.Background(), []byte("user"))
	cfg := &configFake{value: migrationConfig(token, user), commitErr: config.NewCommitError(config.Unchanged, errors.New("disk"))}
	_, err := (BackendMigrator{Config: cfg, Factory: factoryFake{"native": old, "file": target}}).Migrate(context.Background(), config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}})
	if err == nil || cfg.value.Backend.Type != "native" {
		t.Fatalf("error=%v config=%#v", err, cfg.value.Backend)
	}
	if len(target.values) != 0 {
		t.Fatalf("staged values remain: %d", len(target.values))
	}
}
func TestMigrationDurabilityRefIsRolledBackRatherThanOrphaned(t *testing.T) {
	old := NewMemoryStore()
	target := durabilityPutStore{NewMemoryStore()}
	token, _ := old.Put(context.Background(), []byte("token"))
	user, _ := old.Put(context.Background(), []byte("user"))
	cfg := &configFake{value: migrationConfig(token, user)}
	_, err := (BackendMigrator{Config: cfg, Factory: factoryFake{"native": old, "file": target}}).Migrate(context.Background(), config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}})
	if err == nil || cfg.value.Backend.Type != "native" {
		t.Fatalf("error=%v backend=%s", err, cfg.value.Backend.Type)
	}
	if len(target.values) != 0 {
		t.Fatalf("durability ref orphaned: %d", len(target.values))
	}
}
