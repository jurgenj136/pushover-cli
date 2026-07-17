package skilltest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolatedInstall(t *testing.T) {
	if _, err := exec.LookPath("npx"); err != nil {
		unavailable(t, "npx not found")
	}

	for _, agent := range []string{"claude-code", "codex"} {
		t.Run(agent, func(t *testing.T) {
			home := t.TempDir()
			source := filepath.Join(home, "source")
			if err := os.MkdirAll(source, 0o700); err != nil {
				t.Fatalf("create isolated skill source: %v", err)
			}
			canonical, err := os.ReadFile(filepath.Join(repoRoot(t), ".claude", "skills", "pushover-cli", "SKILL.md"))
			if err != nil {
				t.Fatalf("read canonical skill: %v", err)
			}
			if err := os.WriteFile(filepath.Join(source, "SKILL.md"), canonical, 0o600); err != nil {
				t.Fatalf("write isolated skill source: %v", err)
			}
			command := exec.Command("npx", "--yes", "skills@1.5.9", "add", ".", "--skill", "pushover-cli", "--agent", agent, "--copy", "--global")
			command.Dir = source
			command.Env = append(os.Environ(),
				"DISABLE_TELEMETRY=1",
				"DO_NOT_TRACK=1",
				"NO_UPDATE_NOTIFIER=1",
				"HOME="+home,
				"CODEX_HOME="+filepath.Join(home, ".codex"),
				"CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"),
				"CLAUDE_CODE_HOME="+filepath.Join(home, ".claude"),
			)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("pinned %s install failed: %v\nstdout: %s\nstderr: %s", agent, err, stdout.String(), stderr.String())
			}

			installedPath := filepath.Join(home, ".claude", "skills", "pushover-cli", "SKILL.md")
			if agent == "codex" {
				installedPath = filepath.Join(home, ".agents", "skills", "pushover-cli", "SKILL.md")
			}
			installed, err := os.ReadFile(installedPath)
			if err != nil {
				t.Fatalf("read isolated %s install at %s: %v\nstdout: %s\nstderr: %s", agent, installedPath, err, stdout.String(), stderr.String())
			}
			if !strings.Contains(string(installed), "name: pushover-cli") {
				t.Fatalf("pinned %s install did not create pushover-cli SKILL.md", agent)
			}
		})
	}
}

func unavailable(t *testing.T, reason string) {
	t.Helper()
	message := "SKIP skills-cli-unavailable: " + reason
	if os.Getenv("CI") != "" {
		t.Fatal(message)
	}
	t.Skip(message)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return path
}
