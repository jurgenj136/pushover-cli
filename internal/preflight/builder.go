// Package preflight resolves and validates complete sends before side effects.
package preflight

import (
	"context"
	"net/url"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/attachment"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/e2ee"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/resolver"
)

// Builder owns every fallible operation before mutation is allowed.
type Builder struct {
	Resolver    *resolver.Resolver
	Attachments attachment.Loader
	Validator   pushover.Client
	Encryptor   e2ee.Service
	Scope       operation.Scope

	resolve func(context.Context, resolver.Input) (domain.ResolvedSendDraft, error)
}

func (b *Builder) Build(ctx context.Context, request Request) (FinalizedSendPlan, *domain.BatchResult, error) {
	if b == nil || b.Scope.Context() == nil {
		return FinalizedSendPlan{}, nil, apperror.New("preflight_scope", apperror.CategoryInternal, "operation context is not initialized")
	}
	if ctx != nil && ctx.Err() != nil {
		return FinalizedSendPlan{}, nil, ctx.Err()
	}
	if request.Attachment.LocalPath != "" && request.Attachment.RemoteURL != nil {
		return FinalizedSendPlan{}, nil, apperror.New("attachment_conflict", apperror.CategoryUsage, "local and remote attachments cannot be combined")
	}
	if request.Resolve.Message.Attachment != nil {
		return FinalizedSendPlan{}, nil, apperror.New("attachment_input_required", apperror.CategoryUsage, "attachments must be supplied through the attachment input")
	}
	var remoteURL *url.URL
	if source := request.Attachment.RemoteURL; source != nil {
		cloned := *source
		remoteURL = &cloned
		if !validRemoteURL(remoteURL) {
			return FinalizedSendPlan{}, nil, apperror.New("attachment_url", apperror.CategoryValidation, "remote attachment URL must be an absolute HTTPS URL without credentials")
		}
	}
	if b.Resolver == nil {
		return FinalizedSendPlan{}, nil, apperror.New("resolver_unavailable", apperror.CategoryInternal, "resolver is not configured")
	}
	workCtx, stop := callerContext(b.Scope.Context(), ctx)
	defer stop()
	resolve := b.Resolver.Resolve
	if b.resolve != nil {
		resolve = b.resolve
	}
	draft, err := resolve(workCtx, request.Resolve)
	if err != nil {
		return FinalizedSendPlan{}, nil, err
	}
	defer wipePayloadKeys(draft.Targets)
	targets := make([]domain.TargetPayload, len(draft.Targets))
	for i := range draft.Targets {
		targets[i] = clonePayload(draft.Targets[i])
	}
	if request.Attachment.LocalPath != "" || remoteURL != nil {
		if request.DryRun && remoteURL != nil && !request.ValidateNetwork {
			// The fetch stays offline, but the placeholder preserves attachment
			// presence for local compatibility checks (notably E2EE conflicts).
			for i := range targets {
				targets[i].Message.Attachment = &domain.Attachment{}
			}
		} else {
			if b.Attachments == nil {
				return FinalizedSendPlan{}, nil, apperror.New("attachment_unavailable", apperror.CategoryInternal, "attachment loader is not configured")
			}
			var snapshot *domain.Attachment
			if request.Attachment.LocalPath != "" {
				snapshot, err = b.Attachments.Local(workCtx, request.Attachment.LocalPath)
			} else {
				snapshot, err = b.Attachments.Remote(workCtx, remoteURL)
			}
			if err != nil {
				wipePayloadKeys(targets)
				return FinalizedSendPlan{}, nil, err
			}
			if snapshot == nil {
				wipePayloadKeys(targets)
				return FinalizedSendPlan{}, nil, apperror.New("attachment_unavailable", apperror.CategoryValidation, "attachment could not be loaded")
			}
			for i := range targets {
				targets[i].Message.Attachment = cloneAttachment(snapshot)
			}
		}
	}
	if len(targets) == 0 {
		return FinalizedSendPlan{}, nil, apperror.New("recipient_required", apperror.CategoryUsage, "at least one recipient alias is required")
	}
	if err := validateBatch(workCtx, targets); err != nil {
		wipePayloadKeys(targets)
		return FinalizedSendPlan{}, nil, err
	}
	if request.Validate && !request.DryRun {
		if b.Validator == nil {
			wipePayloadKeys(targets)
			return FinalizedSendPlan{}, nil, apperror.New("validator_unavailable", apperror.CategoryInternal, "recipient validator is not configured")
		}
		for i := range targets {
			phase, cancel, phaseErr := b.Scope.Phase(operation.PhaseValidation)
			if phaseErr != nil {
				wipePayloadKeys(targets)
				return FinalizedSendPlan{}, nil, phaseErr
			}
			phase, stopPhase := combineContext(phase, workCtx)
			validated, validateErr := b.Validator.ValidateRecipient(phase, targets[i].Target.Credentials.Token, targets[i].Target.Credentials.UserKey, append([]string(nil), targets[i].Message.Devices...))
			stopPhase()
			cancel()
			if validateErr != nil {
				wipePayloadKeys(targets)
				return FinalizedSendPlan{}, nil, validateErr
			}
			if !validated.Valid {
				wipePayloadKeys(targets)
				return FinalizedSendPlan{}, nil, apperror.New("recipient_invalid", apperror.CategoryValidation, "recipient validation failed")
			}
			targets[i].Target.Validated = true
		}
	}
	for i := range targets {
		if !targets[i].Message.Encrypted {
			continue
		}
		if b.Encryptor == nil {
			wipePayloadKeys(targets)
			return FinalizedSendPlan{}, nil, apperror.New("encryptor_unavailable", apperror.CategoryInternal, "encryptor is not configured")
		}
		transformed, encryptErr := b.Encryptor.EncryptPayload(workCtx, targets[i])
		if encryptErr != nil {
			wipePayloadKeys(targets)
			return FinalizedSendPlan{}, nil, encryptErr
		}
		wipe(targets[i].Target.E2EEKey)
		targets[i] = transformed
		wipe(targets[i].Target.E2EEKey)
		targets[i].Target.E2EEKey = nil
	}
	for _, target := range targets {
		if err := validateWirePayload(target); err != nil {
			wipePayloadKeys(targets)
			return FinalizedSendPlan{}, nil, err
		}
	}
	confirmation := makeConfirmation(targets)
	if request.DryRun {
		result := &domain.BatchResult{Command: "send", DryRun: true, Results: make([]domain.TargetResult, len(targets))}
		for i, target := range targets {
			result.Results[i] = domain.TargetResult{Alias: target.Target.Alias, Outcome: domain.Planned, Priority: target.Message.Priority}
		}
		wipePayloadKeys(targets)
		return FinalizedSendPlan{}, result, nil
	}
	sealedTargets := make([]domain.TargetPayload, len(targets))
	for i := range targets {
		sealedTargets[i] = clonePayload(targets[i])
	}
	wipePayloadKeys(targets)
	return FinalizedSendPlan{targets: sealedTargets, confirmation: confirmation, sealed: finalPlanSeal}, nil, nil
}

func validRemoteURL(source *url.URL) bool {
	if source == nil || !source.IsAbs() || source.Opaque != "" || !strings.EqualFold(source.Scheme, "https") || source.User != nil || source.Host == "" || strings.ContainsAny(source.Host, "\r\n") {
		return false
	}
	host := source.Hostname()
	if host == "" || strings.ContainsAny(host, "\r\n%") {
		return false
	}
	return source.Port() != "" || !strings.HasSuffix(source.Host, ":")
}

func validateBatch(ctx context.Context, targets []domain.TargetPayload) error {
	for _, target := range targets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		err := domain.ValidateTargetPayload(target)
		if err != nil {
			return err
		}
	}
	return nil
}

func combineContext(phase, parent context.Context) (context.Context, func()) {
	if parent == nil {
		return phase, func() {}
	}
	ctx, cancel := context.WithCancel(phase)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() { stop(); cancel() }
}

func makeConfirmation(targets []domain.TargetPayload) Confirmation {
	result := Confirmation{Aliases: make([]string, len(targets))}
	if len(targets) == 0 {
		return result
	}
	result.Priority = targets[0].Message.Priority
	result.Retry = targets[0].Message.Retry
	result.Expire = targets[0].Message.Expire
	result.EffectiveRetries = domain.EffectiveRetryCount(result.Retry, result.Expire)
	for i, target := range targets {
		result.Aliases[i] = target.Target.Alias
		result.Encrypted = result.Encrypted || target.Message.Encrypted
	}
	return result
}

func callerContext(scopeCtx, caller context.Context) (context.Context, func()) {
	if caller == nil {
		return scopeCtx, func() {}
	}
	ctx, cancel := context.WithCancel(scopeCtx)
	stop := context.AfterFunc(caller, cancel)
	return ctx, func() { stop(); cancel() }
}

func cloneAttachment(input *domain.Attachment) *domain.Attachment {
	if input == nil {
		return nil
	}
	result := *input
	result.Bytes = append([]byte(nil), input.Bytes...)
	return &result
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func wipePayloadKeys(targets []domain.TargetPayload) {
	for i := range targets {
		wipe(targets[i].Target.E2EEKey)
	}
}
