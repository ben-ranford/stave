package agent

import (
	"context"
	"sync"

	"github.com/ben-ranford/stave/protocol"
)

// snapshotSubscription owns one capacity-one full-snapshot slot for one Serve
// connection. The producer never writes the transport directly.
type snapshotSubscription struct {
	mu       sync.Mutex
	active   bool
	sequence uint64
	revision uint64
	pending  *protocol.SnapshotResult
	terminal string
	wake     chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
}

func newSnapshotSubscription(parent context.Context, baseline protocol.SnapshotResult, wake chan struct{}, wait SnapshotPublicationWaiter, snapshot func(context.Context) (protocol.SnapshotResult, bool)) *snapshotSubscription {
	ctx, cancel := context.WithCancel(parent)
	s := &snapshotSubscription{active: true, sequence: baseline.Sequence, revision: baseline.Revision, wake: wake, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer func() {
			if recover() != nil {
				s.fail("provider_failed")
			}
		}()
		for {
			if wait(ctx, s.currentSequence()) != nil {
				s.fail("session_closed")
				return
			}
			result, ok := snapshot(ctx)
			if !ok {
				s.fail("provider_failed")
				return
			}
			if !s.replace(result) {
				s.fail("provider_failed")
				return
			}
		}
	}()
	return s
}
func (s *snapshotSubscription) fail(reason string) {
	s.mu.Lock()
	if s.active {
		s.active = false
		s.pending = nil
		s.terminal = reason
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	s.cancel()
}

func (s *snapshotSubscription) currentSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sequence
}
func (s *snapshotSubscription) replace(result protocol.SnapshotResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || result.Sequence <= s.sequence || result.Revision < s.revision {
		return false
	}
	pending := result
	s.pending = &pending
	s.sequence = result.Sequence
	s.revision = result.Revision
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return true
}
func (s *snapshotSubscription) take() (protocol.SnapshotResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.pending == nil {
		return protocol.SnapshotResult{}, false
	}
	result := *s.pending
	s.pending = nil
	return result, true
}
func (s *snapshotSubscription) takeTerminal() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal == "" {
		return "", false
	}
	reason := s.terminal
	s.terminal = ""
	return reason, true
}
func (s *snapshotSubscription) close() {
	s.mu.Lock()
	s.active = false
	s.pending = nil
	s.mu.Unlock()
	s.cancel()
	<-s.done
}
