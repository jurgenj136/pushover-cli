package attachment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestLocalSnapshotsBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-name.png")
	original := pngWithDimensions(t, 1, 1)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	attachment, err := Local(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pngWithDimensions(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	if string(attachment.Bytes) != string(original) {
		t.Fatal("snapshot changed after source mutation")
	}
}

func TestLocalRejectsMissingNonRegularAndOversized(t *testing.T) {
	dir := t.TempDir()
	for name, path := range map[string]string{
		"missing":     filepath.Join(dir, "very-secret-name.png"),
		"non-regular": dir,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Local(context.Background(), path)
			if err == nil || strings.Contains(err.Error(), path) {
				t.Fatalf("unsafe local error: %v", err)
			}
		})
	}
	path := filepath.Join(dir, "large.png")
	data := append(pngWithDimensions(t, 1, 1), make([]byte, domain.MaxAttachmentBytes-len(pngWithDimensions(t, 1, 1)))...)
	if len(data) != domain.MaxAttachmentBytes {
		t.Fatal("bad exact boundary fixture")
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Local(context.Background(), path); err != nil {
		t.Fatalf("exact boundary rejected: %v", err)
	}
	if err := os.WriteFile(path, append(data, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Local(context.Background(), path); err == nil {
		t.Fatal("one-byte overflow accepted")
	}
}
