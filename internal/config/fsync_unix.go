//go:build !windows

package config

import "os"

func (osFileSystem) SyncDir(path string) error {
	// #nosec G304 -- path is the application-owned configuration directory.
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

func secureDirectory(string) error { return nil }
func secureFile(string) error      { return nil }
