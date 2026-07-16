// Package domain holds dependency-free Pushover protocol types and rules.
package domain

import "time"

type SecretRef string

type TargetKind string

const (
	KindUnknown   TargetKind = ""
	KindUser      TargetKind = "user"
	KindGroup     TargetKind = "group"
	KindTeamGroup TargetKind = "team-group"
)

type TargetOutcome string

const (
	Planned   TargetOutcome = "planned"
	Sent      TargetOutcome = "sent"
	Failed    TargetOutcome = "failed"
	Ambiguous TargetOutcome = "ambiguous"
	Skipped   TargetOutcome = "skipped"
)

type Credentials struct {
	Token   string
	UserKey string
}

type Target struct {
	Alias       string
	Credentials Credentials
	E2EEKey     []byte
	Kind        TargetKind
	Validated   bool
}

type Attachment struct {
	Name      string
	MediaType string
	Bytes     []byte
}

type Message struct {
	Body, Title, URL, URLTitle string
	Devices                    []string
	Priority                   int
	Timestamp                  *time.Time
	TTL, Retry, Expire         time.Duration
	Sound                      string
	HTML, Monospace, Encrypted bool
	Attachment                 *Attachment
}

type TargetPayload struct {
	Target  Target
	Message Message
}

type ResolvedSendDraft struct {
	Command                           string
	Targets                           []TargetPayload
	DryRun, Validate, ValidateNetwork bool
}

type ErrorDetail struct {
	Code, Category, Message string
	Retryable, Ambiguous    bool
}

type TargetResult struct {
	Alias                string
	Outcome              TargetOutcome
	Priority             int
	RequestID, ReceiptID string
	QuotaRemaining       *int
	Error                *ErrorDetail
}

type BatchResult struct {
	Command     string
	DryRun      bool
	Results     []TargetResult
	Interrupted bool
}
