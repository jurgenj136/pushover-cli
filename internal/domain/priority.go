package domain

import "time"

const (
	PriorityLowest    = -2
	PriorityLow       = -1
	PriorityNormal    = 0
	PriorityHigh      = 1
	PriorityEmergency = 2

	IdentifierLength    = 30
	MaxDeviceRunes      = 25
	MaxBodyRunes        = 1024
	MaxTitleRunes       = 250
	MaxURLRunes         = 512
	MaxURLTitleRunes    = 100
	MaxAttachmentBytes  = 5_242_880
	MinEmergencyRetry   = 30 * time.Second
	MaxEmergencyExpire  = 3 * time.Hour
	MaxEmergencyRetries = 50
)

// EffectiveRetryCount is the documented Pushover delivery cap, not a local
// validation constraint. API-valid emergency messages may mathematically exceed it.
func EffectiveRetryCount(retry, expire time.Duration) int {
	if retry <= 0 || expire <= 0 {
		return 0
	}
	count := int((expire + retry - 1) / retry)
	if count > MaxEmergencyRetries {
		return MaxEmergencyRetries
	}
	return count
}
