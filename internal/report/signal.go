package report

import (
	"context"
	"os"
	"os/signal"
	"sync"
)

// Signal coordinates graceful (1st) vs forced (2nd) shutdown.
//
// Use NewSignal + Notify in main; the worker pool watches Ctx() and stops
// dispatching new jobs when it cancels. After the first signal, callers can
// still detect a second by monitoring HardExit().
type Signal struct {
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc
	hard   chan struct{}

	mu    sync.Mutex
	count int
	relay chan os.Signal
}

// NewSignal builds a Signal whose Ctx() cancels on the first delivery.
func NewSignal(parent context.Context) *Signal {
	ctx, cancel := context.WithCancel(parent)
	return &Signal{
		parent: parent,
		ctx:    ctx,
		cancel: cancel,
		hard:   make(chan struct{}),
		relay:  make(chan os.Signal, 4),
	}
}

// Notify subscribes to the given signals and starts a goroutine that calls
// Trigger for each delivery.
func (s *Signal) Notify(sigs ...os.Signal) {
	signal.Notify(s.relay, sigs...)
	go func() {
		for sig := range s.relay {
			if s.Trigger(sig) {
				return
			}
		}
	}()
}

// Trigger advances the state machine. Returns true when the call represents
// the second (hard) signal — caller should os.Exit(130) immediately.
func (s *Signal) Trigger(_ os.Signal) bool {
	s.mu.Lock()
	s.count++
	n := s.count
	s.mu.Unlock()
	switch n {
	case 1:
		s.cancel()
		return false
	default:
		select {
		case <-s.hard:
		default:
			close(s.hard)
		}
		return true
	}
}

// Ctx returns a context cancelled on the first signal.
func (s *Signal) Ctx() context.Context { return s.ctx }

// HardExit returns a channel closed on the second signal.
func (s *Signal) HardExit() <-chan struct{} { return s.hard }
