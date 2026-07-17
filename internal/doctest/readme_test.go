package doctest

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/output"
)

const (
	dryRunCommand   = `pushover --json send --to <recipient-alias> --dry-run "message"`
	realSendCommand = `pushover --json send --to <recipient-alias> "message"`
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

func TestREADMEIsCompactUserGuide(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	validator := readRepoFile(t, filepath.Join("scripts", "validate-skill.sh"))
	skillsCLI := regexp.MustCompile(`skills@([0-9]+\.[0-9]+\.[0-9]+)`).FindStringSubmatch(validator)
	if len(skillsCLI) != 2 {
		t.Fatal("validate-skill.sh does not pin a semantic skills CLI version")
	}
	requireText(t, readme, "README.md",
		"unofficial, outbound-only", "supported pre-1.0 release",
		"## Features", "## Installation",
		"brew install jurgenj136/tap/pushover",
		"go install github.com/jurgenj136/pushover-cli/cmd/pushover@v0.1.3",
		"npx --yes skills@"+skillsCLI[1]+" add jurgenj136/pushover-cli --skill pushover-cli --yes",
		"The Agent Skill does not install the native `pushover` binary.",
		"git clone https://github.com/jurgenj136/pushover-cli.git",
		"go install ./cmd/pushover", "## Quick start", "pushover setup",
		dryRunCommand, realSendCommand, "## Common examples",
		"## Output and completion", "schema version 1", "## Safety",
		"credentials for your own Pushover account and application",
		"non-idempotent", "at most once", "never automatically retries", "ambiguous",
		"[Command reference](REFERENCE.md)", "[Security model](SECURITY.md)",
		"[v0.1.3 release](https://github.com/jurgenj136/pushover-cli/releases/tag/v0.1.3)")
	if output.SchemaVersion != 1 {
		t.Fatalf("README/output schema mismatch: code=%d", output.SchemaVersion)
	}
	if lines := strings.Count(readme, "\n") + 1; lines > 180 {
		t.Fatalf("README has %d lines, want at most 180", lines)
	}
	for _, removed := range []string{"`v0.1.0` is tagged", "PUSHOVER_LIVE_CONFIRM", "pins resolved addresses against DNS rebinding", "reuses it for AES and HMAC"} {
		if strings.Contains(readme, removed) {
			t.Errorf("README retains detailed material %q", removed)
		}
	}
}

func TestREADMEShowsDryRunBeforeExplicitSend(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	if err := validateFirstSendOrder(readme); err != nil {
		t.Fatal(err)
	}
	t.Run("rejects reversed commands", func(t *testing.T) {
		mutated := strings.Replace(readme, dryRunCommand, "__DRY_RUN__", 1)
		mutated = strings.Replace(mutated, realSendCommand, dryRunCommand, 1)
		mutated = strings.Replace(mutated, "__DRY_RUN__", realSendCommand, 1)
		if err := validateFirstSendOrder(mutated); err == nil {
			t.Fatal("validator accepted a real send before the dry run")
		}
	})
}

func validateFirstSendOrder(readme string) error {
	dryRunIndex := strings.Index(readme, dryRunCommand)
	if dryRunIndex < 0 {
		return errors.New("README is missing the dry-run command")
	}
	realSendIndex := strings.Index(readme, realSendCommand)
	if realSendIndex < 0 {
		return errors.New("README is missing the explicit send command")
	}
	if realSendIndex <= dryRunIndex {
		return errors.New("README must show the dry run before the real send")
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
		"[Security model](SECURITY.md)", "public HTTPS",
		"End-to-end encryption is fail-closed",
		"never automatically retries", "ambiguous")
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
