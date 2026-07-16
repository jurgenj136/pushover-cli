package preflight

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestValidateWirePayloadUsesByteLimitsForEncryptedFields(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, 16+16+32+1024))
	payload := domain.TargetPayload{Message: domain.Message{Body: encoded, Priority: domain.PriorityNormal, Encrypted: true}}
	if err := validateWirePayload(payload); err == nil {
		t.Fatal("expected oversized encrypted message to be rejected")
	}
}

func TestValidateWirePayloadDoesNotApplyByteLimitsToPlaintextUnicode(t *testing.T) {
	payload := domain.TargetPayload{Message: domain.Message{Body: strings.Repeat("é", domain.MaxBodyRunes), Priority: domain.PriorityNormal}}
	if err := validateWirePayload(payload); err != nil {
		t.Fatalf("plaintext rune-valid Unicode rejected by wire validator: %v", err)
	}
}
