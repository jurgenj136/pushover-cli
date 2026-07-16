package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionArtifactsAreStaticManifestRenderings(t *testing.T) {
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
			want, err := os.ReadFile(filepath.Clean(test.file))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("%s artifact does not match manifest rendering", test.shell)
			}
			for _, forbidden := range []string{"profile list", "recipient list", "config get", "alias -"} {
				if strings.Contains(strings.ToLower(got), forbidden) {
					t.Fatalf("%s completion dynamically discovers %q: %q", test.shell, forbidden, got)
				}
			}
		})
	}
}
