// Package secrets provides secret stores whose public API exposes only opaque
// references. Stores are intentionally single-writer across processes; each
// implementation protects concurrent callers in one process.
package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"sync"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

// Store retains secret bytes behind opaque, randomly generated references.
type Store interface {
	Get(context.Context, domain.SecretRef) ([]byte, error)
	Put(context.Context, []byte) (domain.SecretRef, error)
	Delete(context.Context, domain.SecretRef) error
	Check(context.Context, domain.SecretRef) error
}

// Factory opens the store selected by safe configuration metadata.
type Factory interface {
	Open(context.Context, config.Backend) (Store, error)
}

var errMissing = errors.New("secret reference is unavailable")

func unavailable(cause error) error {
	return apperror.Wrap("secret_store_unavailable", apperror.CategoryConfig, "secret store unavailable", cause)
}
func missing() error {
	return apperror.Wrap("secret_reference_missing", apperror.CategoryConfig, "required secret is unavailable", errMissing)
}

func newRef() (domain.SecretRef, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Raw base32 is letters and digits only after stripping padding, satisfying
	// config's safe-reference validator without encoding meaning or labels.
	return domain.SecretRef(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes)), nil
}

func copyBytes(value []byte) []byte { return append([]byte(nil), value...) }

// MemoryStore is a hermetic contract-test fake. It is not selected by the
// production factory and never persists values.
type MemoryStore struct {
	mu     sync.Mutex
	values map[domain.SecretRef][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{values: make(map[domain.SecretRef][]byte)} }
func (s *MemoryStore) Get(ctx context.Context, ref domain.SecretRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[ref]
	if !ok {
		return nil, missing()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return copyBytes(value), nil
}
func (s *MemoryStore) Put(ctx context.Context, value []byte) (domain.SecretRef, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		ref, err := newRef()
		if err != nil {
			return "", unavailable(err)
		}
		if _, used := s.values[ref]; used {
			continue
		}
		s.values[ref] = copyBytes(value)
		return ref, nil
	}
}
func (s *MemoryStore) Delete(ctx context.Context, ref domain.SecretRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, ref)
	return nil
}
func (s *MemoryStore) Check(ctx context.Context, ref domain.SecretRef) error {
	_, err := s.Get(ctx, ref)
	return err
}

// DefaultFactory selects the native store or the explicit age file fallback.
// Dependency injection keeps platform and prompt access out of unit tests.
type DefaultFactory struct {
	Native Factory
	File   Factory
}

func (f DefaultFactory) Open(ctx context.Context, backend config.Backend) (Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch backend.Type {
	case "native":
		if f.Native != nil {
			return f.Native.Open(ctx, backend)
		}
		return openNative(ctx, backend)
	case "file":
		if f.File == nil {
			return nil, unavailable(errors.New("file backend factory is not configured"))
		}
		return f.File.Open(ctx, backend)
	default:
		return nil, unavailable(errors.New("unsupported backend"))
	}
}
