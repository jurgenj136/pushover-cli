package apperror

import (
	"errors"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const (
	ExitSuccess       = 0
	ExitInternal      = 1
	ExitUsage         = 2
	ExitConfiguration = 3
	ExitValidation    = 4
	ExitQuota         = 5
	ExitNetwork       = 6
	ExitPartial       = 7
	ExitConfirmation  = 8
	ExitAmbiguous     = 9
	ExitInterrupted   = 130
)

// Exit maps the command error and completed batch to the documented coarse
// process status. A batch wins because it preserves the actual delivery state.
func Exit(commandError error, batch *domain.BatchResult) int {
	if batch != nil && (len(batch.Results) > 0 || batch.Interrupted) {
		return exitBatch(batch)
	}
	if commandError == nil {
		return ExitSuccess
	}
	detail := SafeDetail(commandError)
	if detail.Ambiguous {
		return ExitAmbiguous
	}
	return exitCategory(detail.Category)
}

func exitBatch(batch *domain.BatchResult) int {
	sent, ambiguous, failed, skipped := 0, 0, 0, 0
	category := ""
	for _, result := range batch.Results {
		switch result.Outcome {
		case domain.Sent:
			sent++
		case domain.Ambiguous:
			ambiguous++
		case domain.Failed:
			failed++
			if result.Error != nil {
				if category == "" {
					category = result.Error.Category
				} else if category != result.Error.Category {
					category = "mixed"
				}
			}
		case domain.Skipped, domain.Planned:
			skipped++
		}
	}
	if sent > 0 && (ambiguous > 0 || failed > 0 || skipped > 0 || batch.Interrupted) {
		return ExitPartial
	}
	if ambiguous > 0 {
		return ExitAmbiguous
	}
	if batch.Interrupted && sent == 0 {
		return ExitInterrupted
	}
	if failed > 0 && skipped == 0 {
		return exitCategory(category)
	}
	if failed > 0 || skipped > 0 {
		return ExitPartial
	}
	return ExitSuccess
}

func exitCategory(category string) int {
	switch category {
	case CategoryUsage:
		return ExitUsage
	case CategoryConfig:
		return ExitConfiguration
	case CategoryValidation:
		return ExitValidation
	case CategoryQuota:
		return ExitQuota
	case CategoryNetwork:
		return ExitNetwork
	case CategoryConfirmation:
		return ExitConfirmation
	default:
		return ExitInternal
	}
}

// Is reports whether err's stable category is category without exposing a
// wrapped cause to renderers.
func Is(err error, category string) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Category == category
}
