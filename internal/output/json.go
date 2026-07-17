package output

import (
	"encoding/json"
	"io"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type JSONRenderer struct{}

type jsonEnvelope struct {
	SchemaVersion int        `json:"schema_version"`
	Command       string     `json:"command"`
	Status        string     `json:"status"`
	Result        any        `json:"result,omitempty"`
	Error         *jsonError `json:"error,omitempty"`
}
type jsonError struct {
	Code      string `json:"code"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Ambiguous bool   `json:"ambiguous"`
}
type sendJSON struct {
	DryRun  bool             `json:"dry_run"`
	Targets []sendTargetJSON `json:"targets"`
	Counts  sendCountsJSON   `json:"counts"`
}
type sendTargetJSON struct {
	Alias          string               `json:"alias"`
	Outcome        domain.TargetOutcome `json:"outcome"`
	Priority       int                  `json:"priority"`
	RequestID      *string              `json:"request_id"`
	ReceiptID      *string              `json:"receipt_id"`
	QuotaRemaining *int                 `json:"quota_remaining"`
	Error          *sendErrorJSON       `json:"error"`
}
type sendErrorJSON struct {
	Code      string `json:"code"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Ambiguous bool   `json:"ambiguous"`
}
type sendCountsJSON struct {
	Planned   int `json:"planned"`
	Sent      int `json:"sent"`
	Failed    int `json:"failed"`
	Ambiguous int `json:"ambiguous"`
	Skipped   int `json:"skipped"`
}

func (r JSONRenderer) Result(w io.Writer, result CommandResult) error {
	result = normalizeResult(result)
	if err := validateResult(result); err != nil {
		return err
	}
	if send, ok := result.(SendResult); ok {
		if send.Status != deriveSendStatus(send.Batch) {
			return usage("output_status_mismatch", "send status does not match target outcomes")
		}
	}
	doc := jsonEnvelope{SchemaVersion: SchemaVersion, Command: result.CommandName(), Status: result.StatusName(), Result: jsonPayload(result)}
	return writeJSON(w, doc)
}

func (r JSONRenderer) Error(w io.Writer, command string, err *apperror.Error) error {
	if command == "" {
		return usage("output_command_missing", "command is missing")
	}
	if !knownCommand(command) {
		return usage("output_command_mismatch", "unknown command path")
	}
	if err == nil {
		err = apperror.New(apperror.CodeInternal, apperror.CategoryInternal, apperror.MessageInternal)
	}
	detail := apperror.SafeDetail(err)
	doc := jsonEnvelope{SchemaVersion: SchemaVersion, Command: command, Status: "error", Error: &jsonError{Code: detail.Code, Category: detail.Category, Message: detail.Message, Retryable: detail.Retryable, Ambiguous: detail.Ambiguous}}
	return writeJSON(w, doc)
}

func writeJSON(w io.Writer, doc any) error {
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	n, err := w.Write(b)
	if err == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return err
}

func jsonPayload(result CommandResult) any {
	result = normalizeResult(result)
	switch v := result.(type) {
	case SendResult:
		return sendDTO(v.Batch)
	case ProfilesResult:
		rows := make([]profileJSON, 0, len(v.Rows))
		for _, row := range v.Rows {
			rows = append(rows, profileJSON{Name: safeHuman(row.Name), Default: row.Default, Priority: row.Priority, Title: safeHuman(row.Title), Sound: safeHuman(row.Sound)})
		}
		return struct {
			Rows []profileJSON `json:"rows"`
		}{rows}
	case RecipientsResult:
		rows := make([]recipientJSON, 0, len(v.Rows))
		for _, row := range v.Rows {
			rows = append(rows, recipientJSON{Alias: safeHuman(row.Alias), Kind: row.Kind, Validated: row.Validated, Devices: safeStrings(row.Devices)})
		}
		return struct {
			Rows []recipientJSON `json:"rows"`
		}{rows}
	case ValidationsResult:
		rows := make([]validationJSON, 0, len(v.Rows))
		for _, row := range v.Rows {
			rows = append(rows, validationJSON{Alias: safeHuman(row.Alias), Valid: row.Valid, Devices: safeStrings(row.Devices), Licenses: safeStrings(row.Licenses), ErrorCode: safeHuman(row.ErrorCode)})
		}
		return struct {
			Rows []validationJSON `json:"rows"`
		}{rows}
	case SoundsResult:
		rows := make([]soundJSON, 0, len(v.Rows))
		for _, row := range v.Rows {
			rows = append(rows, soundJSON{Name: safeHuman(row.Name), Description: safeHuman(row.Description)})
		}
		return struct {
			Rows []soundJSON `json:"rows"`
		}{rows}
	case LimitsResult:
		return struct {
			Limit     int   `json:"limit"`
			Remaining int   `json:"remaining"`
			Reset     int64 `json:"reset_unix"`
		}{v.Limit, v.Remaining, v.Reset}
	case AuthStatusResult:
		return struct {
			Profile       string `json:"profile"`
			Backend       string `json:"backend"`
			Configured    bool   `json:"configured"`
			TokenResolves bool   `json:"token_resolves"`
		}{safeHuman(v.Profile), safeHuman(v.Backend), v.Configured, v.TokenResolves}
	case BackendResult:
		return struct {
			Backend string `json:"backend"`
		}{safeHuman(v.Backend)}
	case InfoResult:
		return struct {
			ConfigPath string   `json:"config_path"`
			Profile    string   `json:"profile"`
			Backend    string   `json:"backend"`
			Version    string   `json:"version"`
			Commit     string   `json:"commit"`
			BuildDate  string   `json:"build_date"`
			Features   []string `json:"features"`
		}{safeHuman(v.ConfigPath), safeHuman(v.Profile), safeHuman(v.Backend), safeHuman(v.Version), safeHuman(v.Commit), safeHuman(v.BuildDate), safeStrings(v.Features)}
	case VersionResult:
		return struct {
			Version   string `json:"version"`
			Commit    string `json:"commit"`
			BuildDate string `json:"build_date"`
		}{safeHuman(v.Version), safeHuman(v.Commit), safeHuman(v.BuildDate)}
	case MutationResult:
		return struct {
			Resource string   `json:"resource"`
			Name     string   `json:"name"`
			Action   string   `json:"action"`
			Warnings []string `json:"warnings"`
		}{safeHuman(v.Resource), safeHuman(v.Name), safeHuman(v.Action), safeStrings(v.Warnings)}
	case CompletionResult:
		return struct {
			Shell  string `json:"shell"`
			Script string `json:"script"`
		}{safeHuman(v.Shell), v.Script}
	default:
		return nil
	}
}

type recipientJSON struct {
	Alias     string            `json:"alias"`
	Kind      domain.TargetKind `json:"kind"`
	Validated bool              `json:"validated"`
	Devices   []string          `json:"devices"`
}
type profileJSON struct {
	Name     string `json:"name"`
	Default  bool   `json:"default"`
	Priority int    `json:"priority"`
	Title    string `json:"title"`
	Sound    string `json:"sound"`
}
type soundJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type validationJSON struct {
	Alias     string   `json:"alias"`
	Valid     bool     `json:"valid"`
	Devices   []string `json:"devices"`
	Licenses  []string `json:"licenses"`
	ErrorCode string   `json:"error_code"`
}

func sendDTO(batch domain.BatchResult) sendJSON {
	targets := make([]sendTargetJSON, 0, len(batch.Results))
	c := sendCounts(batch.Results)
	for _, target := range batch.Results {
		var req, receipt *string
		if target.RequestID != "" {
			x := target.RequestID
			req = &x
		}
		if target.ReceiptID != "" {
			x := target.ReceiptID
			receipt = &x
		}
		var e *sendErrorJSON
		if target.Error != nil {
			e = &sendErrorJSON{Code: safeHuman(target.Error.Code), Category: safeHuman(target.Error.Category), Message: safeHuman(target.Error.Message), Retryable: false, Ambiguous: target.Outcome == domain.Ambiguous}
		}
		targets = append(targets, sendTargetJSON{Alias: safeHuman(target.Alias), Outcome: target.Outcome, Priority: target.Priority, RequestID: sanitizePtr(req), ReceiptID: sanitizePtr(receipt), QuotaRemaining: target.QuotaRemaining, Error: e})
	}
	return sendJSON{DryRun: batch.DryRun, Targets: targets, Counts: sendCountsJSON(c)}
}

func safeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, safeHuman(value))
	}
	return out
}
func sanitizePtr(value *string) *string {
	if value == nil {
		return nil
	}
	out := safeHuman(*value)
	return &out
}
