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
	if _, err := ResolvePaths("linux", "relative-home", nil); err == nil {
		t.Fatal("expected relative Linux home error")
	}
	if _, err := ResolvePaths("darwin", "relative-home", nil); err == nil {
		t.Fatal("expected relative Darwin home error")
	}
	if _, err := ResolvePaths("linux", "/home/alice", func(string) string { return "relative" }); err == nil {
		t.Fatal("expected relative XDG rejection")
	}
	if _, err := ResolvePaths("windows", "", func(key string) string {
		if key == "APPDATA" {
			return `C:relative`
		}
		return `C:\\Users\\alice\\AppData\\Local`
	}); err == nil {
		t.Fatal("expected relative APPDATA rejection")
	}
	if _, err := ResolvePaths("windows", "", func(key string) string {
		if key == "LOCALAPPDATA" {
			return `relative`
		}
		return `C:\\Users\\alice\\AppData\\Roaming`
	}); err == nil {
		t.Fatal("expected relative LOCALAPPDATA rejection")
	}
}

func TestWindowsJoinPreservesUNCVolume(t *testing.T) {
	if got, want := windowsJoin(`\\server\share\`, "pushover-cli", "config.yaml"), `\\server\share\pushover-cli\config.yaml`; got != want {
		t.Fatalf("windowsJoin = %q, want %q", got, want)
	}
}

func TestWindowsAbsoluteValidationIsHostIndependent(t *testing.T) {
	for _, path := range []string{`C:\Users\alice`, `z:/data`, `\\server\share\dir`, `//server/share/dir`} {
		if !isWindowsAbsolute(path) {
			t.Fatalf("expected Windows absolute path: %q", path)
		}
	}
	for _, path := range []string{"", `C:relative`, `\\server`, `\\\server\share`, `server\share`, `/posix/path`} {
		if isWindowsAbsolute(path) {
			t.Fatalf("expected Windows relative/invalid path: %q", path)
		}
	}
}
