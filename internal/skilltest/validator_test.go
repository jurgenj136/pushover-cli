package skilltest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatorAcceptsValidFixture(t *testing.T) {
	stdout, stderr, err := validateOnly(t, "valid-skill.md")
	if err != nil {
		t.Fatalf("validator rejected valid fixture: %v\nstderr: %s", err, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("validator output = stdout %q, stderr %q; want silence", stdout, stderr)
	}
}

func TestValidatorRejectsInvalidFrontmatter(t *testing.T) {
	stdout, stderr, err := validateOnly(t, "invalid-frontmatter.md")
	if err == nil {
		t.Fatal("validator accepted invalid frontmatter")
	}
	if stdout != "" {
		t.Fatalf("validator wrote stdout %q", stdout)
	}
	if !strings.HasPrefix(stderr, "ERROR skill-frontmatter: ") {
		t.Fatalf("validator stderr = %q, want skill-frontmatter diagnostic", stderr)
	}
}

func TestValidatorRejectsVersionDrift(t *testing.T) {
	stdout, stderr, err := validateOnly(t, "version-drift.md")
	if err == nil {
		t.Fatal("validator accepted version drift")
	}
	if stdout != "" {
		t.Fatalf("validator wrote stdout %q", stdout)
	}
	if !strings.HasPrefix(stderr, "ERROR skill-version: ") {
		t.Fatalf("validator stderr = %q, want skill-version diagnostic", stderr)
	}
}

func TestValidatorRejectsMalformedYAMLPlainScalars(t *testing.T) {
	cases := map[string]string{
		"additional colon":  "Use when: YAML becomes ambiguous.",
		"comment":           "Use when explicitly requested. # comment",
		"double quote":      "Use when explicitly \"requested\".",
		"single quote":      "Use when explicitly 'requested'.",
		"flow collection":   "Use when [explicitly requested].",
		"flow mapping":      "Use when {explicitly requested}.",
		"block scalar":      "Use when | explicitly requested.",
		"folded scalar":     "Use when > explicitly requested.",
		"anchor":            "Use when &authorized explicitly requested.",
		"alias":             "Use when *authorized explicitly requested.",
		"tag":               "Use when !authorized explicitly requested.",
		"tab":               "Use when\texplicitly requested.",
		"control character": "Use when \x01 explicitly requested.",
	}
	for name, description := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "malformed.md")
			contents := "---\nname: pushover-cli\ndescription: " + description + "\n---\n\n# Pushover CLI\n\nSkill version: 0.1.0\nMinimum CLI version: 0.1.0\n"
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatalf("write malformed skill: %v", err)
			}
			stdout, stderr, err := validatePath(t, path)
			if err == nil {
				t.Fatalf("validator accepted malformed YAML description %q", description)
			}
			if stdout != "" {
				t.Fatalf("validator wrote stdout %q", stdout)
			}
			if !strings.HasPrefix(stderr, "ERROR skill-frontmatter: ") {
				t.Fatalf("validator stderr = %q, want skill-frontmatter diagnostic", stderr)
			}
		})
	}
}

func TestValidatorRejectsDiscoveryThatFindsNoSkills(t *testing.T) {
	binDir := t.TempDir()
	fakeNPM := filepath.Join(binDir, "npx")
	if err := os.WriteFile(fakeNPM, []byte("#!/bin/sh\n[ \"$1\" = \"--yes\" ] && [ \"$2\" = \"skills@1.5.9\" ] && [ \"$3\" = \"add\" ] && [ \"$4\" = \".\" ] && [ \"$5\" = \"--list\" ] || exit 99\nprintf '%s\\n' 'No skills found'\n"), 0o700); err != nil {
		t.Fatalf("write fake npx: %v", err)
	}

	command := exec.Command("sh", filepath.Join(repoRoot(t), "scripts", "validate-skill.sh"))
	command.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		t.Fatal("validator accepted successful discovery with no skills")
	}
	if !strings.Contains(stderr.String(), "ERROR skill-discovery: ") {
		t.Fatalf("validator stderr = %q, want skill-discovery diagnostic", stderr.String())
	}
}

func validateOnly(t *testing.T, fixture string) (string, string, error) {
	t.Helper()
	return validatePath(t, filepath.Join(repoRoot(t), "internal", "skilltest", "testdata", fixture))
}

func validatePath(t *testing.T, path string) (string, string, error) {
	t.Helper()
	command := exec.Command("sh", filepath.Join(repoRoot(t), "scripts", "validate-skill.sh"), "--validate-only", path)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
