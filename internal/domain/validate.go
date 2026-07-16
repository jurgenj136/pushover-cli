package domain

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidationError represents a local protocol-rule violation. It intentionally
// contains only field names and fixed messages, never values.
type ValidationError struct {
	Field string
	Rule  string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Rule
	}
	return fmt.Sprintf("%s %s", e.Field, e.Rule)
}

func invalid(field, rule string) error { return &ValidationError{Field: field, Rule: rule} }

func ValidateIdentifier(value string) error {
	if len(value) != IdentifierLength {
		return invalid("identifier", "must be 30 alphanumeric characters")
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return invalid("identifier", "must be 30 alphanumeric characters")
		}
	}
	return nil
}

func ValidateDevice(device string) error {
	if device == "" {
		return nil
	}
	if utf8.RuneCountInString(device) > MaxDeviceRunes {
		return invalid("device", "must be at most 25 characters")
	}
	for _, r := range device {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return invalid("device", "contains unsupported characters")
		}
	}
	return nil
}

func ValidateSavedPriority(priority int) error {
	if priority < PriorityLowest || priority > PriorityNormal {
		return invalid("priority", "must be -2, -1, or 0 when saved")
	}
	return nil
}

func ValidateTargetPayload(payload TargetPayload) error {
	if err := ValidateMessage(payload.Message); err != nil {
		return err
	}
	for _, device := range payload.Message.Devices {
		if err := ValidateDevice(device); err != nil {
			return err
		}
	}
	if payload.Target.Validated && payload.Target.Kind == KindGroup && len(payload.Message.Devices) > 0 {
		return invalid("device", "cannot be used with an ordinary group")
	}
	if payload.Target.Kind != KindUnknown && payload.Target.Kind != KindUser && payload.Target.Kind != KindGroup && payload.Target.Kind != KindTeamGroup {
		return invalid("recipient kind", "is unsupported")
	}
	return nil
}

func ValidateMessage(message Message) error {
	if message.Body == "" {
		return invalid("message", "is required")
	}
	if utf8.RuneCountInString(message.Body) > MaxBodyRunes {
		return invalid("message", "must be at most 1024 characters")
	}
	if utf8.RuneCountInString(message.Title) > MaxTitleRunes {
		return invalid("title", "must be at most 250 characters")
	}
	if utf8.RuneCountInString(message.URL) > MaxURLRunes {
		return invalid("url", "must be at most 512 characters")
	}
	if utf8.RuneCountInString(message.URLTitle) > MaxURLTitleRunes {
		return invalid("url title", "must be at most 100 characters")
	}
	if message.Priority < PriorityLowest || message.Priority > PriorityEmergency {
		return invalid("priority", "must be between -2 and 2")
	}
	if message.TTL < 0 {
		return invalid("ttl", "must be positive")
	}
	if message.URL != "" {
		parsed, err := url.Parse(message.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return invalid("url", "must be an absolute HTTPS URL without credentials")
		}
	}
	if message.URLTitle != "" && message.URL == "" {
		return invalid("url title", "requires a URL")
	}
	if message.HTML && message.Monospace {
		return invalid("formatting", "html and monospace cannot be combined")
	}
	if message.Attachment != nil && len(message.Attachment.Bytes) > MaxAttachmentBytes {
		return invalid("attachment", "exceeds 5242880 bytes")
	}
	if message.Encrypted && (message.Attachment != nil || message.HTML || message.Monospace) {
		return invalid("encrypted message", "cannot use attachments, html, or monospace")
	}
	if message.Priority == PriorityEmergency {
		if message.TTL != 0 {
			return invalid("ttl", "cannot be used with emergency priority")
		}
		if message.Retry < MinEmergencyRetry {
			return invalid("retry", "must be at least 30 seconds for emergency priority")
		}
		if message.Expire <= 0 || message.Expire > MaxEmergencyExpire {
			return invalid("expire", "must be between one second and 10800 seconds for emergency priority")
		}
	} else if message.Retry != 0 || message.Expire != 0 {
		return invalid("retry and expire", "require emergency priority")
	}
	for _, device := range message.Devices {
		if err := ValidateDevice(device); err != nil {
			return err
		}
	}
	return nil
}

// IsSafeName is shared by future config validation. It permits human-friendly
// aliases while rejecting control characters and empty values.
func IsSafeName(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsFunc(value, unicode.IsControl)
}
