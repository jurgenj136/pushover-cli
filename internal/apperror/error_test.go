package apperror

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeDetailRedactsCauseAndControls(t *testing.T) {
	secret := "token-very-secret"
	err := Wrap("api_failure", CategoryValidation, "upstream \\x1b[31mfailed "+secret, errors.New("cause has "+secret), WithSecrets(secret), WithRetryable(true), WithAmbiguous(true))
	detail := SafeDetail(err)
	if detail == nil || detail.Code != "api_failure" || !detail.Retryable || !detail.Ambiguous {
		t.Fatalf("unexpected detail: %#v", detail)
	}
	if strings.Contains(detail.Message, secret) || strings.Contains(detail.Message, "\x1b") || strings.Contains(detail.Message, "cause") {
		t.Fatalf("unsafe detail: %q", detail.Message)
	}
	if got := err.Error(); strings.Contains(got, secret) || strings.Contains(got, "cause") {
		t.Fatalf("unsafe error string: %q", got)
	}
}

func TestSafeDetailGenericError(t *testing.T) {
	detail := SafeDetail(errors.New("bad\nthing"))
	if detail.Code != CodeInternal || detail.Category != CategoryInternal || detail.Message != "bad thing" {
		t.Fatalf("unexpected generic detail: %#v", detail)
	}
}

func FuzzSanitizeError(f *testing.F) {
	f.Add("hello\x1b[31m", "secret")
	f.Fuzz(func(t *testing.T, message, secret string) {
		if len(message)+len(secret) > 8*1024 {
			t.Skip()
		}
		got := Sanitize(message, secret)
		if strings.Contains(got, "\x1b") || (secret != "" && strings.Contains(got, secret)) {
			t.Fatalf("unsafe sanitized value: %q", got)
		}
	})
}
