package output

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
)

func safeHuman(value string) string { return apperror.Sanitize(value) }

func tsvEscape(value string) string {
	var b strings.Builder
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			b.WriteString(`\u00FF`)
			value = value[1:]
			continue
		}
		value = value[size:]
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		default:
			if r <= 0x1f || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				_, _ = fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func tsvList(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, tsvEscape(value))
	}
	return strings.Join(parts, ",")
}
