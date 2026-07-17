package config

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths are the non-secret locations used by the CLI.
type Paths struct {
	ConfigFile string
	AgeDataDir string
}

// ResolvePaths is deterministic for tests. getenv must return an empty string
// for an absent value.
func ResolvePaths(goos, home string, getenv func(string) string) (Paths, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	switch goos {
	case "linux":
		if !isPOSIXAbsolute(home) {
			return Paths{}, errors.New("home directory must be absolute")
		}
		configRoot := getenv("XDG_CONFIG_HOME")
		if configRoot == "" {
			configRoot = path.Join(home, ".config")
		} else if !path.IsAbs(configRoot) {
			return Paths{}, errors.New("XDG_CONFIG_HOME must be absolute")
		}
		dataRoot := getenv("XDG_DATA_HOME")
		if dataRoot == "" {
			dataRoot = path.Join(home, ".local", "share")
		} else if !path.IsAbs(dataRoot) {
			return Paths{}, errors.New("XDG_DATA_HOME must be absolute")
		}
		return Paths{ConfigFile: path.Join(configRoot, "pushover-cli", "config.yaml"), AgeDataDir: path.Join(dataRoot, "pushover-cli")}, nil
	case "darwin":
		if !isPOSIXAbsolute(home) {
			return Paths{}, errors.New("home directory must be absolute")
		}
		root := path.Join(home, "Library", "Application Support", "pushover-cli")
		return Paths{ConfigFile: path.Join(root, "config.yaml"), AgeDataDir: root}, nil
	case "windows":
		appData, localAppData := getenv("APPDATA"), getenv("LOCALAPPDATA")
		if appData == "" || localAppData == "" {
			return Paths{}, errors.New("APPDATA and LOCALAPPDATA are required on windows")
		}
		if !isWindowsAbsolute(appData) || !isWindowsAbsolute(localAppData) {
			return Paths{}, errors.New("APPDATA and LOCALAPPDATA must be absolute on windows")
		}
		return Paths{ConfigFile: windowsJoin(appData, "pushover-cli", "config.yaml"), AgeDataDir: windowsJoin(localAppData, "pushover-cli")}, nil
	default:
		return Paths{}, errors.New("unsupported operating system")
	}
}

func isPOSIXAbsolute(location string) bool { return path.IsAbs(location) }

// isWindowsAbsolute intentionally avoids filepath.VolumeName: tests execute on
// non-Windows hosts, but APPDATA still needs Windows path semantics.
func isWindowsAbsolute(path string) bool {
	if len(path) >= 3 && isASCIIAlpha(path[0]) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	if !strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, `//`) {
		return false
	}
	unc := path[2:]
	if unc == "" || unc[0] == '\\' || unc[0] == '/' {
		return false
	}
	parts := strings.FieldsFunc(unc, func(character rune) bool { return character == '\\' || character == '/' })
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}

func isASCIIAlpha(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

// DefaultPaths resolves the current user's platform-standard locations.
func DefaultPaths() (Paths, error) {
	if runtime.GOOS == "linux" {
		if configured := os.Getenv("XDG_CONFIG_HOME"); configured != "" && !filepath.IsAbs(configured) {
			return Paths{}, errors.New("XDG_CONFIG_HOME must be absolute")
		}
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	switch runtime.GOOS {
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		dataRoot := os.Getenv("XDG_DATA_HOME")
		if dataRoot == "" {
			dataRoot = filepath.Join(home, ".local", "share")
		}
		if !filepath.IsAbs(dataRoot) {
			return Paths{}, errors.New("XDG_DATA_HOME must be absolute")
		}
		return Paths{ConfigFile: filepath.Join(configRoot, "pushover-cli", "config.yaml"), AgeDataDir: filepath.Join(dataRoot, "pushover-cli")}, nil
	case "darwin":
		root := filepath.Join(configRoot, "pushover-cli")
		return Paths{ConfigFile: filepath.Join(root, "config.yaml"), AgeDataDir: root}, nil
	case "windows":
		cacheRoot, err := os.UserCacheDir()
		if err != nil {
			return Paths{}, err
		}
		return Paths{ConfigFile: filepath.Join(configRoot, "pushover-cli", "config.yaml"), AgeDataDir: filepath.Join(cacheRoot, "pushover-cli")}, nil
	default:
		return Paths{}, errors.New("unsupported operating system")
	}
}

func windowsJoin(parts ...string) string {
	result := ""
	for index, part := range parts {
		if index == 0 {
			part = strings.TrimRight(part, `\/`)
		} else {
			part = strings.Trim(part, `\/`)
		}
		if result == "" {
			result = part
		} else {
			result += `\` + part
		}
	}
	return result
}
