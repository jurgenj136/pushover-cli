package secrets

import (
	"context"
	"errors"
	"sync"

	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type nativeAdapter interface {
	Get(string) ([]byte, error)
	Put(string, []byte) error
	Delete(string) error
}

const nativeService = "pushover-cli"

var errNativeMissing = errors.New("native secret is missing")

// NativeStore is a small adapter around the OS credential service. Its trust
// boundary is the current OS user: same-user processes may access entries.
type NativeStore struct {
	mu      sync.Mutex
	adapter nativeAdapter
}

func newNativeStore(adapter nativeAdapter) *NativeStore { return &NativeStore{adapter: adapter} }
func (s *NativeStore) Get(ctx context.Context, ref domain.SecretRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.adapter.Get(string(ref))
	if errors.Is(err, errNativeMissing) {
		return nil, missing()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return copyBytes(value), nil
}
func (s *NativeStore) Put(ctx context.Context, value []byte) (domain.SecretRef, error) {
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
		if _, err := s.adapter.Get(string(ref)); err == nil {
			continue
		} else if !errors.Is(err, errNativeMissing) {
			return "", unavailable(err)
		}
		if err := s.adapter.Put(string(ref), copyBytes(value)); err != nil {
			return "", unavailable(err)
		}
		if err := ctx.Err(); err != nil {
			_ = s.adapter.Delete(string(ref))
			return "", err
		}
		return ref, nil
	}
}
func (s *NativeStore) Delete(ctx context.Context, ref domain.SecretRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.adapter.Delete(string(ref))
	if errors.Is(err, errNativeMissing) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	return ctx.Err()
}
func (s *NativeStore) Check(ctx context.Context, ref domain.SecretRef) error {
	_, err := s.Get(ctx, ref)
	return err
}

type NativeFactory struct{ Adapter nativeAdapter }

func (f NativeFactory) Open(ctx context.Context, _ config.Backend) (Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Adapter == nil {
		return nil, unavailable(errors.New("native adapter is not configured"))
	}
	return newNativeStore(f.Adapter), nil
}
