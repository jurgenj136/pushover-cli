package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"filippo.io/age"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const ageFileName = "secrets.age"
const identityFileName = "identity.txt"
const maxSecretMapBytes = 1 << 20

// An age header, stanza, and stream framing are small relative to our map
// limit. The conservative 64KiB allowance bounds malformed input allocation.
const maxEncryptedMapBytes = maxSecretMapBytes + 64<<10

type passphraseProvider func(context.Context) ([]byte, error)

// AgeFactory opens the explicit encrypted-file fallback. Passphrase is called
// on every operation and is never retained. Identity mode is only available
// after the caller explicitly authorizes persisted-identity use.
type AgeFactory struct {
	DataDir       string
	Passphrase    func(context.Context) ([]byte, error)
	AllowIdentity bool
	fs            ageFilesystem
}

// ageFilesystem is an I/O seam for atomic-write failure tests. Production
// uses osAgeFilesystem; it is deliberately not exported as a second API.
type ageFile interface {
	io.Writer
	Sync() error
	Close() error
	Chmod(os.FileMode) error
	Name() string
}
type ageDirectory interface {
	Sync() error
	Close() error
}
type ageFilesystem interface {
	ReadFileLimited(string, int) ([]byte, error)
	WriteFile(string, []byte, os.FileMode) error
	MkdirAll(string, os.FileMode) error
	Chmod(string, os.FileMode) error
	CreateTemp(string, string) (ageFile, error)
	Rename(string, string) error
	Remove(string) error
	Open(string) (ageDirectory, error)
}
type osAgeFilesystem struct{}

func (osAgeFilesystem) ReadFileLimited(path string, limit int) ([]byte, error) {
	// #nosec G304 -- path is the application-owned secret-store path selected
	// from validated configuration, not an HTTP or message input.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		wipe(data)
		return nil, errTooLarge
	}
	return data, nil
}
func (osAgeFilesystem) WriteFile(path string, value []byte, mode os.FileMode) error {
	return os.WriteFile(path, value, mode)
}
func (osAgeFilesystem) MkdirAll(path string, mode os.FileMode) error { return os.MkdirAll(path, mode) }
func (osAgeFilesystem) Chmod(path string, mode os.FileMode) error    { return os.Chmod(path, mode) }
func (osAgeFilesystem) CreateTemp(path, pattern string) (ageFile, error) {
	return os.CreateTemp(path, pattern)
}
func (osAgeFilesystem) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (osAgeFilesystem) Remove(path string) error             { return os.Remove(path) }
func (osAgeFilesystem) Open(path string) (ageDirectory, error) {
	// #nosec G304 -- callers provide the validated application data directory.
	return os.Open(path)
}

func (f AgeFactory) Open(ctx context.Context, backend config.Backend) (Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if backend.Type != "file" {
		return nil, unavailable(errors.New("wrong backend type"))
	}
	mode := backend.Metadata["mode"]
	if mode != "passphrase" && mode != "identity" {
		return nil, unavailable(errors.New("unsupported file mode"))
	}
	if f.DataDir == "" {
		return nil, unavailable(errors.New("file data path is not configured"))
	}
	if mode == "identity" && !f.AllowIdentity {
		return nil, unavailable(errors.New("persisted identity was not authorized"))
	}
	if mode == "passphrase" && f.Passphrase == nil {
		return nil, unavailable(errors.New("passphrase provider is not configured"))
	}
	filesystem := f.fs
	if filesystem == nil {
		filesystem = osAgeFilesystem{}
	}
	if err := filesystem.MkdirAll(f.DataDir, 0o700); err != nil {
		return nil, unavailable(err)
	}
	if err := filesystem.Chmod(f.DataDir, 0o700); err != nil {
		return nil, unavailable(err)
	}
	if err := protectAgePath(f.DataDir); err != nil {
		return nil, unavailable(err)
	}
	return &AgeStore{path: filepath.Join(f.DataDir, ageFileName), identityPath: filepath.Join(f.DataDir, identityFileName), mode: mode, passphrase: f.Passphrase, fs: filesystem}, nil
}

// AgeStore encrypts one versioned ref-to-secret map. It has an intentionally
// single-writer cross-process contract; in-process calls are serialized.
type AgeStore struct {
	mu                       sync.Mutex
	path, identityPath, mode string
	passphrase               passphraseProvider
	fs                       ageFilesystem
}
type ageEnvelope struct {
	Version int               `json:"version"`
	Values  map[string][]byte `json:"values"`
}

// ageDurabilityError means a rename completed but its directory flush did not.
// Callers retain the returned ref and must reconcile configuration before any
// retry; the store never silently hides the uncertainty.
type ageDurabilityError struct{ cause error }

func (e *ageDurabilityError) Error() string { return "secret storage durability is unknown" }
func (e *ageDurabilityError) Unwrap() error { return e.cause }
func (e *ageDurabilityError) Format(state fmt.State, verb rune) {
	formatRedactedError(state, verb, e.Error())
}

type identityDurabilityError struct{ cause error }

func (e *identityDurabilityError) Error() string { return "identity storage durability is unknown" }
func (e *identityDurabilityError) Unwrap() error { return e.cause }
func (e *identityDurabilityError) Format(state fmt.State, verb rune) {
	formatRedactedError(state, verb, e.Error())
}

func formatRedactedError(state fmt.State, verb rune, message string) {
	if verb == 'q' {
		_, _ = fmt.Fprintf(state, "%q", message)
		return
	}
	_, _ = io.WriteString(state, message)
}

var errTooLarge = errors.New("secret storage exceeds size limit")

func (s *AgeStore) Get(ctx context.Context, ref domain.SecretRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	value, ok := values[string(ref)]
	if !ok {
		return nil, missing()
	}
	return copyBytes(value), nil
}
func (s *AgeStore) Check(ctx context.Context, ref domain.SecretRef) error {
	_, err := s.Get(ctx, ref)
	return err
}
func (s *AgeStore) Put(ctx context.Context, value []byte) (domain.SecretRef, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	for {
		ref, err := newRef()
		if err != nil {
			return "", unavailable(err)
		}
		if _, exists := values[string(ref)]; exists {
			continue
		}
		values[string(ref)] = copyBytes(value)
		if err := s.save(ctx, values); err != nil {
			var durability *ageDurabilityError
			if errors.As(err, &durability) {
				return ref, err
			}
			return "", err
		}
		return ref, nil
	}
}
func (s *AgeStore) Delete(ctx context.Context, ref domain.SecretRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load(ctx)
	if err != nil {
		return err
	}
	if _, exists := values[string(ref)]; !exists {
		return nil
	}
	delete(values, string(ref))
	return s.save(ctx, values)
}

func (s *AgeStore) load(ctx context.Context) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.hardenExisting(s.path); err != nil {
		return nil, err
	}
	data, err := s.fs.ReadFileLimited(s.path, maxEncryptedMapBytes)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string][]byte), nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	identity, _, clearCredentials, err := s.credentials(ctx)
	if err != nil {
		return nil, err
	}
	defer clearCredentials()
	reader, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, unavailable(err)
	}
	plain, err := io.ReadAll(io.LimitReader(reader, maxSecretMapBytes+1))
	if err != nil {
		return nil, unavailable(err)
	}
	defer wipe(plain)
	if len(plain) > maxSecretMapBytes {
		return nil, unavailable(errors.New("encrypted secret map exceeds limit"))
	}
	var envelope ageEnvelope
	if err := json.Unmarshal(plain, &envelope); err != nil || envelope.Version != 1 || envelope.Values == nil {
		return nil, unavailable(errors.New("corrupt encrypted secret map"))
	}
	return envelope.Values, nil
}
func (s *AgeStore) save(ctx context.Context, values map[string][]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, recipient, clearCredentials, err := s.credentials(ctx)
	if err != nil {
		return err
	}
	defer clearCredentials()
	plain, err := json.Marshal(ageEnvelope{Version: 1, Values: values})
	if err != nil {
		return unavailable(err)
	}
	defer wipe(plain)
	if len(plain) > maxSecretMapBytes {
		return unavailable(errTooLarge)
	}
	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		return unavailable(err)
	}
	if err := writeAllAge(writer, plain); err != nil {
		_ = writer.Close()
		return unavailable(err)
	}
	if err := writer.Close(); err != nil {
		return unavailable(err)
	}
	if err := s.fs.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return unavailable(err)
	}
	if err := s.fs.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return unavailable(err)
	}
	temporary, err := s.fs.CreateTemp(filepath.Dir(s.path), ".secrets-*.tmp")
	if err != nil {
		return unavailable(err)
	}
	temporaryName := temporary.Name()
	failed := func(cause error) error {
		_ = temporary.Close()
		_ = s.fs.Remove(temporaryName)
		return unavailable(cause)
	}
	if err := temporary.Chmod(0o600); err != nil {
		return failed(err)
	}
	if err := protectAgePath(temporaryName); err != nil {
		return failed(err)
	}
	if encrypted.Len() > maxEncryptedMapBytes {
		return failed(errTooLarge)
	}
	if err := writeAllAge(temporary, encrypted.Bytes()); err != nil {
		return failed(err)
	}
	if err := temporary.Sync(); err != nil {
		return failed(err)
	}
	if err := temporary.Close(); err != nil {
		return failed(err)
	}
	if err := ctx.Err(); err != nil {
		_ = s.fs.Remove(temporaryName)
		return err
	}
	if err := s.fs.Rename(temporaryName, s.path); err != nil {
		_ = s.fs.Remove(temporaryName)
		return unavailable(err)
	}
	if err := protectAgePath(s.path); err != nil {
		return &ageDurabilityError{cause: err}
	}
	directory, err := s.fs.Open(filepath.Dir(s.path))
	if err != nil {
		return &ageDurabilityError{cause: err}
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return &ageDurabilityError{cause: err}
	}
	if err := directory.Close(); err != nil {
		return &ageDurabilityError{cause: err}
	}
	return nil
}

func (s *AgeStore) credentials(ctx context.Context) (age.Identity, age.Recipient, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, func() {}, err
	}
	if s.mode == "passphrase" {
		secret, err := s.passphrase(ctx)
		if err != nil {
			return nil, nil, func() {}, unavailable(err)
		}
		if len(secret) == 0 {
			return nil, nil, func() { wipe(secret) }, unavailable(errors.New("empty passphrase"))
		}
		passphrase := string(secret)
		wipe(secret)
		identity, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return nil, nil, func() {}, unavailable(err)
		}
		recipient, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return nil, nil, func() {}, unavailable(err)
		}
		identity.SetMaxWorkFactor(18)
		return identity, recipient, func() {}, nil
	}
	if err := s.hardenExisting(s.identityPath); err != nil {
		return nil, nil, func() {}, err
	}
	data, err := s.fs.ReadFileLimited(s.identityPath, 1024)
	if errors.Is(err, os.ErrNotExist) {
		// Identity creation is reachable only after AgeFactory accepted the
		// caller's explicit AllowIdentity authorization and identity mode.
		generated, generateErr := age.GenerateX25519Identity()
		if generateErr != nil {
			return nil, nil, func() {}, unavailable(generateErr)
		}
		data = []byte(generated.String())
		if writeErr := s.writePrivate(ctx, s.identityPath, data); writeErr != nil {
			wipe(data)
			return nil, nil, func() {}, unavailable(writeErr)
		}
	} else if err != nil {
		return nil, nil, func() {}, unavailable(err)
	}
	defer wipe(data)
	identity, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, nil, func() {}, unavailable(err)
	}
	return identity, identity.Recipient(), func() {}, nil
}

func (s *AgeStore) writePrivate(ctx context.Context, path string, value []byte) error {
	temporary, err := s.fs.CreateTemp(filepath.Dir(path), ".identity-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	fail := func(cause error) error { _ = temporary.Close(); _ = s.fs.Remove(temporaryName); return cause }
	if err := temporary.Chmod(0o600); err != nil {
		return fail(err)
	}
	if err := protectAgePath(temporaryName); err != nil {
		return fail(err)
	}
	if err := writeAllAge(temporary, value); err != nil {
		return fail(err)
	}
	if err := temporary.Sync(); err != nil {
		return fail(err)
	}
	if err := temporary.Close(); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		_ = s.fs.Remove(temporaryName)
		return err
	}
	if err := s.fs.Rename(temporaryName, path); err != nil {
		_ = s.fs.Remove(temporaryName)
		return err
	}
	if err := protectAgePath(path); err != nil {
		return &identityDurabilityError{cause: err}
	}
	directory, err := s.fs.Open(filepath.Dir(path))
	if err != nil {
		return &identityDurabilityError{cause: err}
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return &identityDurabilityError{cause: err}
	}
	if err := directory.Close(); err != nil {
		return &identityDurabilityError{cause: err}
	}
	return nil
}

// hardenExisting upgrades existing private material before it is parsed. A
// missing path is normal on first use; every other hardening failure blocks use.
func (s *AgeStore) hardenExisting(path string) error {
	err := s.fs.Chmod(path, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	if err := protectAgePath(path); err != nil {
		return unavailable(err)
	}
	return nil
}

func writeAllAge(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		count, err := writer.Write(value)
		if count > 0 {
			value = value[count:]
		}
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
