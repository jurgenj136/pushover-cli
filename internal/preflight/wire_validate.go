package preflight

import (
	"crypto/aes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"unicode/utf8"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const maxGzipWireOverhead = 32

// validateWirePayload applies only transformed-wire constraints. In
// particular, it never applies plaintext rune limits to encrypted Base64.
func validateWirePayload(payload domain.TargetPayload) error {
	message := payload.Message
	if message.Body == "" {
		return wireError("message")
	}
	if message.Priority < domain.PriorityLowest || message.Priority > domain.PriorityEmergency || message.TTL < 0 {
		return wireError("priority")
	}
	if message.URLTitle != "" && message.URL == "" {
		return wireError("url title")
	}
	if message.URL != "" && !message.Encrypted {
		parsed, err := url.Parse(message.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return wireError("url")
		}
	}
	if message.Priority == domain.PriorityEmergency {
		if message.TTL != 0 || message.Retry < domain.MinEmergencyRetry || message.Expire <= 0 || message.Expire > domain.MaxEmergencyExpire {
			return wireError("retry")
		}
	} else if message.Retry != 0 || message.Expire != 0 {
		return wireError("retry")
	}
	if message.Encrypted {
		for _, field := range []struct {
			name     string
			value    string
			maxRunes int
		}{
			{"message", message.Body, domain.MaxBodyRunes},
			{"title", message.Title, domain.MaxTitleRunes},
			{"url", message.URL, domain.MaxURLRunes},
			{"url title", message.URLTitle, domain.MaxURLTitleRunes},
		} {
			if field.value == "" {
				continue
			}
			if len(field.value) > encryptedWireLimit(field.maxRunes) || !validEnvelope(field.value) {
				return wireError(field.name)
			}
		}
	}
	return nil
}

// encryptedWireLimit bounds the Base64 envelope produced by e2ee.encryptField
// for a rune-limited plaintext. Each supported rune may occupy utf8.UTFMax
// bytes. e2ee.gzipBytes uses one gzip member with a 10-byte header and 8-byte
// footer; for every supported field, its DEFLATE payload is one data block
// capped by a stored block (plaintext plus 5 bytes), followed by a 5-byte
// final empty block. maxGzipWireOverhead conservatively rounds that 28-byte
// gzip overhead to 32 bytes before CBC padding, IV/HMAC, and Base64 expansion.
func encryptedWireLimit(maxRunes int) int {
	maxPlaintextBytes := maxRunes * utf8.UTFMax
	maxCompressedBytes := maxPlaintextBytes + maxGzipWireOverhead
	paddedBytes := maxCompressedBytes + aes.BlockSize - maxCompressedBytes%aes.BlockSize
	envelopeBytes := aes.BlockSize + paddedBytes + sha256.Size
	return base64.StdEncoding.EncodedLen(envelopeBytes)
}

func validEnvelope(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(decoded) >= 64 && (len(decoded)-16-32)%16 == 0
}

func wireError(field string) error {
	return apperror.New("wire_limit", apperror.CategoryValidation, fmt.Sprintf("%s exceeds the Pushover wire limit", field))
}
