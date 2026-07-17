package releasetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type contract struct {
	Versions struct {
		GolangCILint string `json:"golangci_lint"`
		GoReleaser   string `json:"goreleaser"`
		Skills       string `json:"skills"`
		Syft         string `json:"syft"`
	} `json:"versions"`
	Syft struct {
		ChecksumsURL string `json:"checksums_url"`
		HostAsset    string `json:"host_asset"`
	} `json:"syft"`
	Archives []struct {
		GOOS       string `json:"goos"`
		GOARCH     string `json:"goarch"`
		CGO        string `json:"cgo"`
		Name       string `json:"name"`
		Format     string `json:"format"`
		Binary     string `json:"binary"`
		SBOM       string `json:"sbom"`
		Provenance string `json:"provenance_subject"`
	} `json:"archives"`
	Checksum           string   `json:"checksum"`
	ProvenanceSubjects []string `json:"provenance_subjects"`
	Homebrew           struct {
		Owner   string `json:"owner"`
		Formula string `json:"formula"`
		PRMode  bool   `json:"pull_request"`
	} `json:"homebrew"`
}

type actionPin struct {
	Tag  string `json:"tag"`
	SHA  string `json:"sha"`
	Date string `json:"date"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("find test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func readContract(t *testing.T) contract {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/releasetest/testdata/release-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got contract
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join(repoRoot(t), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

func workflowStepBody(t *testing.T, name, path string) string {
	t.Helper()
	contents := readFile(t, path)
	marker := "- name: " + name
	lines := strings.Split(contents, "\n")
	inStep := false
	var body []string
	for _, line := range lines {
		if strings.TrimSpace(line) == marker {
			inStep = true
			continue
		}
		if inStep && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			break
		}
		if inStep {
			body = append(body, strings.TrimSpace(line))
		}
	}
	if len(body) == 0 {
		t.Fatalf("workflow step %q not found in %s", name, path)
	}
	return strings.Join(body, "\n")
}

func TestReleaseContract(t *testing.T) {
	c := readContract(t)
	if c.Versions.GolangCILint != "v2.12.2" || c.Versions.Syft != "v1.46.0" || c.Versions.GoReleaser != "v2.17.0" || c.Versions.Skills != "1.5.9" {
		t.Fatalf("unexpected frozen versions: %#v", c.Versions)
	}
	if c.Syft.ChecksumsURL != "https://github.com/anchore/syft/releases/download/v1.46.0/syft_1.46.0_checksums.txt" || c.Syft.HostAsset != "syft_1.46.0_darwin_${GOARCH}.tar.gz" {
		t.Fatalf("unexpected syft contract: %#v", c.Syft)
	}
	if len(c.Archives) != 6 || c.Checksum != "dist/checksums.txt" || len(c.ProvenanceSubjects) != 7 {
		t.Fatalf("unexpected distribution contract: archives=%d checksum=%q provenance=%d", len(c.Archives), c.Checksum, len(c.ProvenanceSubjects))
	}
	for _, archive := range c.Archives {
		if archive.Name == "" || archive.Binary == "" || archive.SBOM == "" || archive.Provenance == "" {
			t.Fatalf("incomplete archive contract: %#v", archive)
		}
		if archive.GOOS == "windows" && archive.Format != "zip" {
			t.Fatalf("windows archive must be zip: %#v", archive)
		}
		if archive.GOOS != "windows" && archive.Format != "tar.gz" {
			t.Fatalf("non-windows archive must be tar.gz: %#v", archive)
		}
		if archive.GOOS == "darwin" && archive.CGO != "1" {
			t.Fatalf("darwin must use cgo: %#v", archive)
		}
		if archive.GOOS != "darwin" && archive.CGO != "0" {
			t.Fatalf("non-darwin must not use cgo: %#v", archive)
		}
	}
	expectedNames := map[string]bool{
		"pushover_{{ .Version }}_darwin_amd64.tar.gz": true,
		"pushover_{{ .Version }}_darwin_arm64.tar.gz": true,
		"pushover_{{ .Version }}_linux_amd64.tar.gz":  true,
		"pushover_{{ .Version }}_linux_arm64.tar.gz":  true,
		"pushover_{{ .Version }}_windows_amd64.zip":   true,
		"pushover_{{ .Version }}_windows_arm64.zip":   true,
	}
	pairs := make(map[string]string, len(c.Archives))
	for _, archive := range c.Archives {
		if !expectedNames[archive.Name] {
			t.Fatalf("unexpected archive name: %q", archive.Name)
		}
		if prior, exists := pairs[archive.SBOM]; exists {
			t.Fatalf("SBOM %q is shared by %q and %q", archive.SBOM, prior, archive.Binary)
		}
		pairs[archive.SBOM] = archive.Binary
		delete(expectedNames, archive.Name)
	}
	if len(expectedNames) != 0 {
		t.Fatalf("missing archive names: %#v", expectedNames)
	}
	if c.Homebrew.Owner != "jurgenj136/homebrew-tap" || c.Homebrew.Formula != "Formula/pushover.rb" || !c.Homebrew.PRMode {
		t.Fatalf("unexpected homebrew contract: %#v", c.Homebrew)
	}
}

func TestActionPinsAreImmutable(t *testing.T) {
	bytes, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/releasetest/testdata/action-pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pins map[string]actionPin
	if err := json.Unmarshal(bytes, &pins); err != nil {
		t.Fatal(err)
	}
	if len(pins) != 4 {
		t.Fatalf("expected exactly four action pins, got %d", len(pins))
	}
	expected := map[string]actionPin{
		"actions/checkout":             {Tag: "v7.0.0", SHA: "9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0"},
		"actions/setup-go":             {Tag: "v7.0.0", SHA: "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"},
		"actions/attest":               {Tag: "v4.2.0", SHA: "f7c74d28b9d84cb8768d0b8ca14a4bac6ef463e6"},
		"goreleaser/goreleaser-action": {Tag: "v7.2.3", SHA: "f06c13b6b1a9625abc9e6e439d9c05a8f2190e94"},
	}
	for action, pin := range pins {
		if len(pin.SHA) != 40 || strings.Trim(pin.SHA, "0123456789abcdef") != "" || !strings.HasPrefix(pin.Tag, "v") {
			t.Fatalf("invalid immutable pin for %s: %#v", action, pin)
		}
		want, ok := expected[action]
		if !ok || pin.Tag != want.Tag || pin.SHA != want.SHA || len(pin.Date) != len("2006-01-02") {
			t.Fatalf("unexpected verified pin for %s: %#v", action, pin)
		}
	}
}

func TestReleaseConfigurationUsesFrozenContract(t *testing.T) {
	c := readContract(t)
	config := readFile(t, ".goreleaser.yaml")
	for _, archive := range c.Archives {
		for _, want := range []string{archive.GOOS, archive.GOARCH, "CGO_ENABLED=" + archive.CGO, archive.Format} {
			if !strings.Contains(config, want) {
				t.Errorf(".goreleaser.yaml missing %q", want)
			}
		}
	}
	for _, want := range []string{"checksums.txt", "spdx-json", "internal/buildinfo.Version", "internal/buildinfo.Commit", "internal/buildinfo.Date", "homebrew-tap", "directory: Formula", "pull_request:"} {
		if !strings.Contains(config, want) {
			t.Errorf(".goreleaser.yaml missing %q", want)
		}
	}

	workflow := readFile(t, ".github/workflows/release.yml")
	for _, want := range []string{c.Versions.GoReleaser, ".syft.checksums_url", ".syft.host_asset", "gsub", "contents: write", "id-token: write", "attestations: write", "HOMEBREW_TAP_GITHUB_TOKEN", "actions/attest@", "checksums.txt"} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	if strings.Contains(workflow, "PUSHOVER_") || strings.Contains(workflow, "pushover send") {
		t.Error("release workflow must not contain live Pushover credentials or sends")
	}
}

func TestSnapshotWorkflowsInstallVerifiedSyft(t *testing.T) {
	const step = "Install verified Syft"
	release := workflowStepBody(t, step, ".github/workflows/release.yml")
	ci := workflowStepBody(t, step, ".github/workflows/ci.yml")
	if release != ci {
		t.Fatal("CI and release workflows must use the same hardened Syft installer")
	}
	for _, want := range []string{
		"contract=internal/releasetest/testdata/release-contract.json",
		"checksums_url=$(jq -r '.syft.checksums_url' \"$contract\")",
		"asset=$(jq -r '.syft.host_asset' \"$contract\" | sed",
		"curl --fail --location --silent --show-error --remote-name \"$checksums_url\"",
		"grep \" $asset$\" syft_1.46.0_checksums.txt | shasum -a 256 -c -",
		"tar -xzf \"$asset\" syft",
		"echo \"$PWD\" >> \"$GITHUB_PATH\"",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("Syft installer missing %q", want)
		}
	}
}

func TestWorkflowsPinEveryActionAndUseLeastPrivilege(t *testing.T) {
	c := readContract(t)
	bytes, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/releasetest/testdata/action-pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pins map[string]actionPin
	if err := json.Unmarshal(bytes, &pins); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		workflow := readFile(t, file)
		if !strings.Contains(workflow, "permissions:") {
			t.Errorf("%s has no explicit permissions", file)
		}
		for action, pin := range pins {
			if strings.Contains(workflow, action+"@") {
				want := action + "@" + pin.SHA + " # " + pin.Tag
				if !strings.Contains(workflow, want) {
					t.Errorf("%s must use %s", file, want)
				}
			}
		}
		for _, line := range strings.Split(workflow, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- uses:") {
				actionRef := strings.TrimSpace(strings.TrimPrefix(line, "- uses:"))
				action, _, found := strings.Cut(actionRef, "@")
				if !found {
					t.Errorf("%s unpinned action: %s", file, line)
					continue
				}
				if _, ok := pins[action]; !ok {
					t.Errorf("%s has an unverified action: %s", file, action)
				}
			}
		}
	}
	ci := readFile(t, ".github/workflows/ci.yml")
	if strings.Contains(ci, ": write") {
		t.Error("CI must have read-only permissions")
	}
	for _, want := range []string{c.Versions.GolangCILint, "go vet ./...", "go mod verify", "go-licenses@v1.6.0", "govulncheck", "go test -race ./...", "-fuzz=Fuzz -fuzztime=5s", "scripts/check-generated.sh", "scripts/validate-skill.sh"} {
		if !strings.Contains(ci, want) {
			t.Errorf("ci workflow missing %q", want)
		}
	}
}

func TestReleaseGuardsAndPermissions(t *testing.T) {
	workflow := readFile(t, ".github/workflows/release.yml")
	for _, want := range []string{"GITHUB_REF_NAME", "^v(0|[1-9][0-9]*)", "^v0\\.1\\.", "contents: write", "id-token: write", "attestations: write"} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing guard or permission %q", want)
		}
	}
	for _, forbidden := range []string{"actions: write", "checks: write", "issues: write", "packages: write", "pull-requests: write"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("release workflow has unnecessary permission %q", forbidden)
		}
	}
}

func TestReleaseWorkflowUsesGoReleaserVersionForSBOM(t *testing.T) {
	workflow := readFile(t, ".github/workflows/release.yml")
	if !strings.Contains(workflow, `version="${GITHUB_REF_NAME#v}"`) {
		t.Fatal("release workflow must strip the leading v before expanding GoReleaser SBOM names")
	}
	if !strings.Contains(workflow, `--arg version "$version"`) {
		t.Fatal("release workflow must expand SBOM names with the normalized release version")
	}

	const tag = "v0.1.0"
	version := strings.TrimPrefix(tag, "v")
	contract := readContract(t)
	for _, archive := range contract.Archives {
		want := strings.ReplaceAll(archive.SBOM, "{{ .Version }}", version)
		if strings.Contains(want, "_v0.1.0_") {
			t.Fatalf("GoReleaser SBOM name must not include the tag prefix: %q", want)
		}
		if !strings.Contains(want, "_0.1.0_") {
			t.Fatalf("unexpected SBOM name for %s: %q", archive.Name, want)
		}
	}
}

func TestChecksConsumeTheContract(t *testing.T) {
	for _, file := range []string{"scripts/check-generated.sh", "scripts/check-release.sh"} {
		contents := readFile(t, file)
		if !strings.Contains(contents, "release-contract.json") {
			t.Errorf("%s does not consume release contract", file)
		}
		if strings.Contains(contents, "UPDATE_COMPLETIONS=1") {
			t.Errorf("%s must not mutate completion artifacts", file)
		}
	}
}
