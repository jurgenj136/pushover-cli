// Package buildinfo exposes metadata injected into release binaries by the
// linker and supplements development builds from Go runtime metadata.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Info contains the version, source revision, and build timestamp for a
// binary.
type Info struct {
	Version string
	Commit  string
	Date    string
}

var (
	version       = "dev"
	commit        = "unknown"
	date          = "unknown"
	readBuildInfo = debug.ReadBuildInfo
)

const modulePath = "github.com/jurgenj136/pushover-cli"

// Current returns metadata compiled into this binary, supplementing development
// builds with module and VCS metadata when Go makes it available.
func Current() Info {
	info := Info{Version: version, Commit: commit, Date: date}
	build, ok := readBuildInfo()
	if !ok {
		return info
	}

	if info.Version == "dev" && build.Main.Path == modulePath && build.Main.Version != "" && build.Main.Version != "(devel)" {
		info.Version = strings.TrimPrefix(build.Main.Version, "v")
	}
	if info.Commit == "unknown" {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				info.Commit = setting.Value
				break
			}
		}
	}

	return info
}
