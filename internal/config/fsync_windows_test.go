//go:build windows

package config

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestSyncWindowsDirectoryUsesWritableDirectoryHandle(t *testing.T) {
	ops := &fakeWindowsDirectoryOps{}
	if err := syncWindowsDirectory(ops, `C:\\config`); err != nil {
		t.Fatal(err)
	}
	if want := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE); ops.access != want {
		t.Fatalf("access = %#x, want %#x", ops.access, want)
	}
	if !ops.closed {
		t.Fatal("directory handle was not closed")
	}
}

func TestSyncWindowsDirectoryNoopsOnlyForUnsupportedFlush(t *testing.T) {
	for _, allowed := range []error{windows.ERROR_INVALID_FUNCTION, windows.ERROR_NOT_SUPPORTED} {
		if err := syncWindowsDirectory(&fakeWindowsDirectoryOps{flushErr: allowed}, `C:\\config`); err != nil {
			t.Fatalf("%v should allow no-op: %v", allowed, err)
		}
	}
	if err := syncWindowsDirectory(&fakeWindowsDirectoryOps{flushErr: windows.ERROR_ACCESS_DENIED}, `C:\\config`); err == nil {
		t.Fatal("access denied must remain a failure")
	}
}

type fakeWindowsDirectoryOps struct {
	access   uint32
	flushErr error
	closed   bool
}

func (f *fakeWindowsDirectoryOps) Open(_ string, access uint32) (windows.Handle, error) {
	f.access = access
	return 1, nil
}
func (f *fakeWindowsDirectoryOps) Flush(windows.Handle) error { return f.flushErr }
func (f *fakeWindowsDirectoryOps) Close(windows.Handle) error { f.closed = true; return nil }
