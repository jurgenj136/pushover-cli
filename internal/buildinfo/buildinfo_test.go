package buildinfo

import "testing"

func TestCurrentUsesDeterministicDefaults(t *testing.T) {
	got := Current()
	want := Info{Version: "dev", Commit: "unknown", Date: "unknown"}
	if got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}

func TestCurrentProjectsLinkerVariables(t *testing.T) {
	originalVersion, originalCommit, originalDate := version, commit, date
	t.Cleanup(func() {
		version, commit, date = originalVersion, originalCommit, originalDate
	})

	version = "v1.2.3"
	commit = "abc123"
	date = "2026-07-16T12:34:56Z"

	want := Info{Version: version, Commit: commit, Date: date}
	if got := Current(); got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}
