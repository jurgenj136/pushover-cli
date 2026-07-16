package delivery

import (
	"context"
	"os"
)

// interruptMonitor is a single-owner state gate. Its goroutine is the only
// code that reads Signals or changes launch state, so a queued signal is
// consumed before any pending launch command can be approved.
type interruptMonitor struct {
	commands chan any
	stop     chan struct{}
	done     chan struct{}
}

type beginLaunch struct {
	cancel context.CancelFunc
	reply  chan launchDecision
}

type finishLaunch struct{ reply chan monitorState }
type inspectMonitor struct{ reply chan monitorState }

type launchDecision struct {
	started     bool
	interrupted bool
	err         error
}

type monitorState struct {
	interrupted bool
	err         error
}

func startInterruptMonitor(signals <-chan os.Signal, callerCtx, scopeCtx context.Context, cancelConfirmation context.CancelFunc) *interruptMonitor {
	m := &interruptMonitor{commands: make(chan any), stop: make(chan struct{}), done: make(chan struct{})}
	go m.run(signals, callerCtx, scopeCtx, cancelConfirmation)
	return m
}

func (m *interruptMonitor) begin(cancel context.CancelFunc) launchDecision {
	// Approval is the linearized scheduling boundary. Signals or cancellation
	// observed before this decision prevent Client.Send; a first signal after
	// approval is, by definition, during the active attempt and does not cancel.
	reply := make(chan launchDecision, 1)
	m.commands <- beginLaunch{cancel: cancel, reply: reply}
	return <-reply
}

func (m *interruptMonitor) finish() monitorState {
	reply := make(chan monitorState, 1)
	m.commands <- finishLaunch{reply: reply}
	return <-reply
}

func (m *interruptMonitor) inspect() monitorState {
	reply := make(chan monitorState, 1)
	m.commands <- inspectMonitor{reply: reply}
	return <-reply
}

func (m *interruptMonitor) close() {
	close(m.stop)
	<-m.done
}

func (m *interruptMonitor) run(signals <-chan os.Signal, callerCtx, scopeCtx context.Context, cancelConfirmation context.CancelFunc) {
	defer close(m.done)
	callerDone := callerCtx.Done()
	scopeDone := scopeCtx.Done()
	state := monitorState{}
	var activeCancel context.CancelFunc
	signalCount := 0

	handleSignal := func() {
		signalCount++
		state.interrupted = true
		if signalCount == 1 {
			cancelConfirmation()
		}
		if signalCount >= 2 && activeCancel != nil {
			activeCancel()
		}
	}
	handleCancellation := func(err error) {
		if state.err == nil {
			state.err = err
		}
		cancelConfirmation()
		if activeCancel != nil {
			activeCancel()
		}
		callerDone = nil
		scopeDone = nil
	}
	drainControl := func() {
		for {
			progress := false
			if signals != nil {
				select {
				case _, open := <-signals:
					if !open {
						signals = nil
					} else {
						handleSignal()
					}
					progress = true
				default:
				}
			}
			if callerDone != nil {
				select {
				case <-callerDone:
					handleCancellation(callerCtx.Err())
					progress = true
				default:
				}
			}
			if scopeDone != nil {
				select {
				case <-scopeDone:
					handleCancellation(scopeCtx.Err())
					progress = true
				default:
				}
			}
			if !progress {
				return
			}
		}
	}

	for {
		drainControl()
		select {
		case _, open := <-signals:
			if !open {
				signals = nil
			} else {
				handleSignal()
			}
		case <-callerDone:
			handleCancellation(callerCtx.Err())
		case <-scopeDone:
			handleCancellation(scopeCtx.Err())
		case command := <-m.commands:
			// Signals/cancellation win when they were queued before a launch
			// request but select happened to choose the request first.
			drainControl()
			switch command := command.(type) {
			case beginLaunch:
				decision := launchDecision{interrupted: state.interrupted, err: state.err}
				if !state.interrupted && state.err == nil {
					activeCancel = command.cancel
					decision.started = true
				}
				command.reply <- decision
			case finishLaunch:
				activeCancel = nil
				command.reply <- state
			case inspectMonitor:
				command.reply <- state
			}
		case <-m.stop:
			return
		}
	}
}
