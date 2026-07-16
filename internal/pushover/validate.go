package pushover

import (
	"crypto/aes"
	"crypto/sha256"
	"encoding/base64"
	"mime"
	"net/url"
	"strings"

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
	if payload.Message.Encrypted {
		return validateEncryptedPayload(payload)
	}
	if err := domain.ValidateTargetPayload(payload); err != nil {
		return err
	}
	if payload.Message.Attachment != nil {
		_, err := attachmentContentType(payload.Message.Attachment.MediaType)
		return err
	}
	return nil
}

func validateEncryptedPayload(payload domain.TargetPayload) error {
	if err := domain.ValidateCredentials(payload.Target.Credentials); err != nil {
		return err
	}
	message := payload.Message
	if message.Body == "" || message.Priority < domain.PriorityLowest || message.Priority > domain.PriorityEmergency || message.TTL < 0 {
		return fixedError("invalid_payload")
	}
	if message.URLTitle != "" && message.URL == "" {
		return fixedError("invalid_payload")
	}
	if message.HTML || message.Monospace || message.Attachment != nil {
		return fixedError("invalid_payload")
	}
	if message.Priority == domain.PriorityEmergency {
		if message.TTL != 0 || message.Retry < domain.MinEmergencyRetry || message.Expire <= 0 || message.Expire > domain.MaxEmergencyExpire {
			return fixedError("invalid_payload")
		}
	} else if message.Retry != 0 || message.Expire != 0 {
		return fixedError("invalid_payload")
	}
	if payload.Target.Kind != domain.KindUnknown && payload.Target.Kind != domain.KindUser && payload.Target.Kind != domain.KindGroup && payload.Target.Kind != domain.KindTeamGroup {
		return fixedError("invalid_payload")
	}
	if payload.Target.Kind == domain.KindGroup && len(message.Devices) > 0 {
		return fixedError("invalid_payload")
	}
	for _, device := range message.Devices {
		if err := domain.ValidateDevice(device); err != nil {
			return err
		}
	}
	for _, field := range []string{message.Body, message.Title, message.URL, message.URLTitle} {
		if field == "" {
			continue
		}
		if err := validateEncryptedField(field); err != nil {
			return fixedError("invalid_payload")
		}
	}
	return nil
}

func validateEncryptedField(field string) error {
	if len(field) > maxResponseBytes {
		return fixedError("invalid_payload")
	}
	envelope, err := base64.StdEncoding.DecodeString(field)
	if err != nil {
		return err
	}
	if len(envelope) < aes.BlockSize+aes.BlockSize+sha256.Size {
		return fixedError("invalid_payload")
	}
	ciphertextBytes := len(envelope) - aes.BlockSize - sha256.Size
	if ciphertextBytes%aes.BlockSize != 0 {
		return fixedError("invalid_payload")
	}
	return nil
}

func attachmentContentType(value string) (string, error) {
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || (mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif") {
		return "", fixedError("invalid_payload")
	}
	return mime.FormatMediaType(mediaType, parameters), nil
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
