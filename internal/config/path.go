package config

import (
	"errors"
	"os"
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
	if home == "" && goos != "windows" {
		return Paths{}, errors.New("home directory is required")
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	join := filepath.Join
	switch goos {
	case "linux":
		configRoot := getenv("XDG_CONFIG_HOME")
		if configRoot == "" {
			configRoot = join(home, ".config")
		} else if !filepath.IsAbs(configRoot) {
			return Paths{}, errors.New("XDG_CONFIG_HOME must be absolute")
		}
		dataRoot := getenv("XDG_DATA_HOME")
		if dataRoot == "" {
			dataRoot = join(home, ".local", "share")
		} else if !filepath.IsAbs(dataRoot) {
			return Paths{}, errors.New("XDG_DATA_HOME must be absolute")
		}
		return Paths{ConfigFile: join(configRoot, "pushover-cli", "config.yaml"), AgeDataDir: join(dataRoot, "pushover-cli")}, nil
	case "darwin":
		root := join(home, "Library", "Application Support", "pushover-cli")
		return Paths{ConfigFile: join(root, "config.yaml"), AgeDataDir: root}, nil
	case "windows":
		appData, localAppData := getenv("APPDATA"), getenv("LOCALAPPDATA")
		if appData == "" || localAppData == "" {
			return Paths{}, errors.New("APPDATA and LOCALAPPDATA are required on windows")
		}
		return Paths{ConfigFile: windowsJoin(appData, "pushover-cli", "config.yaml"), AgeDataDir: windowsJoin(localAppData, "pushover-cli")}, nil
	default:
		return Paths{}, errors.New("unsupported operating system")
	}
}

// DefaultPaths resolves the current user's platform-standard locations.
func DefaultPaths() (Paths, error) {
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
			part = strings.TrimRight(part, `\\/`)
		} else {
			part = strings.Trim(part, `\\/`)
		}
		if result == "" {
			result = part
		} else {
			result += `\` + part
		}
	}
	return result
}
