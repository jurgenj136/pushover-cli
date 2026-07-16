package apperror

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSafeDetailRedactsCauseAndControls(t *testing.T) {
	secret := "token-very-secret"
	err := Wrap("api_failure", CategoryValidation, "upstream \x1b[31mfailed \u009b31m"+secret, errors.New("cause has "+secret), WithSecrets(secret), WithRetryable(true), WithAmbiguous(true))
	detail := SafeDetail(err)
	if detail == nil || detail.Code != "api_failure" || !detail.Retryable || !detail.Ambiguous {
		t.Fatalf("unexpected detail: %#v", detail)
	}
	if strings.Contains(detail.Message, secret) || strings.ContainsRune(detail.Message, '\x1b') || strings.ContainsRune(detail.Message, '\u009b') || strings.Contains(detail.Message, "cause") {
		t.Fatalf("unsafe detail: %q", detail.Message)
	}
	if got := err.Error(); strings.Contains(got, secret) || strings.Contains(got, "cause") {
		t.Fatalf("unsafe error string: %q", got)
	}
}

func TestSafeDetailGenericError(t *testing.T) {
	detail := SafeDetail(errors.New("untrusted token-very-secret\nthing"))
	if detail.Code != CodeInternal || detail.Category != CategoryInternal || detail.Message != MessageInternal {
		t.Fatalf("unexpected generic detail: %#v", detail)
	}
}

func TestSafeDetailTypedNilAndUnknownCategory(t *testing.T) {
	var typedNil *Error
	var err error = typedNil
	detail := SafeDetail(err)
	if detail == nil || detail.Code != CodeInternal || detail.Category != CategoryInternal || detail.Message != MessageInternal {
		t.Fatalf("typed nil detail = %#v", detail)
	}
	unknown := New("unsafe", "unknown\x1b", "safe")
	if detail := SafeDetail(unknown); detail.Category != CategoryInternal {
		t.Fatalf("unknown category projected as %q", detail.Category)
	}
}

func TestErrorFormattingNeverExposesCauseOrSecrets(t *testing.T) {
	secret := "token-very-secret"
	err := Wrap("api_failure", CategoryValidation, "safe "+secret+"\x1b[31m", errors.New("cause "+secret), WithSecrets(secret))
	wrapped := fmt.Errorf("outer: %w", err)
	for _, format := range []string{"%v", "%+v", "%#v", "%q"} {
		for _, value := range []error{err, wrapped} {
			rendered := fmt.Sprintf(format, value)
			if strings.Contains(rendered, secret) || strings.Contains(rendered, "cause") || strings.ContainsRune(rendered, '\x1b') {
				t.Fatalf("unsafe formatted error for %q: %q", format, rendered)
			}
		}
	}
}

func TestSanitizeFinalPassAndMarkerCollision(t *testing.T) {
	for _, test := range []struct {
		name    string
		message string
		secrets []string
	}{
		{"control synthesizes whitespace secret", "left\x1bright", []string{" "}},
		{"normalization synthesizes whitespace secret", "left\tright", []string{" "}},
		{"legacy marker is itself secret", "before [redacted] after", []string{"[redacted]"}},
		{"one removal synthesizes another secret", "aXb", []string{"X", "ab"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Sanitize(test.message, test.secrets...)
			for _, secret := range test.secrets {
				if secret != "" && strings.Contains(got, secret) {
					t.Fatalf("sanitized output %q contains secret %q", got, secret)
				}
			}
		})
	}
}

func FuzzSanitizeError(f *testing.F) {
	f.Add("hello\x1b[31m", "secret")
	f.Fuzz(func(t *testing.T, message, secret string) {
		if len(message)+len(secret) > 8*1024 {
			t.Skip()
		}
		got := Sanitize(message+secret, secret)
		if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\u009b') {
			t.Fatalf("unsafe sanitized value: %q", got)
		}
		if secret != "" && strings.Contains(got, secret) {
			t.Fatalf("sanitized output retained registered secret")
		}
	})
}
