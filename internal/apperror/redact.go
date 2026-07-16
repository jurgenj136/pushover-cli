package apperror

import (
	"sort"
	"strings"
	"unicode"
)

const redacted = "[redacted]"

// Sanitize makes untrusted text terminal-safe and removes registered secret
// values. Controls become one space, runs of whitespace are collapsed, and DEL
// is treated as a control character.
func Sanitize(value string, secrets ...string) string {
	values := append([]string(nil), secrets...)
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, secret := range values {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, redacted)
		}
	}
	filtered := strings.Map(func(r rune) rune {
		if r == 0x7f || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(filtered), " ")
}
