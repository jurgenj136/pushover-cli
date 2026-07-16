package config

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func (s *FileStore) migrateV0(ctx context.Context, original []byte, config Config) (Config, error) {
	// v0 is a private pre-release fixture only. It has no public compatibility
	// guarantee; the migration exists to exercise durable upgrade mechanics.
	config.SchemaVersion = CurrentSchemaVersion
	if err := Validate(config); err != nil {
		return Config{}, err
	}
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	parent := filepath.Dir(s.path)
	backup := filepath.Join(parent, fmt.Sprintf("%s.v0.%s.bak", filepath.Base(s.path), s.clock.Now().UTC().Format("20060102T150405.000000000Z")))
	file, err := s.fs.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Config{}, privateStorageError(err)
	}
	fail := func(stage string, cause error) (Config, error) {
		_ = file.Close()
		_ = s.fs.Remove(backup)
		return Config{}, privateStorageError(fmt.Errorf("%s: %w", stage, cause))
	}
	if err := file.Chmod(fs.FileMode(0o600)); err != nil {
		return fail("restrict migration backup", err)
	}
	if err := secureFile(backup); err != nil {
		return fail("restrict migration backup", err)
	}
	if err := writeAll(file, original); err != nil {
		return fail("write migration backup", err)
	}
	if err := file.Sync(); err != nil {
		return fail("sync migration backup", err)
	}
	if err := file.Close(); err != nil {
		return Config{}, privateStorageError(err)
	}
	if err := s.fs.SyncDir(parent); err != nil {
		return Config{}, privateStorageError(err)
	}
	if err := s.Commit(ctx, config); err != nil {
		return Config{}, err
	}
	return config, nil
}
