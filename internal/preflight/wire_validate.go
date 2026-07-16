package preflight

import (
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

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
			name  string
			value string
			limit int
		}{
			{"message", message.Body, domain.MaxBodyRunes},
			{"title", message.Title, domain.MaxTitleRunes},
			{"url", message.URL, domain.MaxURLRunes},
			{"url title", message.URLTitle, domain.MaxURLTitleRunes},
		} {
			if field.value == "" {
				continue
			}
			if len(field.value) > field.limit || !validEnvelope(field.value) {
				return wireError(field.name)
			}
		}
	}
	return nil
}

func validEnvelope(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(decoded) >= 64 && (len(decoded)-16-32)%16 == 0
}

func wireError(field string) error {
	return apperror.New("wire_limit", apperror.CategoryValidation, fmt.Sprintf("%s exceeds the Pushover wire limit", field))
}
