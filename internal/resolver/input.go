// Package resolver turns safe configuration and explicitly supplied secrets
// into a complete local send draft. It never performs network I/O.
package resolver

import (
	"io"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const MaxBodyBytes int64 = 64 * 1024

type BodySourceKind string

const (
	BodyPositional BodySourceKind = "positional"
	BodyStdin      BodySourceKind = "stdin"
	BodyFile       BodySourceKind = "file"
)

type BodySource struct {
	Kind     BodySourceKind
	Value    string
	Reader   io.Reader
	OpenFile func(string) (io.ReadCloser, error)
}

// MessageFieldSet records flag presence. It keeps explicit zero values from
// being accidentally replaced by saved defaults.
type MessageFieldSet struct {
	Title, URL, URLTitle, Sound, Devices, Priority, TTL bool
}

type Input struct {
	Profile                    string
	To                         []string
	EnvToken, EnvUserKey       string
	EnvTokenSet, EnvUserKeySet bool
	Credentials                io.Reader
	Message                    domain.Message
	MessageFields              MessageFieldSet
	Body                       BodySource
}
