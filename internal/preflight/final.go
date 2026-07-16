package preflight

import (
	"time"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

// Confirmation is the sanitized emergency confirmation summary. It contains
// no credentials, body text, attachment locations, or URLs.
type Confirmation struct {
	Aliases          []string
	Priority         int
	Retry            time.Duration
	Expire           time.Duration
	EffectiveRetries int
	Encrypted        bool
}

// FinalizedSendPlan is sealed by Builder. Its payloads cannot be supplied by
// callers or forged through exported fields.
type FinalizedSendPlan struct {
	targets      []domain.TargetPayload
	confirmation Confirmation
	sealed       *planSeal
}

type planSeal struct{}

var finalPlanSeal = &planSeal{}

func (p FinalizedSendPlan) zero() bool { return p.sealed != finalPlanSeal || len(p.targets) == 0 }

// IsValid reports whether the plan was sealed by a successful real preflight.
// A valid plan always contains at least one target.
func (p FinalizedSendPlan) IsValid() bool { return !p.zero() }

// Targets returns a complete defensive copy of the finalized payloads.
func (p FinalizedSendPlan) Targets() []domain.TargetPayload {
	if p.zero() {
		return nil
	}
	result := make([]domain.TargetPayload, len(p.targets))
	for i := range p.targets {
		result[i] = clonePayload(p.targets[i])
	}
	return result
}

// Confirmation returns a defensive copy of the sanitized summary.
func (p FinalizedSendPlan) Confirmation() Confirmation {
	if p.zero() {
		return Confirmation{}
	}
	result := p.confirmation
	result.Aliases = append([]string(nil), p.confirmation.Aliases...)
	return result
}

func clonePayload(input domain.TargetPayload) domain.TargetPayload {
	result := input
	result.Target.E2EEKey = append([]byte(nil), input.Target.E2EEKey...)
	result.Message.Devices = append([]string(nil), input.Message.Devices...)
	if input.Message.Timestamp != nil {
		timestamp := *input.Message.Timestamp
		result.Message.Timestamp = &timestamp
	}
	if input.Message.Attachment != nil {
		attachment := *input.Message.Attachment
		attachment.Bytes = append([]byte(nil), input.Message.Attachment.Bytes...)
		result.Message.Attachment = &attachment
	}
	return result
}
