package config

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMigratesPrivateV0WithDurableBackup(t *testing.T) {
	path := "/config/config.yaml"
	original := privateV0Fixture(t)
	filesystem := newFakeFS()
	filesystem.contents[path] = original
	config, err := NewFileStoreWith(path, filesystem, fixedClock{}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("version = %d", config.SchemaVersion)
	}
	backup := "/config/config.yaml.v0.19700101T000000.123000000Z.bak"
	if got := string(filesystem.contents[backup]); got != string(original) {
		t.Fatalf("backup = %q", got)
	}
	if !strings.Contains(strings.Join(filesystem.events, "|"), "sync:"+backup+"|close:"+backup+"|syncdir|mkdir") {
		t.Fatalf("backup was not durable before commit: %v", filesystem.events)
	}
}

func TestLoadRefusesFutureOrMissingSchemaWithoutWrites(t *testing.T) {
	for _, document := range []string{
		"schema_version: 2\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"backend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: -1\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: -0\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: null\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: 0.1\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: 1.9\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: '1'\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: true\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: 0x1\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: 01\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
	} {
		filesystem := newFakeFS()
		filesystem.contents["/config/config.yaml"] = []byte(document)
		if _, err := NewFileStoreWith("/config/config.yaml", filesystem, fixedClock{}).Load(context.Background()); err == nil {
			t.Fatalf("accepted %q", document)
		}
		if got := strings.Join(filesystem.events, "|"); got != "open|close:/config/config.yaml" {
			t.Fatalf("wrote while refusing schema: %s", got)
		}
	}
}

func TestLoadMigratesPrivateV0PastBackupNameCollision(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	original := privateV0Fixture(t)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(directory, "config.yaml.v0.19700101T000000.123000000Z.bak")
	if err := os.WriteFile(backup, []byte("existing backup"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := NewFileStoreWith(path, osFileSystem{}, fixedClock{}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version = %d", config.SchemaVersion)
	}
	if got, err := os.ReadFile(backup); err != nil || string(got) != "existing backup" {
		t.Fatalf("colliding backup = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(backup + ".1"); err != nil || string(got) != string(original) {
		t.Fatalf("replacement backup = %q, err=%v", got, err)
	}
	temporary := filepath.Join(directory, ".config.yaml.123000000.tmp")
	if _, err := os.Stat(temporary); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("migration temporary remains: %v", err)
	}
}

func TestMigrationCommitFailureRetainsDurableBackup(t *testing.T) {
	path := "/config/config.yaml"
	original := privateV0Fixture(t)
	backup := "/config/config.yaml.v0.19700101T000000.123000000Z.bak"
	filesystem := newFakeFS()
	filesystem.contents[path] = original
	filesystem.fail["rename"] = errors.New("replace failed")
	_, err := NewFileStoreWith(path, filesystem, fixedClock{}).Load(context.Background())
	if err == nil || OutcomeOf(err) != Unchanged {
		t.Fatalf("migration error = %v", err)
	}
	if got := string(filesystem.contents[path]); got != string(original) {
		t.Fatalf("original changed: %q", got)
	}
	if got := string(filesystem.contents[backup]); got != string(original) {
		t.Fatalf("durable backup missing: %q", got)
	}
}

func privateV0Fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/v0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
