package doctest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
)

var approvedCommandTree = []string{
	"pushover setup",
	"pushover send [message] [flags]",
	"pushover profiles add <name>",
	"pushover profiles list",
	"pushover profiles show <name>",
	"pushover profiles use <name>",
	"pushover profiles remove <name>",
	"pushover recipients add <alias>",
	"pushover recipients list",
	"pushover recipients show <alias>",
	"pushover recipients validate <alias>...",
	"pushover recipients remove <alias>",
	"pushover sounds",
	"pushover limits",
	"pushover auth status",
	"pushover auth backend",
	"pushover auth backend set <native|file>",
	"pushover info",
	"pushover version",
	"pushover completion <bash|zsh|fish>",
}

func TestReferenceCommandTreeMatchesHelpAndCompletions(t *testing.T) {
	reference := readRepoFile(t, "REFERENCE.md")
	if got := strings.Split(strings.TrimSpace(fencedBlocks(reference)[0]), "\n"); !reflect.DeepEqual(got, approvedCommandTree) {
		t.Fatalf("REFERENCE command tree = %q, want %q", got, approvedCommandTree)
	}

	binary := buildCLI(t)
	help := runCLI(t, binary, "--help")
	for _, signature := range []string{
		"setup [flags]", "send [<body>] [flags]", "profiles add <name> [flags]",
		"profiles list", "profiles show <name>", "profiles use <name>",
		"profiles remove <name> [flags]", "recipients add <alias> [flags]",
		"recipients list", "recipients show <alias>", "recipients validate <aliases> ...",
		"recipients remove <alias> [flags]", "sounds [flags]", "limits [flags]",
		"auth status", "auth backend set <destination>", "info [flags]", "version [flags]",
		"completion <shell> [flags]",
	} {
		if !strings.Contains(help, "  "+signature) {
			t.Errorf("root --help is missing exact command signature %q", signature)
		}
	}
	for _, flag := range []string{"--profile=STRING", "--json", "--plain", "--no-color", "--timeout=STRING"} {
		if !strings.Contains(help, flag) {
			t.Errorf("root --help is missing %q", flag)
		}
	}

	readme := readRepoFile(t, "README.md")
	for _, shell := range []string{"bash", "zsh", "fish"} {
		artifact := map[string]string{"bash": "completions/pushover.bash", "zsh": "completions/_pushover", "fish": "completions/pushover.fish"}[shell]
		requireText(t, readme, "README.md", "pushover completion "+shell+" > "+artifact)
		if got, want := runCLI(t, binary, "completion", shell), readRepoFile(t, artifact); got != want {
			t.Errorf("completion %s output differs from %s", shell, artifact)
		}
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pushover")
	command := exec.Command("go", "build", "-o", path, "./cmd/pushover")
	command.Dir = repoRoot(t)
	command.Env = withoutPushoverEnvironment(os.Environ())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build pushover: %v\n%s", err, output)
	}
	return path
}

func runCLI(t *testing.T, binary string, args ...string) string {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = repoRoot(t)
	command.Env = cleanEnvironment(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pushover %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func cleanEnvironment(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	result := withoutPushoverEnvironment(os.Environ())
	filtered := make([]string, 0, len(result)+2)
	for _, entry := range result {
		name := strings.SplitN(entry, "=", 2)[0]
		if name == "HOME" || name == "XDG_CONFIG_HOME" {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"))
}

func withoutPushoverEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(strings.SplitN(entry, "=", 2)[0], "PUSHOVER_") {
			result = append(result, entry)
		}
	}
	return result
}

func TestReferenceJSONSchemasMatchOutputFixtures(t *testing.T) {
	reference := readRepoFile(t, "REFERENCE.md")
	cases := []struct {
		row, fixture string
	}{
		{"send", "send-json-success.golden"},
		{"profiles list/show", "profiles-list-json-success.golden"},
		{"recipients list/show", "recipients-list-json-success.golden"},
		{"recipients validate", "recipients-validate-json-success.golden"},
		{"sounds", "sounds-json-success.golden"},
		{"limits", "limits-json-success.golden"},
		{"auth status", "auth-status-json-success.golden"},
		{"auth backend", "auth-backend-json-success.golden"},
		{"info", "info-json-success.golden"},
		{"version", "version-json-success.golden"},
		{"setup`; profile/recipient mutations; `auth backend set", "profiles-add-json-success.golden"},
		{"completion", "completion-json-success.golden"},
	}
	for _, test := range cases {
		t.Run(test.row, func(t *testing.T) {
			documented := codeTerms(markdownTableCell(t, reference, test.row, 1))
			actual := fixtureResultKeys(t, test.fixture)
			if !reflect.DeepEqual(documented, actual) {
				t.Fatalf("documented JSON keys = %q, fixture keys = %q", documented, actual)
			}
		})
	}

	send := readJSONFixture(t, "send-json-failed.golden")
	result := send["result"].(map[string]any)
	target := result["targets"].([]any)[0].(map[string]any)
	counts := result["counts"].(map[string]any)
	targetError := target["error"].(map[string]any)
	assertDocumentedKeySet(t, reference, "Each target object has exactly these keys:", "Their types", mapKeys(target))
	assertDocumentedKeySet(t, reference, "A non-null target error object has exactly", ": the first", mapKeys(targetError))
	assertDocumentedKeySet(t, reference, "`counts` always contains exactly", ". Other successful", mapKeys(counts))

	errorEnvelope := readJSONFixture(t, "send-json-error.golden")
	documentedEnvelope, documentedError := documentedCommandErrorKeys(t, reference)
	if got := mapKeys(errorEnvelope); !reflect.DeepEqual(got, documentedEnvelope) {
		t.Fatalf("error envelope keys = %q, want %q", got, documentedEnvelope)
	}
	error, ok := errorEnvelope[documentedError.objectKey].(map[string]any)
	if !ok {
		t.Fatalf("error envelope field %q is not an object", documentedError.objectKey)
	}
	if got := mapKeys(error); !reflect.DeepEqual(got, documentedError.fields) {
		t.Fatalf("error object keys = %q, want %q", got, documentedError.fields)
	}
	requireText(t, reference, "REFERENCE.md", "A command\nfailure has `status: \"error\"`, no `result`", "boolean `ambiguous`")
}

type documentedErrorContract struct {
	objectKey string
	fields    []string
}

func documentedCommandErrorKeys(t *testing.T, reference string) ([]string, documentedErrorContract) {
	t.Helper()
	var paragraph string
	for _, candidate := range strings.Split(reference, "\n\n") {
		if strings.Contains(candidate, "A command\nfailure has") {
			paragraph = strings.Join(strings.Fields(candidate), " ")
			break
		}
	}
	if paragraph == "" {
		t.Fatal("REFERENCE is missing the command-error schema paragraph")
	}
	prefix, failure, ok := strings.Cut(paragraph, "A success document")
	if !ok {
		t.Fatal("command-error paragraph is missing the success-document boundary")
	}
	baseKeys := documentedKeys(prefix)

	objectMatch := regexp.MustCompile("an `([^`]+)` object with (.+)\\.").FindStringSubmatch(failure)
	if len(objectMatch) != 3 {
		t.Fatalf("could not parse command-error object fields from %q", failure)
	}
	contract := documentedErrorContract{
		objectKey: objectMatch[1],
		fields:    documentedKeys(objectMatch[2]),
	}
	return uniqueSorted(append(baseKeys, contract.objectKey)), contract
}

func documentedKeys(value string) []string {
	terms := codeTerms(value)
	keys := make([]string, 0, len(terms))
	for _, term := range terms {
		keys = append(keys, strings.TrimSpace(strings.SplitN(term, ":", 2)[0]))
	}
	return uniqueSorted(keys)
}

func fixtureResultKeys(t *testing.T, name string) []string {
	t.Helper()
	document := readJSONFixture(t, name)
	result := document["result"].(map[string]any)
	keys := mapKeys(result)
	if rows, ok := result["rows"].([]any); ok && len(rows) > 0 {
		keys = append(keys, mapKeys(rows[0].(map[string]any))...)
		keys = uniqueSorted(keys)
	}
	return keys
}

func readJSONFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(readRepoFile(t, filepath.Join("internal", "output", "testdata", name))), &document); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return document
}

func TestReferencePlainSchemasAndExitMeanings(t *testing.T) {
	reference := readRepoFile(t, "REFERENCE.md")
	type plainContract struct {
		columns []string
		fixture string
	}
	plain := map[string]plainContract{
		"send":                 {[]string{"alias", "outcome", "priority", "request_id", "receipt_id", "quota_remaining", "error_code", "error_category"}, "send-plain-failed.golden"},
		"profiles list/show":   {[]string{"name", "default", "priority", "title", "sound"}, "profiles-list-plain-success.golden"},
		"recipients list/show": {[]string{"alias", "kind", "validated", "devices"}, "recipients-list-plain-success.golden"},
		"recipients validate":  {[]string{"alias", "valid", "devices", "licenses", "error_code"}, "recipients-validate-plain-success.golden"},
		"sounds":               {[]string{"name", "description"}, "sounds-plain-success.golden"},
		"limits":               {[]string{"limit", "remaining", "reset_unix"}, "limits-plain-success.golden"},
		"auth status":          {[]string{"profile", "backend", "configured", "token_resolves"}, "auth-status-plain-success.golden"},
		"auth backend":         {[]string{"backend"}, "auth-backend-plain-success.golden"},
		"info":                 {[]string{"config_path", "profile", "backend", "version", "commit", "build_date", "features"}, "info-plain-success.golden"},
		"version":              {[]string{"version", "commit", "build_date"}, "version-plain-success.golden"},
	}
	for row, contract := range plain {
		if got := orderedCodeTerms(markdownTableCellAfter(t, reference, "Plain output is headerless TSV", row, 1)); !reflect.DeepEqual(got, contract.columns) {
			t.Errorf("plain columns for %s = %q, want %q", row, got, contract.columns)
		}
		fixture := strings.TrimSuffix(readRepoFile(t, filepath.Join("internal", "output", "testdata", contract.fixture)), "\n")
		firstRow := strings.SplitN(fixture, "\n", 2)[0]
		if fields := strings.Count(firstRow, "\t") + 1; fields != len(contract.columns) {
			t.Errorf("%s fields = %d, documented columns = %d", contract.fixture, fields, len(contract.columns))
		}
	}
	requireText(t, reference, "REFERENCE.md", "`setup`, mutation commands, and `completion` reject `--plain` as a usage error")

	wantExits := map[int]string{
		apperror.ExitSuccess:       "Complete success",
		apperror.ExitInternal:      "Internal or unclassified failure",
		apperror.ExitUsage:         "Command usage error",
		apperror.ExitConfiguration: "Configuration, secret-store, or authentication failure",
		apperror.ExitValidation:    "Local or remote validation failure",
		apperror.ExitQuota:         "Quota or API rate-limit failure",
		apperror.ExitNetwork:       "Definite network failure with no delivery",
		apperror.ExitPartial:       "Partial, interrupted, or mixed fan-out result",
		apperror.ExitConfirmation:  "Required confirmation was not supplied",
		apperror.ExitAmbiguous:     "Ambiguous delivery with no completed delivery",
		apperror.ExitInterrupted:   "Interrupted before any delivery completed and no delivery remains ambiguous",
	}
	if got := exitTable(t, reference); !reflect.DeepEqual(got, wantExits) {
		t.Fatalf("REFERENCE exit table = %#v, want %#v", got, wantExits)
	}
}

func markdownTableCellAfter(t *testing.T, document, marker, row string, cell int) string {
	t.Helper()
	index := strings.Index(document, marker)
	if index < 0 {
		t.Fatalf("missing table marker %q", marker)
	}
	return markdownTableCell(t, document[index:], row, cell)
}

func markdownTableCell(t *testing.T, document, row string, cell int) string {
	t.Helper()
	needle := "| `" + row + "` |"
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, needle) {
			parts := strings.Split(line, "|")
			if cell+1 >= len(parts) {
				t.Fatalf("malformed table row %q", line)
			}
			return strings.TrimSpace(parts[cell+1])
		}
	}
	t.Fatalf("missing table row %q", row)
	return ""
}

func codeTerms(value string) []string {
	return uniqueSorted(orderedCodeTerms(value))
}

func orderedCodeTerms(value string) []string {
	matches := regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(value, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, match[1])
	}
	return result
}

func assertDocumentedKeySet(t *testing.T, document, start, end string, want []string) {
	t.Helper()
	startIndex := strings.Index(document, start)
	if startIndex < 0 {
		t.Fatalf("missing schema start %q", start)
	}
	section := document[startIndex+len(start):]
	endIndex := strings.Index(section, end)
	if endIndex < 0 {
		t.Fatalf("missing schema end %q", end)
	}
	got := codeTerms(section[:endIndex])
	if got[0] == "counts" {
		got = got[1:]
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("documented keys between %q and %q = %q, want %q", start, end, got, want)
	}
}

func mapKeys(value map[string]any) []string {
	result := make([]string, 0, len(value))
	for key := range value {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func exitTable(t *testing.T, document string) map[int]string {
	t.Helper()
	result := map[int]string{}
	section := strings.SplitN(document, "## Exit codes", 2)
	if len(section) != 2 {
		t.Fatal("REFERENCE is missing exit-code section")
	}
	for _, line := range strings.Split(section[1], "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 4 {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			continue
		}
		result[code] = strings.TrimSpace(parts[2])
	}
	return result
}

func TestReferenceInternalLinksResolve(t *testing.T) {
	root := repoRoot(t)
	for _, doc := range []string{"README.md", "SECURITY.md", "REFERENCE.md"} {
		contents := readRepoFile(t, doc)
		links := regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`).FindAllStringSubmatch(contents, -1)
		for _, match := range links {
			link := match[1]
			if strings.Contains(link, "://") || strings.HasPrefix(link, "#") {
				continue
			}
			path := filepath.Clean(filepath.Join(root, filepath.Dir(doc), strings.SplitN(link, "#", 2)[0]))
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s link %q does not resolve: %v", doc, link, err)
			}
		}
	}
}
