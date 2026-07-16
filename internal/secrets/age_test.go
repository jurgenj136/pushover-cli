package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/config"
)

type renameFailFS struct {
	osAgeFilesystem
	err error
}

func (f renameFailFS) Rename(string, string) error { return f.err }

type syncFailDirectory struct{}

func (syncFailDirectory) Sync() error  { return os.ErrPermission }
func (syncFailDirectory) Close() error { return nil }

type syncFailFS struct{ osAgeFilesystem }

func (syncFailFS) Open(string) (ageDirectory, error) { return syncFailDirectory{}, nil }

func passphrase(_ context.Context) ([]byte, error) {
	return []byte("correct horse battery staple"), nil
}
func TestAgeStoreContract(t *testing.T) {
	storeContract(t, func(t *testing.T) Store {
		t.Helper()
		dir := t.TempDir()
		store, err := (AgeFactory{DataDir: dir, Passphrase: passphrase}).Open(context.Background(), config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}})
		if err != nil {
			t.Fatal(err)
		}
		return store
	})
}
func TestAgeStorePrivateFilesAndWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	backend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}
	store, err := (AgeFactory{DataDir: dir, Passphrase: passphrase}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), []byte("never-render-this-secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, filepath.Join(dir, ageFileName)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != map[bool]os.FileMode{true: 0o700, false: 0o600}[path == dir] {
			t.Fatalf("mode %s = %o", path, info.Mode().Perm())
		}
	}
	wrong := func(context.Context) ([]byte, error) { return []byte("wrong passphrase"), nil }
	other, err := (AgeFactory{DataDir: dir, Passphrase: wrong}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Get(context.Background(), ref); err == nil || strings.Contains(err.Error(), "never-render-this-secret") {
		t.Fatalf("wrong passphrase error = %v", err)
	}
}
func TestAgeStoreCorruptCiphertextIsRedacted(t *testing.T) {
	dir := t.TempDir()
	backend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}
	store, err := (AgeFactory{DataDir: dir, Passphrase: passphrase}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ageFileName), []byte("broken /private/secrets"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "ref"); err == nil || strings.Contains(err.Error(), "/private") {
		t.Fatalf("unsafe corrupt error: %v", err)
	}
}

func TestAgeIdentityModeCreatesOnlyAfterExplicitAuthorization(t *testing.T) {
	dir := t.TempDir()
	backend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "identity"}}
	if _, err := (AgeFactory{DataDir: dir}).Open(context.Background(), backend); err == nil {
		t.Fatal("identity mode opened without authorization")
	}
	store, err := (AgeFactory{DataDir: dir, AllowIdentity: true}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), []byte("identity-backed")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, identityFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity mode = %o", info.Mode().Perm())
	}
}

func TestAgeAtomicRenameFailurePreservesPriorCiphertext(t *testing.T) {
	dir := t.TempDir()
	backend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}
	seed, err := (AgeFactory{DataDir: dir, Passphrase: passphrase}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := seed.Put(context.Background(), []byte("before"))
	if err != nil {
		t.Fatal(err)
	}
	broken, err := (AgeFactory{DataDir: dir, Passphrase: passphrase, fs: renameFailFS{err: os.ErrPermission}}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Put(context.Background(), []byte("after")); err == nil {
		t.Fatal("rename failure succeeded")
	}
	value, err := seed.Get(context.Background(), ref)
	if err != nil || string(value) != "before" {
		t.Fatalf("prior ciphertext not preserved: %q %v", value, err)
	}
}
func TestAgeDirectorySyncFailureIsReturned(t *testing.T) {
	dir := t.TempDir()
	backend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}
	store, err := (AgeFactory{DataDir: dir, Passphrase: passphrase, fs: syncFailFS{}}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), []byte("value"))
	if err == nil || ref == "" {
		t.Fatal("directory sync failure succeeded")
	}
}
