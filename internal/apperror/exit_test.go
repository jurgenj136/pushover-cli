package apperror

import (
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestExitCommandErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{"internal", New(CodeInternal, CategoryInternal, "x"), ExitInternal},
		{"usage", New("bad_flag", CategoryUsage, "x"), ExitUsage},
		{"config", New("bad_config", CategoryConfig, "x"), ExitConfiguration},
		{"validation", New("bad_input", CategoryValidation, "x"), ExitValidation},
		{"quota", New("quota", CategoryQuota, "x"), ExitQuota},
		{"network", New("network", CategoryNetwork, "x"), ExitNetwork},
		{"confirmation", New("confirmation", CategoryConfirmation, "x"), ExitConfirmation},
		{"ambiguous", New("ambiguous", CategoryNetwork, "x", WithAmbiguous(true)), ExitAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Exit(test.err, nil); got != test.want {
				t.Fatalf("Exit = %d, want %d", got, test.want)
			}
		})
	}
}

func TestExitBatchOutcomes(t *testing.T) {
	quota := &domain.ErrorDetail{Category: CategoryQuota}
	network := &domain.ErrorDetail{Category: CategoryNetwork}
	for _, test := range []struct {
		name  string
		batch *domain.BatchResult
		want  int
	}{
		{"success", &domain.BatchResult{Results: []domain.TargetResult{{Outcome: domain.Sent}}}, ExitSuccess},
		{"quota", &domain.BatchResult{Results: []domain.TargetResult{{Outcome: domain.Failed, Error: quota}}}, ExitQuota},
		{"network", &domain.BatchResult{Results: []domain.TargetResult{{Outcome: domain.Failed, Error: network}}}, ExitNetwork},
		{"ambiguous", &domain.BatchResult{Results: []domain.TargetResult{{Outcome: domain.Ambiguous}}}, ExitAmbiguous},
		{"mixed", &domain.BatchResult{Results: []domain.TargetResult{{Outcome: domain.Sent}, {Outcome: domain.Failed, Error: network}}}, ExitPartial},
		{"interrupted no delivery", &domain.BatchResult{Interrupted: true, Results: []domain.TargetResult{{Outcome: domain.Skipped}}}, ExitInterrupted},
		{"interrupted ambiguity", &domain.BatchResult{Interrupted: true, Results: []domain.TargetResult{{Outcome: domain.Ambiguous}}}, ExitAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Exit(nil, test.batch); got != test.want {
				t.Fatalf("Exit = %d, want %d", got, test.want)
			}
		})
	}
}
