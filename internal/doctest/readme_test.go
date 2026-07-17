package doctest

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/output"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	value, err := os.ReadFile(filepath.Join(repoRoot(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(value)
}

func requireText(t *testing.T, document, name string, required ...string) {
	t.Helper()
	normalizedDocument := strings.Join(strings.Fields(document), " ")
	for _, value := range required {
		if !strings.Contains(document, value) && !strings.Contains(normalizedDocument, strings.Join(strings.Fields(value), " ")) {
			t.Errorf("%s is missing contract text %q", name, value)
		}
	}
}

func lineValue(t *testing.T, document, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	t.Fatalf("missing line prefix %q", prefix)
	return ""
}

func TestREADMEInstallCredentialsAndVersionContracts(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	skill := readRepoFile(t, filepath.Join(".claude", "skills", "pushover-cli", "SKILL.md"))
	validator := readRepoFile(t, filepath.Join("scripts", "validate-skill.sh"))

	skillVersion := lineValue(t, skill, "Skill version: ")
	minimumCLI := lineValue(t, skill, "Minimum CLI version: ")
	skillsCLIMatch := regexp.MustCompile(`skills@([0-9]+\.[0-9]+\.[0-9]+)`).FindStringSubmatch(validator)
	if len(skillsCLIMatch) != 2 {
		t.Fatal("validate-skill.sh does not pin a semantic skills CLI version")
	}

	requireText(t, readme, "README.md",
		"unofficial, outbound-only",
		"git clone https://github.com/jurgenj136/pushover-cli.git",
		"go install ./cmd/pushover",
		"Skill version `"+skillVersion+"` requires CLI version `"+minimumCLI+"` or newer",
		"npx --yes skills@"+skillsCLIMatch[1]+" add . --skill pushover-cli --agent codex",
		"The skill does not install the native `pushover` binary",
		"No Homebrew formula or release archive is claimed",
		"Pushover dashboard",
		"register your own Pushover application",
		"required string fields `token` and `user_key`",
		"optional string field `e2ee_key`",
		"Unknown, duplicate, missing, null, or wrongly typed fields are rejected",
	)
	if strings.Contains(readme, "brew install") || strings.Contains(readme, "/releases/download/") {
		t.Fatal("README claims a release installation channel before release configuration is supported")
	}
	if strings.Contains(readme, "non-interactive workflow") {
		t.Fatal("README calls secret-prompting add commands non-interactive")
	}
	requireText(t, readme, "README.md", "still interactive", "require a terminal")
	if output.SchemaVersion != 1 || !strings.Contains(readme, "schema version 1") {
		t.Fatalf("README/output schema mismatch: code=%d", output.SchemaVersion)
	}
}

func TestPublicDocsContainNoCredentialEchoExample(t *testing.T) {
	command := regexp.MustCompile(`(?m)^\s*(?:\$\s*)?(?:echo|printf)\b`)
	for _, name := range []string{"README.md", "REFERENCE.md", "SECURITY.md"} {
		document := readRepoFile(t, name)
		for index, block := range fencedBlocks(document) {
			if command.MatchString(block) {
				t.Errorf("%s fenced block %d contains an echo-style command", name, index+1)
			}
		}
	}
}

func fencedBlocks(document string) []string {
	var blocks []string
	parts := strings.Split(document, "```")
	for index := 1; index < len(parts); index += 2 {
		block := parts[index]
		if newline := strings.IndexByte(block, '\n'); newline >= 0 {
			block = block[newline+1:]
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func TestSecurityAndLiveDocumentationContracts(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	security := readRepoFile(t, "SECURITY.md")

	requireText(t, readme, "README.md",
		"valid 64-hex-character key",
		"no plaintext fallback or mixed encrypted/plaintext batch",
		"message, title, URL, and URL title",
		"reuses it for AES and HMAC",
		"no replay or reordering protection",
		"compression leaks approximate plaintext length",
		"chiefly protects against off-machine disclosure",
		"not local same-user compromise",
		"pins resolved addresses against DNS rebinding",
		"bounds streamed bytes regardless of `Content-Length`",
		"source URL as attachment metadata",
		"PUSHOVER_LIVE_CONFIRM=YES go test -tags=live ./internal/live -run TestLiveSend -v",
		"consumes one message from the dedicated application's quota",
		"exactly one lowest-priority (`-2`)",
	)
	requireText(t, security, "SECURITY.md",
		"read or use those secrets without a new CLI prompt",
		"chiefly protects backups, sync copies, and stolen media",
		"loopback, private, link-local, multicast, unspecified",
		"pinned to prevent DNS rebinding",
		"source URL is never sent to Pushover as attachment metadata",
		"valid 64-hex-character key before any target is sent",
		"same key for AES-256-CBC encryption and HMAC-SHA256",
		"no replay or reordering protection",
		"leaks approximate plaintext length",
	)
}
