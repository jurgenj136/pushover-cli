package config

import (
	"context"
	"errors"
	"testing"
)

type transactionStore struct {
	value  Config
	commit Config
	err    error
}

func (s *transactionStore) Load(context.Context) (Config, error) { return s.value, nil }
func (s *transactionStore) Commit(_ context.Context, value Config) error {
	s.commit = value
	return s.err
}
func (s *transactionStore) Path() string { return "" }

func TestCloneAndTransactionNeverMutateLoadedConfig(t *testing.T) {
	before := Config{SchemaVersion: CurrentSchemaVersion, Backend: Backend{Type: "native", Metadata: map[string]string{"mode": "x"}}, Profiles: map[string]Profile{"p": {Name: "p", TokenRef: "token", Defaults: MessageDefaults{Devices: []string{"one"}}}}, Recipients: map[string]Recipient{"r": {Alias: "r", UserKeyRef: "user", Devices: []string{"two"}}}}
	store := &transactionStore{value: before}
	after, err := Transaction(context.Background(), store, func(value *Config) error {
		value.Backend.Metadata["mode"] = "changed"
		profile := value.Profiles["p"]
		profile.Defaults.Devices[0] = "changed"
		value.Profiles["p"] = profile
		recipient := value.Recipients["r"]
		recipient.Devices[0] = "changed"
		value.Recipients["r"] = recipient
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if before.Backend.Metadata["mode"] != "x" || before.Profiles["p"].Defaults.Devices[0] != "one" || before.Recipients["r"].Devices[0] != "two" {
		t.Fatal("loaded config was mutated")
	}
	if after.Backend.Metadata["mode"] != "changed" || store.commit.Backend.Metadata["mode"] != "changed" {
		t.Fatal("mutation was not committed")
	}
}
func TestTransactionDoesNotCommitMutatorError(t *testing.T) {
	store := &transactionStore{value: Config{Profiles: map[string]Profile{}, Recipients: map[string]Recipient{}}}
	want := errors.New("stop")
	if _, err := Transaction(context.Background(), store, func(*Config) error { return want }); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
	if store.commit.Profiles != nil {
		t.Fatal("committed after mutation failure")
	}
}
