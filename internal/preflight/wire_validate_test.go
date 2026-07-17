package preflight

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/e2ee"
)

func TestValidateWirePayloadAcceptsEncryptedMaximumUnicodeBody(t *testing.T) {
	body := maximumPoorlyCompressibleUnicode(domain.MaxBodyRunes)
	if got := utf8.RuneCountInString(body); got != domain.MaxBodyRunes {
		t.Fatalf("Unicode body rune count = %d, want %d", got, domain.MaxBodyRunes)
	}
	if got, want := len(body), domain.MaxBodyRunes*utf8.UTFMax; got != want {
		t.Fatalf("Unicode body byte length = %d, want %d", got, want)
	}

	payload, err := (e2ee.Encryptor{Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))}).EncryptPayload(context.Background(), domain.TargetPayload{
		Target:  domain.Target{E2EEKey: []byte(strings.Repeat("a", 64))},
		Message: domain.Message{Body: body, Priority: domain.PriorityNormal, Encrypted: true},
	})
	if err != nil {
		t.Fatalf("EncryptPayload() error = %v", err)
	}
	if err := validateWirePayload(payload); err != nil {
		t.Fatalf("maximum encrypted Unicode body rejected by wire validator: %v", err)
	}
}

func TestValidateWirePayloadRejectsMalformedEncryptedEnvelope(t *testing.T) {
	payload := domain.TargetPayload{Message: domain.Message{Body: "YQ==", Priority: domain.PriorityNormal, Encrypted: true}}
	if err := validateWirePayload(payload); err == nil {
		t.Fatal("malformed encrypted envelope was accepted")
	}
}

func TestValidateWirePayloadRejectsGrosslyOversizedEncryptedEnvelope(t *testing.T) {
	const envelopeOverhead = 16 + 32
	encoded := base64.StdEncoding.EncodeToString(make([]byte, envelopeOverhead+16*domain.MaxBodyRunes*utf8.UTFMax))
	if !validEnvelope(encoded) {
		t.Fatal("oversized test envelope is not structurally valid")
	}
	payload := domain.TargetPayload{Message: domain.Message{Body: encoded, Priority: domain.PriorityNormal, Encrypted: true}}
	if err := validateWirePayload(payload); err == nil {
		t.Fatal("grossly oversized encrypted envelope was accepted")
	}
}

func TestValidateWirePayloadDoesNotApplyByteLimitsToPlaintextUnicode(t *testing.T) {
	payload := domain.TargetPayload{Message: domain.Message{Body: strings.Repeat("é", domain.MaxBodyRunes), Priority: domain.PriorityNormal}}
	if err := validateWirePayload(payload); err != nil {
		t.Fatalf("plaintext rune-valid Unicode rejected by wire validator: %v", err)
	}
}

func maximumPoorlyCompressibleUnicode(runes int) string {
	var body strings.Builder
	body.Grow(runes * utf8.UTFMax)
	state := uint32(0x13579bdf)
	for range runes {
		state = state*1664525 + 1013904223
		body.WriteRune(rune(0x10000 + state&0xfffff))
	}
	return body.String()
}
