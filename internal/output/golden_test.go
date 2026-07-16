package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestGoldenSendJSON(t *testing.T) {
	result := SendResult{Meta: Meta{"send", "success"}, Batch: domain.BatchResult{Command: "send", Results: []domain.TargetResult{{Alias: "ops", Outcome: domain.Sent, Priority: 0, RequestID: "req-1"}}}}
	var got bytes.Buffer
	if err := (JSONRenderer{}).Result(&got, result); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "send-json-success.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("golden mismatch\nwant %q\ngot  %q", want, got.Bytes())
	}
}
