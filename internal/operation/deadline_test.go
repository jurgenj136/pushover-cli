package operation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mutableClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type recordedContextMaker struct {
	deadlines []time.Time
	cancels   atomic.Int32
}

func (m *recordedContextMaker) WithDeadline(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	m.deadlines = append(m.deadlines, deadline)
	ctx, cancel := context.WithDeadline(parent, deadline)
	return ctx, func() { m.cancels.Add(1); cancel() }
}

func TestStartAndPhaseBudgets(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	maker := &recordedContextMaker{}
	scope, err := (Factory{Clock: fixedClock{now}, Contexts: maker}).Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := maker.deadlines[0], now.Add(DefaultTotalTimeout); !got.Equal(want) {
		t.Fatalf("total deadline = %s, want %s", got, want)
	}
	for phase, allowance := range map[Phase]time.Duration{
		PhaseConnectTLS:       5 * time.Second,
		PhaseResponseHeaders:  10 * time.Second,
		PhaseRemoteAttachment: 20 * time.Second,
		PhaseValidation:       10 * time.Second,
		PhaseSend:             15 * time.Second,
	} {
		_, cancel, err := scope.Phase(phase)
		if err != nil {
			t.Fatalf("Phase(%s): %v", phase, err)
		}
		cancel()
		got := maker.deadlines[len(maker.deadlines)-1]
		if want := now.Add(allowance); !got.Equal(want) {
			t.Fatalf("Phase(%s) deadline = %s, want %s", phase, got, want)
		}
	}
	scope.Close()
	scope.Close()
	if got := maker.cancels.Load(); got != 6 { // five phase contexts plus total context once
		t.Fatalf("cancels = %d, want 6", got)
	}
}

func TestStartOverrideAndBounds(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	for _, override := range []*time.Duration{durationPtr(time.Second - time.Nanosecond), durationPtr(15*time.Minute + time.Nanosecond)} {
		_, err := (Factory{Clock: fixedClock{now}}).Start(context.Background(), override)
		if err == nil {
			t.Fatal("invalid override should fail")
		}
	}
	for _, override := range []time.Duration{time.Second, 15 * time.Minute} {
		maker := &recordedContextMaker{}
		scope, err := (Factory{Clock: fixedClock{now}, Contexts: maker}).Start(context.Background(), &override)
		if err != nil {
			t.Fatalf("inclusive override %s rejected: %v", override, err)
		}
		if got, want := maker.deadlines[0], now.Add(override); !got.Equal(want) {
			t.Fatalf("deadline = %s, want %s", got, want)
		}
		scope.Close()
	}
	maker := &recordedContextMaker{}
	override := 3 * time.Second
	scope, err := (Factory{Clock: fixedClock{now}, Contexts: maker}).Start(context.Background(), &override)
	if err != nil {
		t.Fatal(err)
	}
	_, cancel, err := scope.Phase(PhaseSend)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if got, want := maker.deadlines[1], now.Add(override); !got.Equal(want) {
		t.Fatalf("phase deadline = %s, want remaining total %s", got, want)
	}
	if _, _, err := scope.Phase("nope"); err == nil {
		t.Fatal("unknown phase should fail")
	}
}

func TestExpiredTotalCapsPhaseAtTotalDeadline(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: now}
	maker := &recordedContextMaker{}
	override := time.Second
	scope, err := (Factory{Clock: clock, Contexts: maker}).Start(context.Background(), &override)
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(now.Add(2 * time.Second))
	_, cancel, err := scope.Phase(PhaseSend)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if got, want := maker.deadlines[1], now.Add(override); !got.Equal(want) {
		t.Fatalf("expired phase deadline = %s, want total %s", got, want)
	}
}

func TestScopeCloseIsZeroValueSafeAndConcurrent(t *testing.T) {
	var zero Scope
	zero.Close()

	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	maker := &recordedContextMaker{}
	scope, err := (Factory{Clock: fixedClock{now}, Contexts: maker}).Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			scope.Close()
		}()
	}
	wait.Wait()
	if got := maker.cancels.Load(); got != 1 {
		t.Fatalf("total cancels = %d, want 1", got)
	}
}

func durationPtr(v time.Duration) *time.Duration { return &v }
