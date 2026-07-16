package apperror

import (
	"sort"
	"strings"
	"unicode"
)

// Sanitize makes untrusted text terminal-safe and removes registered secret
// values. Controls become one space, runs of whitespace are collapsed, and DEL
// is treated as a control character. Secrets are removed rather than replaced
// with a fixed marker: no universal marker can avoid colliding with every
// possible registered secret. A final pass prevents normalization or adjacent
// removals from synthesizing a secret that was not contiguous in the input.
func Sanitize(value string, secrets ...string) string {
	values := append([]string(nil), secrets...)
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	value = removeSecrets(value, values)
	filtered := strings.Map(func(r rune) rune {
		if r == 0x7f || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return removeSecrets(strings.Join(strings.Fields(filtered), " "), values)
}

func removeSecrets(value string, secrets []string) string {
	for {
		previous := value
		for _, secret := range secrets {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "")
			}
		}
		if value == previous {
			return value
		}
	}
}
