//go:build linux || windows

package secrets

import (
	"context"
	"errors"

	"github.com/jurgenj136/pushover-cli/internal/config"
	keyring "github.com/zalando/go-keyring"
)

type keyringAdapter struct{}

func (keyringAdapter) Get(ref string) ([]byte, error) {
	value, err := keyring.Get(nativeService, ref)
	if errors.Is(err, keyring.ErrNotFound) {
		err = errNativeMissing
	}
	return []byte(value), err
}
func (keyringAdapter) Put(ref string, value []byte) error {
	return keyring.Set(nativeService, ref, string(value))
}
func (keyringAdapter) Delete(ref string) error {
	err := keyring.Delete(nativeService, ref)
	if errors.Is(err, keyring.ErrNotFound) {
		return errNativeMissing
	}
	return err
}

func openNative(ctx context.Context, backend config.Backend) (Store, error) {
	return NativeFactory{Adapter: keyringAdapter{}}.Open(ctx, backend)
}
