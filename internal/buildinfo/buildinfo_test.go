package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestCurrentUsesVersionedModuleBuildInfo(t *testing.T) {
	setBuildInfo(t, "dev", "unknown", "unknown", &debug.BuildInfo{
		Main: debug.Module{Path: modulePath, Version: "v0.1.3"},
	})

	want := Info{Version: "0.1.3", Commit: "unknown", Date: "unknown"}
	if got := Current(); got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}

func TestCurrentRejectsUnusableModuleBuildInfoVersion(t *testing.T) {
	for _, test := range []struct {
		name string
		main debug.Module
	}{
		{name: "development version", main: debug.Module{Path: modulePath, Version: "(devel)"}},
		{name: "wrong module", main: debug.Module{Path: "example.com/other", Version: "v0.1.3"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			setBuildInfo(t, "dev", "unknown", "unknown", &debug.BuildInfo{Main: test.main})

			want := Info{Version: "dev", Commit: "unknown", Date: "unknown"}
			if got := Current(); got != want {
				t.Fatalf("Current() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCurrentFallsBackToVCSRevisionWithoutBuildDate(t *testing.T) {
	setBuildInfo(t, "dev", "unknown", "unknown", &debug.BuildInfo{
		Main: debug.Module{Path: modulePath, Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-07-17T12:34:56Z"},
		},
	})

	want := Info{Version: "dev", Commit: "abc123", Date: "unknown"}
	if got := Current(); got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}

func TestCurrentKeepsLinkerInjectedMetadata(t *testing.T) {
	setBuildInfo(t, "0.1.3", "release-commit", "2026-07-17T12:34:56Z", &debug.BuildInfo{
		Main: debug.Module{Path: modulePath, Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "runtime-commit"},
			{Key: "vcs.time", Value: "2027-01-01T00:00:00Z"},
		},
	})

	want := Info{Version: "0.1.3", Commit: "release-commit", Date: "2026-07-17T12:34:56Z"}
	if got := Current(); got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}

func setBuildInfo(t *testing.T, linkedVersion, linkedCommit, linkedDate string, info *debug.BuildInfo) {
	t.Helper()
	originalVersion, originalCommit, originalDate, originalReadBuildInfo := version, commit, date, readBuildInfo
	t.Cleanup(func() {
		version, commit, date, readBuildInfo = originalVersion, originalCommit, originalDate, originalReadBuildInfo
	})

	version, commit, date = linkedVersion, linkedCommit, linkedDate
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, true }
}
