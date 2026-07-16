package delivery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
)

func TestTerminalConfirmerTTYContractAndSanitizedSummary(t *testing.T) {
	summary := preflight.Confirmation{
		Aliases:          []string{"ops\nspoof"},
		Priority:         2,
		Retry:            30 * time.Second,
		Expire:           90 * time.Second,
		EffectiveRetries: 50,
		Encrypted:        true,
	}
	t.Run("non tty requires yes", func(t *testing.T) {
		err := (TerminalConfirmer{Input: strings.NewReader("yes\n")}).Confirm(context.Background(), summary)
		if !apperror.Is(err, apperror.CategoryConfirmation) {
			t.Fatalf("err=%v, want typed confirmation error", err)
		}
	})
	t.Run("tty accepts yes and renders only sanitized summary", func(t *testing.T) {
		var stderr bytes.Buffer
		err := (TerminalConfirmer{
			Input:  strings.NewReader("yes\n"),
			Stderr: &stderr,
			IsTTY:  func(io.Reader) bool { return true },
		}).Confirm(context.Background(), summary)
		if err != nil {
			t.Fatal(err)
		}
		output := stderr.String()
		for _, forbidden := range []string{"ops\nspoof", "secret-body", "https://private.example"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("prompt leaked %q in %q", forbidden, output)
			}
		}
		for _, required := range []string{"ops spoof", "priority 2", "retry 30s", "expire 1m30s", "retries 50", "encrypted true"} {
			if !strings.Contains(output, required) {
				t.Fatalf("prompt missing %q in %q", required, output)
			}
		}
	})
	t.Run("tty decline is typed", func(t *testing.T) {
		err := (TerminalConfirmer{Input: strings.NewReader("no\n"), Stderr: io.Discard, IsTTY: func(io.Reader) bool { return true }}).Confirm(context.Background(), summary)
		if !apperror.Is(err, apperror.CategoryConfirmation) {
			t.Fatalf("err=%v, want typed confirmation error", err)
		}
	})
	t.Run("prompt write failure does not send", func(t *testing.T) {
		err := (TerminalConfirmer{Input: strings.NewReader("yes\n"), Stderr: errWriter{}, IsTTY: func(io.Reader) bool { return true }}).Confirm(context.Background(), summary)
		if !apperror.Is(err, apperror.CategoryConfirmation) {
			t.Fatalf("err=%v, want typed confirmation error", err)
		}
	})
}

func TestDeliveryDetailNormalizesAmbiguousUnknownFailure(t *testing.T) {
	detail := deliveryDetail(errors.New("hostile transport text"), true)
	if detail.Code != "network_failure" || detail.Category != apperror.CategoryNetwork || !detail.Ambiguous {
		t.Fatalf("detail = %#v", detail)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
