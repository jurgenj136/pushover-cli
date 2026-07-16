package pushover

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSendErrorFormattingNeverLeaksCause(t *testing.T) {
	secret := "token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&path=/private/message"
	err := newSendError(errors.New(secret), PhaseRead, true)
	for _, rendered := range []string{fmt.Sprint(err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Errorf("outer: %w", err).Error()} {
		if strings.Contains(rendered, "aaaaaaaa") || strings.Contains(rendered, "/private") {
			t.Fatalf("cause leaked through formatting: %q", rendered)
		}
	}
}
