//go:build !windows

package config

import "os"

func (osFileSystem) SyncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func secureDirectory(string) error { return nil }
func secureFile(string) error      { return nil }
