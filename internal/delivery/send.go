// Package delivery owns the irreversible half of a Pushover send: required
// emergency confirmation and sequential, non-retrying fan-out of a sealed
// preflight plan.
package delivery

import (
	"context"
	"errors"
	"os"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
)

// Confirmer performs the human-facing emergency confirmation. It owns terminal
// details; Runner only decides whether that confirmation is required.
type Confirmer interface {
	Confirm(context.Context, preflight.Confirmation) error
}

// RunOptions affect delivery only after a successful real preflight. In
// particular, AssumeYes cannot manufacture a runnable plan.
type RunOptions struct {
	AssumeYes bool
}

// Runner performs exactly one sequential Send attempt for each scheduled
// finalized target. It never resolves config, reads attachments, validates
// recipients, or encrypts payloads.
type Runner struct {
	Client    pushover.Client
	Confirmer Confirmer
	Signals   <-chan os.Signal
	Scope     operation.Scope

	// beforeSchedule is a deterministic test barrier immediately before the
	// serialized launch gate. Production leaves it nil.
	beforeSchedule func()
}

// Run delivers a sealed plan in input order. Per-target delivery failures are
// recorded in the returned batch; only a malformed invocation or a declined
// required confirmation returns a non-nil error.
func (r *Runner) Run(ctx context.Context, plan preflight.FinalizedSendPlan, options RunOptions) (domain.BatchResult, error) {
	if r == nil || r.Scope.Context() == nil {
		return domain.BatchResult{}, apperror.New("delivery_scope", apperror.CategoryInternal, "operation context is not initialized")
	}
	if !plan.IsValid() {
		return domain.BatchResult{}, apperror.New("send_plan_invalid", apperror.CategoryInternal, "send plan is not finalized")
	}
	if r.Client == nil {
		return domain.BatchResult{}, apperror.New("delivery_client", apperror.CategoryInternal, "Pushover client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	targets := plan.Targets()
	batch := domain.BatchResult{Command: "send", Results: make([]domain.TargetResult, 0, len(targets))}
	if err := ctx.Err(); err != nil {
		batch.Results = append(batch.Results, failedBeforeSend(targets[0], err))
		appendSkipped(&batch, targets[1:])
		return batch, nil
	}

	// Scope remains the sole total deadline. runCtx is only a cancellation
	// child, allowing the caller to narrow that scope without replacing it.
	runCtx, cancelRun := context.WithCancel(r.Scope.Context())
	stopCaller := context.AfterFunc(ctx, cancelRun)
	defer func() { stopCaller(); cancelRun() }()
	confirmationCtx, cancelConfirmation := context.WithCancel(runCtx)
	monitor := startInterruptMonitor(r.Signals, ctx, r.Scope.Context(), cancelConfirmation)
	defer func() { cancelConfirmation(); monitor.close() }()
	confirmation := plan.Confirmation()
	if confirmation.Priority == domain.PriorityEmergency && !options.AssumeYes {
		if r.Confirmer == nil {
			return domain.BatchResult{}, apperror.New("emergency_confirmation_required", apperror.CategoryConfirmation, "emergency send requires confirmation")
		}
		// This is deliberately the final action before mutation scheduling.
		if err := r.Confirmer.Confirm(confirmationCtx, confirmation); err != nil {
			state := monitor.inspect()
			if state.interrupted {
				batch.Interrupted = true
				appendSkipped(&batch, targets)
				return batch, nil
			}
			if state.err != nil {
				batch.Results = append(batch.Results, failedBeforeSend(targets[0], state.err))
				appendSkipped(&batch, targets[1:])
				return batch, nil
			}
			return domain.BatchResult{}, err
		}
	}
	if state := monitor.inspect(); state.interrupted {
		batch.Interrupted = true
		appendSkipped(&batch, targets)
		return batch, nil
	} else if state.err != nil {
		batch.Results = append(batch.Results, failedBeforeSend(targets[0], state.err))
		appendSkipped(&batch, targets[1:])
		return batch, nil
	}

	for index, target := range targets {
		phase, phaseCancel, err := r.Scope.Phase(operation.PhaseSend)
		if err != nil {
			batch.Results = append(batch.Results, failedBeforeSend(target, err))
			appendSkipped(&batch, targets[index+1:])
			break
		}
		phase, cancel := cancellationChild(phase, runCtx, phaseCancel)
		if r.beforeSchedule != nil {
			r.beforeSchedule()
		}
		decision := monitor.begin(cancel)
		if !decision.started {
			cancel()
			if decision.interrupted {
				batch.Interrupted = true
				appendSkipped(&batch, targets[index:])
			} else {
				batch.Results = append(batch.Results, failedBeforeSend(target, decision.err))
				appendSkipped(&batch, targets[index+1:])
			}
			break
		}
		result := make(chan attemptedSend, 1)
		go func(payload domain.TargetPayload) {
			response, sendErr := r.Client.Send(phase, payload)
			result <- attemptedSend{response: response, err: sendErr}
		}(target)

		attempt := <-result
		state := monitor.finish()
		cancel()
		batch.Results = append(batch.Results, resultFor(target, attempt))
		if state.interrupted {
			batch.Interrupted = true
		}
	}

	return batch, nil
}

func cancellationChild(phase, runCtx context.Context, phaseCancel context.CancelFunc) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(phase)
	stopRun := context.AfterFunc(runCtx, cancel)
	return ctx, func() {
		stopRun()
		cancel()
		phaseCancel()
	}
}

type attemptedSend struct {
	response pushover.SendResponse
	err      error
}

func resultFor(target domain.TargetPayload, attempt attemptedSend) domain.TargetResult {
	result := domain.TargetResult{
		Alias:          target.Target.Alias,
		Priority:       target.Message.Priority,
		RequestID:      attempt.response.RequestID,
		ReceiptID:      attempt.response.ReceiptID,
		QuotaRemaining: copyQuota(attempt.response.QuotaRemaining),
	}
	if attempt.err == nil {
		result.Outcome = domain.Sent
		return result
	}
	if transmissionPossible(attempt.err) {
		result.Outcome = domain.Ambiguous
		result.Error = deliveryDetail(attempt.err, true)
		return result
	}
	result.Outcome = domain.Failed
	result.Error = deliveryDetail(attempt.err, false)
	return result
}

func failedBeforeSend(target domain.TargetPayload, cause error) domain.TargetResult {
	return domain.TargetResult{
		Alias:    target.Target.Alias,
		Priority: target.Message.Priority,
		Outcome:  domain.Failed,
		Error: &domain.ErrorDetail{
			Code:     "operation_timeout",
			Category: apperror.CategoryNetwork,
			Message:  "operation deadline exceeded",
		},
	}
}

func appendSkipped(batch *domain.BatchResult, targets []domain.TargetPayload) {
	for _, target := range targets {
		batch.Results = append(batch.Results, domain.TargetResult{Alias: target.Target.Alias, Priority: target.Message.Priority, Outcome: domain.Skipped})
	}
}

func copyQuota(value *int) *int {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

// transmissionPossible is intentionally based only on the typed client error.
// Unknown errors remain ambiguous because guessing from an error string can
// cause accidental duplicate notifications.
func transmissionPossible(err error) bool {
	var typed *pushover.SendError
	if !errors.As(err, &typed) || typed == nil {
		return true
	}
	return typed.TransmissionPossible
}

func deliveryDetail(err error, ambiguous bool) *domain.ErrorDetail {
	detail := apperror.SafeDetail(err)
	if detail.Category == apperror.CategoryInternal {
		detail = &domain.ErrorDetail{Code: "network_failure", Category: apperror.CategoryNetwork, Message: "Pushover request failed"}
	}
	detail.Ambiguous = ambiguous
	return detail
}
