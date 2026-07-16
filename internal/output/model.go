// Package output owns the stable human, JSON, and headerless TSV contracts.
package output

import (
	"io"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type Mode string

const (
	Human Mode = "human"
	JSON  Mode = "json"
	Plain Mode = "plain"
)

const SchemaVersion = 1

type CommandResult interface {
	CommandName() string
	StatusName() string
}

type Meta struct{ Command, Status string }

func (m Meta) CommandName() string { return m.Command }
func (m Meta) StatusName() string  { return m.Status }

type ProfileRow struct {
	Name         string
	Default      bool
	Priority     int
	Title, Sound string
}
type RecipientRow struct {
	Alias     string
	Kind      domain.TargetKind
	Validated bool
	Devices   []string
}
type ValidationRow struct {
	Alias             string
	Valid             bool
	Devices, Licenses []string
	ErrorCode         string
}
type SoundRow struct{ Name, Description string }

type SendResult struct {
	Meta
	Batch domain.BatchResult
}
type ProfilesResult struct {
	Meta
	Rows []ProfileRow
}
type RecipientsResult struct {
	Meta
	Rows []RecipientRow
}
type ValidationsResult struct {
	Meta
	Rows []ValidationRow
}
type SoundsResult struct {
	Meta
	Rows []SoundRow
}
type LimitsResult struct {
	Meta
	Limit, Remaining int
	Reset            int64
}
type AuthStatusResult struct {
	Meta
	Profile, Backend          string
	Configured, TokenResolves bool
}
type BackendResult struct {
	Meta
	Backend string
}
type InfoResult struct {
	Meta
	ConfigPath, Profile, Backend, Version, Commit, BuildDate string
	Features                                                 []string
}
type VersionResult struct {
	Meta
	Version, Commit, BuildDate string
}
type MutationResult struct {
	Meta
	Resource, Name, Action string
	Warnings               []string
}
type CompletionResult struct {
	Meta
	Shell, Script string
}

type Renderer interface {
	Result(io.Writer, CommandResult) error
	Error(io.Writer, string, *apperror.Error) error
}
type Factory interface {
	New(Mode, RenderOptions) (Renderer, error)
}
type RenderOptions struct{ Color, TTY bool }

type factory struct{}

func NewFactory() Factory { return factory{} }
func (factory) New(mode Mode, options RenderOptions) (Renderer, error) {
	switch mode {
	case Human:
		return HumanRenderer{Options: options}, nil
	case JSON:
		return JSONRenderer{}, nil
	case Plain:
		return PlainRenderer{}, nil
	default:
		return nil, usage("output_mode", "unsupported output mode")
	}
}
