package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const maxConfigBytes = 1 << 20

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type osFileSystem struct{}

func (osFileSystem) MkdirAll(path string, mode fs.FileMode) error { return os.MkdirAll(path, mode) }
func (osFileSystem) Open(path string) (File, error)               { return os.Open(path) }
func (osFileSystem) OpenFile(path string, flags int, mode fs.FileMode) (File, error) {
	return os.OpenFile(path, flags, mode)
}
func (osFileSystem) Rename(oldPath, newPath string) error      { return os.Rename(oldPath, newPath) }
func (osFileSystem) Chmod(path string, mode fs.FileMode) error { return os.Chmod(path, mode) }
func (osFileSystem) Remove(path string) error                  { return os.Remove(path) }

// FileStore persists one single-writer configuration file. It intentionally
// does not provide compare-and-swap: concurrent writers are outside v1 scope.
type FileStore struct {
	path  string
	fs    FileSystem
	clock Clock
}

func NewFileStore(path string) *FileStore { return NewFileStoreWith(path, osFileSystem{}, realClock{}) }
func NewFileStoreWith(path string, filesystem FileSystem, clock Clock) *FileStore {
	if filesystem == nil {
		filesystem = osFileSystem{}
	}
	if clock == nil {
		clock = realClock{}
	}
	return &FileStore{path: path, fs: filesystem, clock: clock}
}
func (s *FileStore) Path() string { return s.path }

func (s *FileStore) Load(ctx context.Context) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	file, err := s.fs.Open(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, ErrNotFound
		}
		return Config{}, privateStorageError(err)
	}
	data, readErr := readBounded(file, maxConfigBytes)
	closeErr := file.Close()
	if readErr != nil {
		return Config{}, privateStorageError(readErr)
	}
	if closeErr != nil {
		return Config{}, privateStorageError(closeErr)
	}
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	config, explicitSchema, err := decodeConfig(data)
	if err != nil {
		return Config{}, err
	}
	if !explicitSchema {
		return Config{}, errors.New("schema_version is required")
	}
	if config.SchemaVersion < 0 {
		return Config{}, errors.New("schema_version must not be negative")
	}
	if config.SchemaVersion > CurrentSchemaVersion {
		return Config{}, fmt.Errorf("configuration schema version %d is newer than supported", config.SchemaVersion)
	}
	if config.SchemaVersion == CurrentSchemaVersion {
		if err := Validate(config); err != nil {
			return Config{}, err
		}
		return config, nil
	}
	if config.SchemaVersion != 0 {
		return Config{}, fmt.Errorf("configuration schema version %d is unsupported", config.SchemaVersion)
	}
	return s.migrateV0(ctx, data, config)
}

func (s *FileStore) Commit(ctx context.Context, config Config) error {
	if err := ctx.Err(); err != nil {
		return &CommitError{Outcome: Unchanged, Err: err}
	}
	if err := Validate(config); err != nil {
		return &CommitError{Outcome: Unchanged, Err: err}
	}
	encoded, err := yaml.Marshal(config)
	if err != nil {
		return &CommitError{Outcome: Unchanged, Err: fmt.Errorf("encode configuration: %w", err)}
	}
	parent := filepath.Dir(s.path)
	if err := s.fs.MkdirAll(parent, 0o700); err != nil {
		return &CommitError{Outcome: Unchanged, Err: fmt.Errorf("create configuration directory: %w", err)}
	}
	if err := s.fs.Chmod(parent, 0o700); err != nil {
		return &CommitError{Outcome: Unchanged, Err: fmt.Errorf("restrict configuration directory: %w", err)}
	}
	if err := secureDirectory(parent); err != nil {
		return &CommitError{Outcome: Unchanged, Err: err}
	}
	temporary := filepath.Join(parent, fmt.Sprintf(".%s.%d.tmp", filepath.Base(s.path), s.clock.Now().UTC().UnixNano()))
	file, err := s.fs.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return &CommitError{Outcome: Unchanged, Err: fmt.Errorf("create temporary configuration: %w", err)}
	}
	failed := func(stage string, cause error) error {
		_ = file.Close()
		_ = s.fs.Remove(temporary)
		return &CommitError{Outcome: Unchanged, Err: fmt.Errorf("%s: %w", stage, cause)}
	}
	if err := file.Chmod(0o600); err != nil {
		return failed("restrict temporary configuration", err)
	}
	if err := secureFile(temporary); err != nil {
		return failed("restrict temporary configuration", err)
	}
	if err := writeAll(file, encoded); err != nil {
		return failed("write temporary configuration", err)
	}
	if err := file.Sync(); err != nil {
		return failed("sync temporary configuration", err)
	}
	if err := file.Close(); err != nil {
		_ = s.fs.Remove(temporary)
		return &CommitError{Outcome: Unchanged, Err: fmt.Errorf("close temporary configuration: %w", err)}
	}
	if err := ctx.Err(); err != nil {
		_ = s.fs.Remove(temporary)
		return &CommitError{Outcome: Unchanged, Err: err}
	}
	if err := s.fs.Rename(temporary, s.path); err != nil {
		_ = s.fs.Remove(temporary)
		return &CommitError{Outcome: Unchanged, Err: fmt.Errorf("replace configuration: %w", err)}
	}
	if err := s.fs.SyncDir(parent); err != nil {
		return &CommitError{Outcome: AppliedDurabilityUnknown, Err: fmt.Errorf("sync configuration directory: %w", err)}
	}
	return nil
}

func decodeConfig(data []byte) (Config, bool, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, false, fmt.Errorf("decode configuration: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, false, errors.New("configuration must contain exactly one document")
		}
		return Config{}, false, fmt.Errorf("decode configuration: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return Config{}, false, fmt.Errorf("decode configuration: %w", err)
	}
	return config, containsSchemaVersion(&node), nil
}

func containsSchemaVersion(node *yaml.Node) bool {
	if len(node.Content) == 0 {
		return false
	}
	node = node.Content[0]
	if node.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index < len(node.Content); index += 2 {
		if node.Content[index].Value == "schema_version" {
			return true
		}
	}
	return false
}

func readBounded(file File, maximum int64) ([]byte, error) {
	limited := io.LimitReader(file, maximum+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errors.New("configuration is too large")
	}
	return data, nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
