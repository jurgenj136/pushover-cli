package e2ee

import (
	"strings"
	"testing"
)

func TestParseKey(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef0123456789ABCDEF0123456789ABCDEF"
	got, err := ParseKey(valid)
	if err != nil {
		t.Fatalf("ParseKey() error = %v", err)
	}
	if got[0] != 0x01 || got[31] != 0xef {
		t.Fatalf("ParseKey() decoded unexpected bytes: %x", got)
	}

	for _, key := range []string{
		"",
		strings.Repeat("0", 63),
		strings.Repeat("0", 65),
		strings.Repeat("g", 64),
		" " + strings.Repeat("0", 63),
		strings.Repeat("0", 63) + "\n",
	} {
		if _, err := ParseKey(key); err == nil || err.Error() != "invalid encryption key" {
			t.Errorf("ParseKey(%q) error = %v, want fixed invalid-key error", key, err)
		}
	}
}
