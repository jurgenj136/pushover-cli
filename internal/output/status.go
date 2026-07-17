package output

import (
	"fmt"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

var sendStatuses = map[string]bool{"success": true, "partial": true, "failed": true, "ambiguous": true, "interrupted": true, "dry_run": true}

func usage(code, message string) *apperror.Error {
	return apperror.New(code, apperror.CategoryUsage, message)
}

type counts struct{ Planned, Sent, Failed, Ambiguous, Skipped int }

func sendCounts(results []domain.TargetResult) counts {
	var c counts
	for _, r := range results {
		switch r.Outcome {
		case domain.Planned:
			c.Planned++
		case domain.Sent:
			c.Sent++
		case domain.Failed:
			c.Failed++
		case domain.Ambiguous:
			c.Ambiguous++
		case domain.Skipped:
			c.Skipped++
		}
	}
	return c
}

func deriveSendStatus(batch domain.BatchResult) string {
	if batch.DryRun {
		return "dry_run"
	}
	c := sendCounts(batch.Results)
	if !batch.Interrupted && c.Sent == len(batch.Results) && c.Failed == 0 && c.Ambiguous == 0 && c.Skipped == 0 {
		return "success"
	}
	if c.Sent == 0 && c.Ambiguous > 0 {
		return "ambiguous"
	}
	if batch.Interrupted && c.Sent == 0 && c.Ambiguous == 0 {
		return "interrupted"
	}
	if c.Sent == 0 && c.Failed > 0 && c.Ambiguous == 0 {
		return "failed"
	}
	return "partial"
}

func validateResult(result CommandResult) error {
	result = normalizeResult(result)
	if result == nil {
		return usage("output_result_nil", "result is nil")
	}
	command, status := result.CommandName(), result.StatusName()
	if command == "" {
		return usage("output_command_missing", "command is missing")
	}
	if status == "" {
		return usage("output_status_missing", "status is missing")
	}
	if send, ok := result.(SendResult); ok {
		if command != "send" || send.Batch.Command != "send" {
			return usage("output_command_mismatch", "send result command does not match")
		}
		if !sendStatuses[status] {
			return usage("output_status_invalid", "invalid send status")
		}
		if len(send.Batch.Results) == 0 {
			return usage("output_send_empty", "send result has no targets")
		}
		if send.Batch.DryRun && send.Batch.Interrupted {
			return usage("output_batch_invalid", "dry-run send cannot be interrupted")
		}
		for _, target := range send.Batch.Results {
			switch target.Outcome {
			case domain.Planned, domain.Sent, domain.Failed, domain.Ambiguous, domain.Skipped:
			default:
				return usage("output_outcome_invalid", "invalid send outcome")
			}
			if send.Batch.DryRun && target.Outcome != domain.Planned {
				return usage("output_outcome_invalid", "dry-run send must contain planned targets")
			}
			if !send.Batch.DryRun && target.Outcome == domain.Planned {
				return usage("output_outcome_invalid", "non-dry-run send cannot contain planned targets")
			}
			if err := validateTargetResult(target); err != nil {
				return err
			}
		}
		c := sendCounts(send.Batch.Results)
		if c.Skipped == len(send.Batch.Results) && !send.Batch.Interrupted {
			return usage("output_batch_invalid", "skipped send targets require interruption")
		}
		return nil
	}
	if status != "success" {
		return usage("output_status_invalid", "ordinary result status must be success")
	}
	if !knownPairing(command, result) {
		return usage("output_command_mismatch", fmt.Sprintf("result type does not match command %q", command))
	}
	if info, ok := result.(InfoResult); ok {
		if err := validateInfoFeatures(info.Features); err != nil {
			return err
		}
	}
	return nil
}

func validateInfoFeatures(features []string) error {
	if len(features) == 0 {
		return usage("output_features_missing", "info result must include supported features")
	}
	for i, feature := range features {
		if !safeFeatureIdentifier(feature) {
			return usage("output_feature_invalid", "info feature must be a lowercase identifier")
		}
		if i == 0 {
			continue
		}
		if feature == features[i-1] {
			return usage("output_features_duplicate", "info features must be unique")
		}
		if feature < features[i-1] {
			return usage("output_features_unsorted", "info features must be sorted")
		}
	}
	return nil
}

func safeFeatureIdentifier(value string) bool {
	if value == "" {
		return false
	}
	first := value[0]
	if (first < 'a' || first > 'z') && (first < '0' || first > '9') {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func validateTargetResult(target domain.TargetResult) error {
	if target.Priority < -2 || target.Priority > 2 {
		return usage("output_priority_invalid", "send target priority is out of range")
	}
	if target.QuotaRemaining != nil && *target.QuotaRemaining < 0 {
		return usage("output_quota_invalid", "send target quota is negative")
	}
	switch target.Outcome {
	case domain.Planned, domain.Skipped:
		if target.RequestID != "" || target.ReceiptID != "" || target.QuotaRemaining != nil {
			return usage("output_metadata_invalid", "undelivered target contains response metadata")
		}
		if target.Error != nil {
			return usage("output_error_invalid", "undelivered target contains an error")
		}
	case domain.Sent:
		if target.Error != nil {
			return usage("output_error_invalid", "sent target contains an error")
		}
	case domain.Failed, domain.Ambiguous:
		if target.Error == nil {
			return usage("output_error_missing", "failed or ambiguous target is missing an error")
		}
		if target.Error.Code == "" || !safeErrorCategory(target.Error.Category) {
			return usage("output_error_invalid", "send target error is invalid")
		}
		wantAmbiguous := target.Outcome == domain.Ambiguous
		if target.Error.Ambiguous != wantAmbiguous {
			return usage("output_ambiguity_invalid", "send target error ambiguity does not match outcome")
		}
	}
	return nil
}

func safeErrorCategory(category string) bool {
	switch category {
	case apperror.CategoryUsage, apperror.CategoryConfig, apperror.CategoryAuthentication, apperror.CategoryValidation, apperror.CategoryQuota, apperror.CategoryNetwork, apperror.CategoryConfirmation, apperror.CategoryInternal:
		return true
	default:
		return false
	}
}

func normalizeResult(result CommandResult) CommandResult {
	switch v := result.(type) {
	case *SendResult:
		if v == nil {
			return nil
		}
		return *v
	case *ProfilesResult:
		if v == nil {
			return nil
		}
		return *v
	case *RecipientsResult:
		if v == nil {
			return nil
		}
		return *v
	case *ValidationsResult:
		if v == nil {
			return nil
		}
		return *v
	case *SoundsResult:
		if v == nil {
			return nil
		}
		return *v
	case *LimitsResult:
		if v == nil {
			return nil
		}
		return *v
	case *AuthStatusResult:
		if v == nil {
			return nil
		}
		return *v
	case *BackendResult:
		if v == nil {
			return nil
		}
		return *v
	case *InfoResult:
		if v == nil {
			return nil
		}
		return *v
	case *VersionResult:
		if v == nil {
			return nil
		}
		return *v
	case *MutationResult:
		if v == nil {
			return nil
		}
		return *v
	case *CompletionResult:
		if v == nil {
			return nil
		}
		return *v
	default:
		return result
	}
}

func knownPairing(command string, result CommandResult) bool {
	switch command {
	case "profiles list", "profiles show":
		_, ok := result.(ProfilesResult)
		return ok
	case "recipients list", "recipients show":
		_, ok := result.(RecipientsResult)
		return ok
	case "recipients validate":
		_, ok := result.(ValidationsResult)
		return ok
	case "sounds":
		_, ok := result.(SoundsResult)
		return ok
	case "limits":
		_, ok := result.(LimitsResult)
		return ok
	case "auth status":
		_, ok := result.(AuthStatusResult)
		return ok
	case "auth backend":
		_, ok := result.(BackendResult)
		return ok
	case "info":
		_, ok := result.(InfoResult)
		return ok
	case "version":
		_, ok := result.(VersionResult)
		return ok
	case "profiles add", "profiles use", "profiles remove", "recipients add", "recipients remove", "auth backend set":
		_, ok := result.(MutationResult)
		return ok
	case "completion":
		_, ok := result.(CompletionResult)
		return ok
	case "setup":
		_, ok := result.(MutationResult)
		return ok
	default:
		return false
	}
}

func knownCommand(command string) bool {
	if command == "send" || command == "setup" {
		return true
	}
	switch command {
	case "profiles", "profiles list", "profiles show", "recipients", "recipients list", "recipients show", "recipients validate", "sounds", "limits", "auth", "auth status", "auth backend", "info", "version", "profiles add", "profiles use", "profiles remove", "recipients add", "recipients remove", "auth backend set", "completion":
		return true
	default:
		return false
	}
}

func sendBatchCommand(batch domain.BatchResult) string {
	if batch.Command == "" {
		return "send"
	}
	return batch.Command
}
