package pushover

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSendErrorFormattingNeverLeaksCause(t *testing.T) {
	secret := "token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&path=/private/message"
	pointer := newSendError(errors.New(secret), PhaseRead, true)
	value := *pointer
	for _, rendered := range []string{fmt.Sprint(pointer), fmt.Sprintf("%+v", pointer), fmt.Sprintf("%#v", pointer), fmt.Errorf("outer: %w", pointer).Error(), fmt.Sprint(value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value)} {
		if strings.Contains(rendered, "aaaaaaaa") || strings.Contains(rendered, "/private") {
			t.Fatalf("cause leaked through formatting: %q", rendered)
		}
	}
}
