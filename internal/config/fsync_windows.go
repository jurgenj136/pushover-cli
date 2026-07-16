//go:build windows

package config

import (
	"errors"
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

type windowsDirectoryOps interface {
	Open(string, uint32) (windows.Handle, error)
	Flush(windows.Handle) error
	Close(windows.Handle) error
}

type nativeWindowsDirectoryOps struct{}

func (nativeWindowsDirectoryOps) Open(path string, access uint32) (windows.Handle, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(pointer, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
}
func (nativeWindowsDirectoryOps) Flush(handle windows.Handle) error {
	return windows.FlushFileBuffers(handle)
}
func (nativeWindowsDirectoryOps) Close(handle windows.Handle) error {
	return windows.CloseHandle(handle)
}

func (osFileSystem) SyncDir(path string) error {
	return syncWindowsDirectory(nativeWindowsDirectoryOps{}, path)
}

func syncWindowsDirectory(ops windowsDirectoryOps, path string) error {
	handle, err := ops.Open(path, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		return fmt.Errorf("open directory for flush: %w", err)
	}
	defer ops.Close(handle)
	if err := ops.Flush(handle); err != nil {
		if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
			return nil
		}
		return fmt.Errorf("flush directory: %w", err)
	}
	return nil
}

// secureDirectory and secureFile remove inherited ACLs and grant the current
// user full control. os.Chmod is not an ACL boundary on Windows.
func secureDirectory(path string) error { return restrictToCurrentUser(path) }
func secureFile(path string) error      { return restrictToCurrentUser(path) }

func restrictToCurrentUser(path string) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("open process token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("get process user: %w", err)
	}
	var pinner runtime.Pinner
	pinner.Pin(user.User.Sid)
	defer pinner.Unpin()
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("build current-user DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return fmt.Errorf("protect current-user DACL: %w", err)
	}
	return nil
}
