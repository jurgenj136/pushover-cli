// Package buildinfo exposes metadata injected into release binaries by the
// linker while keeping deterministic values for local development builds.
package buildinfo

// Info contains the version, source revision, and build timestamp for a
// binary.
type Info struct {
	Version string
	Commit  string
	Date    string
}

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// Current returns the metadata compiled into this binary.
func Current() Info {
	return Info{Version: version, Commit: commit, Date: date}
}
