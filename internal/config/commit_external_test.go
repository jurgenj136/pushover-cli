package config_test

import (
	"errors"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/config"
)

func TestNewCommitErrorSupportsCrossPackageFakes(t *testing.T) {
	err := config.NewCommitError(config.AppliedDurabilityUnknown, errors.New("private fake cause"))
	if got := config.OutcomeOf(err); got != config.AppliedDurabilityUnknown {
		t.Fatalf("outcome = %s", got)
	}
	if got := config.NewCommitError(config.CommitOutcome("invalid"), nil).Outcome; got != config.Unchanged {
		t.Fatalf("invalid outcome = %s", got)
	}
}
