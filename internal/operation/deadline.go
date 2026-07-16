// Package operation provides one total deadline and bounded child phase contexts.
package operation

import (
	"context"
	"sync"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
)

type Phase string

const (
	PhaseConnectTLS       Phase = "connect_tls"
	PhaseResponseHeaders  Phase = "response_headers"
	PhaseRemoteAttachment Phase = "remote_attachment"
	PhaseValidation       Phase = "validation"
	PhaseSend             Phase = "send"

	DefaultTotalTimeout = 2 * time.Minute
	MinTotalTimeout     = time.Second
	MaxTotalTimeout     = 15 * time.Minute
)

var phaseBudgets = map[Phase]time.Duration{
	PhaseConnectTLS:       5 * time.Second,
	PhaseResponseHeaders:  10 * time.Second,
	PhaseRemoteAttachment: 20 * time.Second,
	PhaseValidation:       10 * time.Second,
	PhaseSend:             15 * time.Second,
}

type Clock interface{ Now() time.Time }

type ContextMaker interface {
	WithDeadline(context.Context, time.Time) (context.Context, context.CancelFunc)
}

type Factory struct {
	Clock    Clock
	Contexts ContextMaker
}

type Scope struct {
	ctx      context.Context
	deadline time.Time
	cancel   context.CancelFunc
	clock    Clock
	contexts ContextMaker
	once     *sync.Once
}

func (f Factory) Start(parent context.Context, totalOverride *time.Duration) (Scope, error) {
	if parent == nil {
		parent = context.Background()
	}
	total := DefaultTotalTimeout
	if totalOverride != nil {
		total = *totalOverride
		if total < MinTotalTimeout || total > MaxTotalTimeout {
			return Scope{}, apperror.New("invalid_timeout", apperror.CategoryUsage, "timeout must be between 1 second and 15 minutes")
		}
	}
	clock := f.Clock
	if clock == nil {
		clock = wallClock{}
	}
	contexts := f.Contexts
	if contexts == nil {
		contexts = standardContexts{}
	}
	deadline := clock.Now().Add(total)
	ctx, cancel := contexts.WithDeadline(parent, deadline)
	return Scope{ctx: ctx, deadline: deadline, cancel: cancel, clock: clock, contexts: contexts, once: &sync.Once{}}, nil
}

func (s Scope) Context() context.Context { return s.ctx }

func (s Scope) Phase(phase Phase) (context.Context, context.CancelFunc, error) {
	budget, ok := phaseBudgets[phase]
	if !ok {
		return nil, nil, apperror.New("unknown_phase", apperror.CategoryInternal, "unknown operation phase")
	}
	now := s.clock.Now()
	deadline := now.Add(budget)
	if deadline.After(s.deadline) {
		deadline = s.deadline
	}
	ctx, cancel := s.contexts.WithDeadline(s.ctx, deadline)
	return ctx, cancel, nil
}

func (s Scope) Close() {
	if s.once != nil && s.cancel != nil {
		s.once.Do(s.cancel)
	}
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

type standardContexts struct{}

func (standardContexts) WithDeadline(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, deadline)
}
