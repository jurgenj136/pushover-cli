package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func TestAgeHardenExistingMapAndIdentityBeforeUse(t *testing.T) {
	dir := t.TempDir()
	passBackend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}
	store, err := (AgeFactory{DataDir: dir, Passphrase: passphrase}).Open(context.Background(), passBackend)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), []byte("map"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, ageFileName), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(dir, ageFileName)); info.Mode().Perm() != 0o600 {
		t.Fatalf("map mode=%o", info.Mode().Perm())
	}
	identityBackend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "identity"}}
	identityDir := t.TempDir()
	identity, err := (AgeFactory{DataDir: identityDir, AllowIdentity: true}).Open(context.Background(), identityBackend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Put(context.Background(), []byte("identity")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(identityDir, identityFileName), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Put(context.Background(), []byte("again")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(identityDir, identityFileName)); info.Mode().Perm() != 0o600 {
		t.Fatalf("identity mode=%o", info.Mode().Perm())
	}
}

func TestAgeRejectsOversizedPlaintextAndCiphertext(t *testing.T) {
	dir := t.TempDir()
	backend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}
	store, err := (AgeFactory{DataDir: dir, Passphrase: passphrase}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), make([]byte, maxSecretMapBytes)); err == nil {
		t.Fatal("oversized map persisted")
	}
	if _, err := os.Stat(filepath.Join(dir, ageFileName)); !os.IsNotExist(err) {
		t.Fatalf("oversized map created a file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ageFileName), make([]byte, maxEncryptedMapBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "missing"); err == nil {
		t.Fatal("oversized ciphertext was accepted")
	}
}

type chunkWriter struct {
	bytes.Buffer
	size int
}

func (w *chunkWriter) Write(value []byte) (int, error) {
	if len(value) > w.size {
		value = value[:w.size]
	}
	return w.Buffer.Write(value)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
func TestWriteAllAgeHandlesShortWrites(t *testing.T) {
	writer := &chunkWriter{size: 1}
	if err := writeAllAge(writer, []byte("abcdef")); err != nil || writer.String() != "abcdef" {
		t.Fatalf("short write handling: %q %v", writer.String(), err)
	}
	if err := writeAllAge(zeroWriter{}, []byte("x")); err != io.ErrShortWrite {
		t.Fatalf("zero write=%v", err)
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

func TestIdentityDurabilityFailureDoesNotReturnSecretRef(t *testing.T) {
	dir := t.TempDir()
	backend := config.Backend{Type: "file", Metadata: map[string]string{"mode": "identity"}}
	store, err := (AgeFactory{DataDir: dir, AllowIdentity: true, fs: syncFailFS{}}).Open(context.Background(), backend)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), []byte("value"))
	if err == nil || ref != "" {
		t.Fatalf("identity durability result ref=%q err=%v", ref, err)
	}
}

func TestDurabilityErrorsFormattingNeverLeaksWrappedCause(t *testing.T) {
	marker := "/Users/alice/.config/pushover-cli/secret-marker"
	cause := errors.New(marker)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "age",
			err:  &ageDurabilityError{cause: cause},
			want: "secret storage durability is unknown",
		},
		{
			name: "identity",
			err:  &identityDurabilityError{cause: cause},
			want: "identity storage durability is unknown",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, verb := range []string{"%v", "%+v", "%#v", "%q"} {
				rendered := fmt.Sprintf(verb, test.err)
				want := test.want
				if verb == "%q" {
					want = fmt.Sprintf("%q", want)
				}
				if rendered != want {
					t.Fatalf("format %s = %q, want %q", verb, rendered, want)
				}
				if strings.Contains(rendered, marker) {
					t.Fatalf("format %s leaked wrapped cause: %q", verb, rendered)
				}
			}
			if !errors.Is(test.err, cause) {
				t.Fatal("durability error must retain its wrapped cause")
			}
			if got := errors.Unwrap(test.err); got != cause {
				t.Fatalf("Unwrap() = %v, want %v", got, cause)
			}
		})
	}
}
