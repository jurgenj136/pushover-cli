package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func storeContract(t *testing.T, open func(t *testing.T) Store) {
	t.Helper()
	store := open(t)
	first, err := store.Put(context.Background(), []byte("first-secret"))
	if err != nil || first == "" {
		t.Fatalf("Put: %q, %v", first, err)
	}
	second, err := store.Put(context.Background(), []byte("second-secret"))
	if err != nil || first == second {
		t.Fatalf("unique Put: %q %q %v", first, second, err)
	}
	got, err := store.Get(context.Background(), first)
	if err != nil || string(got) != "first-secret" {
		t.Fatalf("Get: %q %v", got, err)
	}
	got[0] = 'X'
	again, err := store.Get(context.Background(), first)
	if err != nil || string(again) != "first-secret" {
		t.Fatalf("Get defensive copy: %q %v", again, err)
	}
	if err := store.Delete(context.Background(), first); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(context.Background(), first); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
	if err := store.Check(context.Background(), first); err == nil {
		t.Fatal("Check missing succeeded")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(canceled, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Put: %v", err)
	}
	if _, err := store.Get(canceled, domain.SecretRef("missing")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Get: %v", err)
	}
}

func TestMemoryStoreContract(t *testing.T) {
	storeContract(t, func(t *testing.T) Store { return NewMemoryStore() })
}

type nativeFake struct {
	values            map[string][]byte
	deleteErr, getErr error
}

func (f *nativeFake) Get(ref string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	value, ok := f.values[ref]
	if !ok {
		return nil, errNativeMissing
	}
	return copyBytes(value), nil
}
func (f *nativeFake) Put(ref string, value []byte) error {
	f.values[ref] = copyBytes(value)
	return nil
}
func (f *nativeFake) Delete(ref string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.values[ref]; !ok {
		return errNativeMissing
	}
	delete(f.values, ref)
	return nil
}
func TestNativeStoreDeleteOnlyIgnoresNotFound(t *testing.T) {
	fake := &nativeFake{values: map[string][]byte{}, deleteErr: errors.New("keychain offline")}
	store := newNativeStore(fake)
	if err := store.Delete(context.Background(), "opaque"); err == nil {
		t.Fatal("provider delete failure was hidden")
	}
	fake.deleteErr = errNativeMissing
	if err := store.Delete(context.Background(), "opaque"); err != nil {
		t.Fatalf("not found delete: %v", err)
	}
}
func TestNativeStoreDoesNotTurnOperationalErrorsIntoMissing(t *testing.T) {
	fake := &nativeFake{values: map[string][]byte{}, getErr: errors.New("service offline")}
	if _, err := newNativeStore(fake).Get(context.Background(), "opaque"); err == nil || err.Error() == missing().Error() {
		t.Fatalf("Get error = %v", err)
	}
	if _, err := newNativeStore(fake).Put(context.Background(), []byte("value")); err == nil {
		t.Fatal("Put collision probe hid operational error")
	}
}
