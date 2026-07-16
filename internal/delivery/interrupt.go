package delivery

import (
	"os"
)

// interruptState is used only by Runner's scheduling goroutine. It therefore
// has no shared mutable state and needs no locks. Signals are drained before
// every new target is scheduled so a first interrupt delivered between sends
// cannot permit an extra mutation.
type interruptState struct {
	signals <-chan os.Signal
	count   int
}

func newInterruptState(signals <-chan os.Signal) *interruptState {
	return &interruptState{signals: signals}
}

func (s *interruptState) interrupted() bool { return s.count > 0 }

func (s *interruptState) drain() {
	for s.signals != nil {
		select {
		case _, open := <-s.signals:
			if !open {
				s.signals = nil
				continue
			}
			s.count++
		default:
			return
		}
	}
}

// wait allows one active request to finish after the first signal. The second
// signal cancels only that request's send phase; no retry is attempted.
func (s *interruptState) wait(result <-chan attemptedSend, cancel func()) attemptedSend {
	for {
		select {
		case attempt := <-result:
			s.drain()
			return attempt
		case _, open := <-s.signals:
			if !open {
				s.signals = nil
				continue
			}
			s.count++
			if s.count >= 2 {
				cancel()
			}
		}
	}
}
