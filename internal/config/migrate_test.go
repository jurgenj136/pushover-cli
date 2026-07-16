package config

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestLoadMigratesPrivateV0WithDurableBackup(t *testing.T) {
	path := "/config/config.yaml"
	original := []byte("schema_version: 0\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n")
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

func TestMigrationBackupCollisionAndCommitFailureKeepOriginal(t *testing.T) {
	path := "/config/config.yaml"
	original := []byte("schema_version: 0\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n")
	backup := "/config/config.yaml.v0.19700101T000000.123000000Z.bak"
	filesystem := newFakeFS()
	filesystem.contents[path] = original
	filesystem.contents[backup] = []byte("existing")
	_, err := NewFileStoreWith(path, filesystem, fixedClock{}).Load(context.Background())
	if err == nil || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("collision err = %v", err)
	}
	if got := string(filesystem.contents[path]); got != string(original) {
		t.Fatalf("original changed: %q", got)
	}
	if got := string(filesystem.contents[backup]); got != "existing" {
		t.Fatalf("existing backup changed: %q", got)
	}
}

func TestMigrationCommitFailureRetainsDurableBackup(t *testing.T) {
	path := "/config/config.yaml"
	original := []byte("schema_version: 0\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n")
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
