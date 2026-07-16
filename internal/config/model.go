// Package config provides versioned, secret-reference-only configuration.
package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/domain"
	"go.yaml.in/yaml/v3"
)

const CurrentSchemaVersion = 1

// ErrNotFound means the configuration file does not exist. It deliberately
// does not contain a path, which keeps it safe to expose in normal output.
var ErrNotFound = errors.New("configuration not found")

type Backend struct {
	Type     string            `yaml:"type"`
	Metadata map[string]string `yaml:"metadata,omitempty"`
}

type MessageDefaults struct {
	Title    string        `yaml:"title,omitempty"`
	URL      string        `yaml:"url,omitempty"`
	URLTitle string        `yaml:"url_title,omitempty"`
	Sound    string        `yaml:"sound,omitempty"`
	Devices  []string      `yaml:"devices,omitempty"`
	Priority int           `yaml:"priority,omitempty"`
	TTL      time.Duration `yaml:"ttl,omitempty"`
}

type Profile struct {
	Name     string           `yaml:"name"`
	TokenRef domain.SecretRef `yaml:"token_ref"`
	Defaults MessageDefaults  `yaml:"defaults,omitempty"`
}

type Recipient struct {
	Alias      string            `yaml:"alias"`
	UserKeyRef domain.SecretRef  `yaml:"user_key_ref"`
	E2EEKeyRef domain.SecretRef  `yaml:"e2ee_key_ref,omitempty"`
	Kind       domain.TargetKind `yaml:"kind,omitempty"`
	Validated  bool              `yaml:"validated"`
	Devices    []string          `yaml:"devices,omitempty"`
}

type Config struct {
	SchemaVersion  int                  `yaml:"schema_version"`
	Backend        Backend              `yaml:"backend"`
	DefaultProfile string               `yaml:"default_profile,omitempty"`
	Profiles       map[string]Profile   `yaml:"profiles"`
	Recipients     map[string]Recipient `yaml:"recipients"`
}

type Store interface {
	Load(context.Context) (Config, error)
	Commit(context.Context, Config) error
	Path() string
}

type File interface {
	io.Reader
	io.Writer
	Sync() error
	Close() error
	Chmod(fs.FileMode) error
}

type FileSystem interface {
	MkdirAll(string, fs.FileMode) error
	Open(string) (File, error)
	OpenFile(string, int, fs.FileMode) (File, error)
	Rename(string, string) error
	Chmod(string, fs.FileMode) error
	Remove(string) error
	SyncDir(string) error
}

type Clock interface{ Now() time.Time }

// CommitOutcome tells coordinators whether it is safe to undo staged secrets.
type CommitOutcome string

const (
	Unchanged                CommitOutcome = "unchanged"
	AppliedDurabilityUnknown CommitOutcome = "applied_durability_unknown"
)

// CommitError marks the point at which a config write failed.
type CommitError struct {
	Outcome CommitOutcome
	Err     error
}

// storageError retains an OS cause for trusted callers without exposing local
// paths through a normal Error string.
type storageError struct{ err error }

func (e *storageError) Error() string { return "configuration storage error" }
func (e *storageError) Unwrap() error { return e.err }
func privateStorageError(err error) error {
	if err == nil {
		return nil
	}
	return &storageError{err: err}
}

func (e *CommitError) Error() string {
	// The wrapped cause can contain a filesystem path. Keep it available to
	// callers that need to branch or log trusted diagnostics via Unwrap, but do
	// not put it in the ordinary user-facing Error string.
	if e.Outcome == AppliedDurabilityUnknown {
		return "configuration may have been applied but its durability is unknown"
	}
	return "configuration was not changed"
}
func (e *CommitError) Unwrap() error { return e.Err }

// OutcomeOf returns Unchanged unless an error explicitly states otherwise.
func OutcomeOf(err error) CommitOutcome {
	var commitErr *CommitError
	if errors.As(err, &commitErr) {
		return commitErr.Outcome
	}
	return Unchanged
}

// MarshalYAML keeps a duration portable and human-readable rather than
// serializing its underlying integer nanoseconds.
func (d MessageDefaults) MarshalYAML() (any, error) {
	type encoded struct {
		Title    string   `yaml:"title,omitempty"`
		URL      string   `yaml:"url,omitempty"`
		URLTitle string   `yaml:"url_title,omitempty"`
		Sound    string   `yaml:"sound,omitempty"`
		Devices  []string `yaml:"devices,omitempty"`
		Priority int      `yaml:"priority,omitempty"`
		TTL      string   `yaml:"ttl,omitempty"`
	}
	result := encoded{Title: d.Title, URL: d.URL, URLTitle: d.URLTitle, Sound: d.Sound, Devices: d.Devices, Priority: d.Priority}
	if d.TTL != 0 {
		result.TTL = d.TTL.String()
	}
	return result, nil
}

// UnmarshalYAML rejects numeric durations. YAML would otherwise decode an
// integer into time.Duration, which makes config units dangerously implicit.
func (d *MessageDefaults) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("defaults must be a mapping")
	}
	allowed := map[string]bool{"title": true, "url": true, "url_title": true, "sound": true, "devices": true, "priority": true, "ttl": true}
	seen := make(map[string]bool)
	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		if !allowed[key.Value] {
			return fmt.Errorf("field %q not found in type config.MessageDefaults", key.Value)
		}
		if seen[key.Value] {
			return fmt.Errorf("mapping key %q already defined", key.Value)
		}
		seen[key.Value] = true
		if key.Value == "ttl" {
			if value.Tag != "!!str" {
				return errors.New("ttl must be a Go duration string")
			}
			ttl, err := time.ParseDuration(value.Value)
			if err != nil {
				return fmt.Errorf("ttl must be a Go duration string: %w", err)
			}
			d.TTL = ttl
			continue
		}
		var target any
		switch key.Value {
		case "title":
			target = &d.Title
		case "url":
			target = &d.URL
		case "url_title":
			target = &d.URLTitle
		case "sound":
			target = &d.Sound
		case "devices":
			target = &d.Devices
		case "priority":
			target = &d.Priority
		}
		if err := value.Decode(target); err != nil {
			return err
		}
	}
	return nil
}
