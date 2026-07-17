package doctest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/output"
)

const v011ReleaseHeading = "### Install v0.1.1 after the public release"

var v011ReleaseSectionContracts = []string{
	"brew install jurgenj136/tap/pushover",
	"go install github.com/jurgenj136/pushover-cli/cmd/pushover@v0.1.1",
	"npx --yes skills@1.5.9 add jurgenj136/pushover-cli --skill pushover-cli --yes",
	"pushover_0.1.1_<os>_<arch>.tar.gz",
	"pushover_0.1.1_<os>_<arch>.zip",
	"shasum -a 256 -c -",
	"sha256sum --check -",
	"Get-FileHash -Algorithm SHA256",
	"gh attestation verify \"$archive\" --repo jurgenj136/pushover-cli",
	"gh attestation verify checksums.txt --repo jurgenj136/pushover-cli",
}

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
		"npx --yes skills@"+skillsCLIMatch[1]+" add . --skill pushover-cli --agent codex --yes",
		"The skill does not install the native `pushover` binary",
		"No Homebrew formula or release archive is claimed",
		"Pushover dashboard",
		"register your own Pushover application",
		"required string fields `token` and `user_key`",
		"optional string field `e2ee_key`",
		"Unknown, duplicate, missing, null, or wrongly typed fields are rejected",
	)
	if strings.Contains(readme, "non-interactive workflow") {
		t.Fatal("README calls secret-prompting add commands non-interactive")
	}
	requireText(t, readme, "README.md", "still interactive", "require a terminal")
	if output.SchemaVersion != 1 || !strings.Contains(readme, "schema version 1") {
		t.Fatalf("README/output schema mismatch: code=%d", output.SchemaVersion)
	}
}

func TestREADMEKeepsV011ReleasePathsFutureGated(t *testing.T) {
	readme := readRepoFile(t, "README.md")

	requireText(t, readme, "README.md",
		"`v0.1.0` is tagged, but its release workflow failed before publishing artifacts",
		"No supported release exists yet",
		"Until the `v0.1.1` release workflow succeeds, a source build is the only supported installation path",
		"Homebrew, release-archive, and versioned `go install` paths below become supported only after that public release succeeds; they do not exist yet",
		"checksums.txt",
		"GitHub artifact attestations",
	)
	requireText(t, readme, "README.md", v011ReleaseSectionContracts...)
	if err := validateV011ReleasePathPlacement(readme); err != nil {
		t.Fatal(err)
	}

	t.Run("rejects a deliberate pre-gate release command", func(t *testing.T) {
		const command = "brew install jurgenj136/tap/pushover"
		mutated := strings.Replace(readme, v011ReleaseHeading, command+"\n\n"+v011ReleaseHeading, 1)

		if err := validateV011ReleasePathPlacement(mutated); err == nil {
			t.Fatalf("validator accepted %q before %q", command, v011ReleaseHeading)
		}
	})
}

func validateV011ReleasePathPlacement(readme string) error {
	sectionStart := strings.Index(readme, v011ReleaseHeading)
	if sectionStart < 0 {
		return fmt.Errorf("README.md is missing %q", v011ReleaseHeading)
	}
	sectionEnd := len(readme)
	if nextHeading := strings.Index(readme[sectionStart+len(v011ReleaseHeading):], "\n## "); nextHeading >= 0 {
		sectionEnd = sectionStart + len(v011ReleaseHeading) + nextHeading
	}

	section := readme[sectionStart:sectionEnd]
	outsideSection := readme[:sectionStart] + readme[sectionEnd:]
	for _, contract := range v011ReleaseSectionContracts {
		if !strings.Contains(section, contract) {
			return fmt.Errorf("README.md is missing release-section contract %q", contract)
		}
		if strings.Contains(outsideSection, contract) {
			return fmt.Errorf("README.md contains release-section contract %q outside %q", contract, v011ReleaseHeading)
		}
	}
	return nil
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
