package pushover

import (
	"encoding/base64"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func validateHTTPSBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fixedError("invalid_endpoint")
	}
	return nil
}

func trimBaseURL(raw string) string { return strings.TrimSuffix(raw, "/") }

func validatePayload(payload domain.TargetPayload) error {
	if !payload.Message.Encrypted {
		return domain.ValidateTargetPayload(payload)
	}
	if err := domain.ValidateCredentials(payload.Target.Credentials); err != nil {
		return err
	}
	message := payload.Message
	if message.Body == "" || message.Priority < domain.PriorityLowest || message.Priority > domain.PriorityEmergency {
		return fixedError("invalid_payload")
	}
	for _, field := range []string{message.Body, message.Title, message.URL, message.URLTitle} {
		if field == "" {
			continue
		}
		if !utf8.ValidString(field) || len(field) > maxResponseBytes {
			return fixedError("invalid_payload")
		}
		if _, err := base64.StdEncoding.DecodeString(field); err != nil {
			return fixedError("invalid_payload")
		}
	}
	if message.Attachment != nil || message.HTML || message.Monospace {
		return fixedError("invalid_payload")
	}
	for _, device := range message.Devices {
		if err := domain.ValidateDevice(device); err != nil {
			return err
		}
	}
	return nil
}

func fixedError(code string) *apperror.Error {
	switch code {
	case "invalid_endpoint", "invalid_client", "invalid_payload":
		return apperror.New(code, apperror.CategoryValidation, "invalid Pushover request")
	case "invalid_response":
		return apperror.New(code, apperror.CategoryNetwork, "Pushover returned an invalid response")
	default:
		return apperror.New("network_failure", apperror.CategoryNetwork, "Pushover request failed", apperror.WithRetryable(true))
	}
}
