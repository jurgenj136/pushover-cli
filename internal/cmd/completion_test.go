package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var approvedCompletionRootCommands = []string{
	"setup", "send", "profiles", "recipients", "sounds", "limits", "auth", "info", "version", "completion",
}

var approvedCompletionGlobalFlags = []string{
	"--profile", "--json", "--plain", "--no-color", "--timeout",
}

var approvedCompletionWords = []string{
	"setup", "send", "profiles", "recipients", "sounds", "limits", "auth", "info", "version", "completion",
	"add", "list", "show", "use", "remove", "validate", "status", "backend", "set",
	"--to", "--title", "--device", "--priority", "--sound", "--timestamp", "--ttl", "--url", "--url-title",
	"--html", "--monospace", "--attachment", "--attachment-url", "--encrypted", "--retry", "--expire",
	"--validate", "--validate-network", "--dry-run", "--yes", "--stdin", "--file", "--credentials-stdin",
	"--kind", "--offline", "--replace-with",
	"bash", "zsh", "fish", "native", "file",
}

func TestCompletionManifestHasApprovedRootAndGlobalWords(t *testing.T) {
	if !reflect.DeepEqual(manifest.Commands, approvedCompletionRootCommands) {
		t.Fatalf("root commands = %q, want %q", manifest.Commands, approvedCompletionRootCommands)
	}
	if !reflect.DeepEqual(manifest.GlobalFlags, approvedCompletionGlobalFlags) {
		t.Fatalf("global flags = %q, want %q", manifest.GlobalFlags, approvedCompletionGlobalFlags)
	}
}

func TestCompletionScriptsAreStaticCompleteAndDeterministic(t *testing.T) {
	tests := []struct {
		shell string
		file  string
	}{
		{shell: "bash", file: "../../completions/pushover.bash"},
		{shell: "zsh", file: "../../completions/_pushover"},
		{shell: "fish", file: "../../completions/pushover.fish"},
	}
	for _, test := range tests {
		t.Run(test.shell, func(t *testing.T) {
			got, err := completionScript(test.shell)
			if err != nil {
				t.Fatal(err)
			}
			again, err := completionScript(test.shell)
			if err != nil {
				t.Fatal(err)
			}
			if got != again {
				t.Fatalf("%s completion generation is not stable", test.shell)
			}

			want, err := os.ReadFile(filepath.Clean(test.file))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("%s artifact does not match manifest rendering", test.shell)
			}

			for _, word := range approvedCompletionWords {
				needle := word
				if test.shell == "fish" && strings.HasPrefix(word, "--") {
					needle = "-l " + strings.TrimPrefix(word, "--")
				}
				if !strings.Contains(got, needle) {
					t.Fatalf("%s completion omits static word %q", test.shell, word)
				}
			}
			for _, forbidden := range []string{
				"receipt", "refresh", "callback", "auth backend show", "profiles list", "recipients list",
				"config get", "secret", "curl ", "wget ", "http://", "https://", "jq ", "yq ", "alias -",
			} {
				if strings.Contains(strings.ToLower(got), forbidden) {
					t.Fatalf("%s completion contains unsupported or dynamic content %q", test.shell, forbidden)
				}
			}
		})
	}
}

func TestBashCompletionUsesStaticPathAndGlobalCandidates(t *testing.T) {
	tests := []struct {
		name  string
		words []string
		want  []string
	}{
		{
			name:  "root after boolean global",
			words: []string{"pushover", "--json", ""},
			want:  append(append([]string(nil), manifest.Commands...), manifest.GlobalFlags...),
		},
		{
			name:  "root after global value",
			words: []string{"pushover", "--profile", "default", ""},
			want:  append(append([]string(nil), manifest.Commands...), manifest.GlobalFlags...),
		},
		{
			name:  "completion shells and globals",
			words: []string{"pushover", "completion", ""},
			want:  append(append([]string(nil), manifest.CompletionShells...), manifest.GlobalFlags...),
		},
		{
			name:  "profile add flags only",
			words: []string{"pushover", "profiles", "add", "default", ""},
			want:  append(append([]string(nil), manifest.ProfileAddFlags...), manifest.GlobalFlags...),
		},
		{
			name:  "recipient add flags only",
			words: []string{"pushover", "recipients", "add", "phone", ""},
			want:  append(append([]string(nil), manifest.RecipientAddFlags...), manifest.GlobalFlags...),
		},
		{
			name:  "backend destinations and globals",
			words: []string{"pushover", "auth", "backend", "set", ""},
			want:  append(append([]string(nil), manifest.BackendDestinations...), manifest.GlobalFlags...),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := bashCandidates(t, test.words...); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("candidates = %q, want %q", got, test.want)
			}
		})
	}
}

func TestZshCompletionUsesStaticPathAndGlobalCandidates(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	tests := []struct {
		name  string
		words []string
		want  []string
	}{
		{
			name:  "root after global value",
			words: []string{"pushover", "--timeout", "5s", ""},
			want:  append(append([]string(nil), manifest.Commands...), manifest.GlobalFlags...),
		},
		{
			name:  "completion shells and globals",
			words: []string{"pushover", "completion", ""},
			want:  append(append([]string(nil), manifest.CompletionShells...), manifest.GlobalFlags...),
		},
		{
			name:  "profile remove flags only",
			words: []string{"pushover", "profiles", "remove", "default", ""},
			want:  append(append([]string(nil), manifest.ProfileRemoveFlags...), manifest.GlobalFlags...),
		},
		{
			name:  "backend destinations and globals",
			words: []string{"pushover", "auth", "backend", "set", ""},
			want:  append(append([]string(nil), manifest.BackendDestinations...), manifest.GlobalFlags...),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := zshCandidates(t, test.words...); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("candidates = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFishCompletionUsesExactStaticPaths(t *testing.T) {
	script, err := completionScript("fish")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "__fish_seen_subcommand_from") {
		t.Fatalf("fish completion uses broad word matching: %q", script)
	}
	for _, path := range []string{
		"root", "send", "profiles", "profiles-add", "profiles-remove", "recipients", "recipients-add", "recipients-remove", "auth", "auth-backend", "auth-backend-set", "completion",
	} {
		if !strings.Contains(script, "__fish_pushover_path_is "+path) {
			t.Fatalf("fish completion lacks exact path predicate for %q", path)
		}
	}
	if !strings.Contains(script, "__fish_pushover_global_value_pending") {
		t.Fatal("fish completion does not suppress static command words while a global value is pending")
	}
}

func TestFishCompletionResolvesNestedPathWithoutErrors(t *testing.T) {
	if _, err := exec.LookPath("fish"); err != nil {
		t.Skip("fish is not installed")
	}
	script, err := completionScript("fish")
	if err != nil {
		t.Fatal(err)
	}
	probe := "function commandline\n" +
		"    if test \"$argv[1]\" = -opc\n" +
		"        printf '%s\\n' pushover profiles add\n" +
		"    end\n" +
		"end\n\n" +
		script +
		"\nprintf '%s\\n' (__fish_pushover_path)\n"
	command := exec.Command("fish", "-c", probe)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("fish completion probe failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "profiles-add" {
		t.Fatalf("fish path = %q, want %q", got, "profiles-add")
	}
	if got := strings.TrimSpace(stderr.String()); got != "" {
		t.Fatalf("fish completion emitted stderr: %q", got)
	}
}

func bashCandidates(t *testing.T, words ...string) []string {
	t.Helper()
	script, err := completionScript("bash")
	if err != nil {
		t.Fatal(err)
	}
	var input strings.Builder
	input.WriteString(script)
	input.WriteString("COMP_WORDS=(")
	for _, word := range words {
		input.WriteString(strconv.Quote(word))
		input.WriteByte(' ')
	}
	input.WriteString(")\n")
	input.WriteString("COMP_CWORD=")
	input.WriteString(strconv.Itoa(len(words) - 1))
	input.WriteString("\n_pushover\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n")
	command := exec.Command("bash", "-s")
	command.Stdin = strings.NewReader(input.String())
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("bash completion probe failed: %v\n%s", err, output.String())
	}
	return strings.Fields(output.String())
}

func zshCandidates(t *testing.T, words ...string) []string {
	t.Helper()
	script, err := completionScript("zsh")
	if err != nil {
		t.Fatal(err)
	}
	script = strings.TrimSuffix(script, "\n_pushover \"$@\"\n")
	var input strings.Builder
	input.WriteString(script)
	input.WriteString("\n_values() { print -r -- \"__description__=$1\"; shift; print -l -- \"$@\"; }\nwords=(")
	for _, word := range words {
		input.WriteString(strconv.Quote(word))
		input.WriteByte(' ')
	}
	input.WriteString(")\nCURRENT=")
	input.WriteString(strconv.Itoa(len(words)))
	input.WriteString("\n_pushover\n")
	command := exec.Command("zsh", "-f")
	command.Stdin = strings.NewReader(input.String())
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("zsh completion probe failed: %v\n%s", err, output.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "__description__=") {
		t.Fatalf("zsh completion probe returned no description or candidates")
	}
	return lines[1:]
}

func TestCompletionRejectsUnsupportedShell(t *testing.T) {
	if _, err := completionScript("powershell"); err == nil {
		t.Fatal("unsupported shell was accepted")
	}
}
