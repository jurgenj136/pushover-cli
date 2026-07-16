//go:build darwin && cgo

package secrets

import (
	"context"
	"errors"

	"github.com/jurgenj136/pushover-cli/internal/config"
	keychain "github.com/keybase/go-keychain"
)

// keychainAdapter calls Keychain Services through the binding. It deliberately
// does not invoke /usr/bin/security, which would expose values in argv.
type keychainAdapter struct{}

func (keychainAdapter) Get(ref string) ([]byte, error) {
	value, err := keychain.GetGenericPassword(nativeService, ref, "", "")
	if errors.Is(err, keychain.ErrorItemNotFound) {
		err = errNativeMissing
	}
	if err != nil || value == nil {
		if err == nil {
			err = errors.New("item not found")
		}
		return nil, err
	}
	return append([]byte(nil), value...), nil
}
func (keychainAdapter) Put(ref string, value []byte) error {
	return keychain.AddItem(keychain.NewGenericPassword(nativeService, ref, "", value, ""))
}
func (keychainAdapter) Delete(ref string) error {
	err := keychain.DeleteGenericPasswordItem(nativeService, ref)
	if errors.Is(err, keychain.ErrorItemNotFound) {
		return errNativeMissing
	}
	return err
}
func openNative(ctx context.Context, backend config.Backend) (Store, error) {
	return NativeFactory{Adapter: keychainAdapter{}}.Open(ctx, backend)
}
