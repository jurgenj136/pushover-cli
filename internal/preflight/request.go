package preflight

import (
	"net/url"

	"github.com/jurgenj136/pushover-cli/internal/resolver"
)

// AttachmentInput selects at most one immutable attachment source.
type AttachmentInput struct {
	LocalPath string
	RemoteURL *url.URL
}

// Request contains the resolver input and preflight controls. Validate is
// intentionally ignored for dry runs; dry runs never call Pushover.
type Request struct {
	Resolve         resolver.Input
	Attachment      AttachmentInput
	DryRun          bool
	Validate        bool
	ValidateNetwork bool
}
