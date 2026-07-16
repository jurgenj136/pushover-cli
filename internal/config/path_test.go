package config

import (
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	tests := []struct {
		name, goos, home string
		env              map[string]string
		config, data     string
	}{
		{"linux defaults", "linux", "/home/alice", nil, "/home/alice/.config/pushover-cli/config.yaml", "/home/alice/.local/share/pushover-cli"},
		{"linux xdg", "linux", "/home/alice", map[string]string{"XDG_CONFIG_HOME": "/config", "XDG_DATA_HOME": "/data"}, "/config/pushover-cli/config.yaml", "/data/pushover-cli"},
		{"darwin", "darwin", "/Users/alice", nil, "/Users/alice/Library/Application Support/pushover-cli/config.yaml", "/Users/alice/Library/Application Support/pushover-cli"},
		{"windows", "windows", "", map[string]string{"APPDATA": `C:\Users\alice\AppData\Roaming`, "LOCALAPPDATA": `C:\Users\alice\AppData\Local`}, `C:\Users\alice\AppData\Roaming\pushover-cli\config.yaml`, `C:\Users\alice\AppData\Local\pushover-cli`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, err := ResolvePaths(test.goos, test.home, func(key string) string { return test.env[key] })
			if err != nil {
				t.Fatal(err)
			}
			if paths.ConfigFile != filepath.FromSlash(test.config) || paths.AgeDataDir != filepath.FromSlash(test.data) {
				t.Fatalf("paths = %#v", paths)
			}
		})
	}
}

func TestResolvePathsRejectsMissingPlatformVariables(t *testing.T) {
	if _, err := ResolvePaths("windows", "", func(string) string { return "" }); err == nil {
		t.Fatal("expected Windows environment error")
	}
	if _, err := ResolvePaths("linux", "", nil); err == nil {
		t.Fatal("expected home error")
	}
	if _, err := ResolvePaths("linux", "/home/alice", func(string) string { return "relative" }); err == nil {
		t.Fatal("expected relative XDG rejection")
	}
}

func TestWindowsJoinPreservesUNCVolume(t *testing.T) {
	if got, want := windowsJoin(`\\server\share\`, "pushover-cli", "config.yaml"), `\\server\share\pushover-cli\config.yaml`; got != want {
		t.Fatalf("windowsJoin = %q, want %q", got, want)
	}
}
