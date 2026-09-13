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
	pending  *protocol.SnapshotResult
	wake     chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
}

func newSnapshotSubscription(parent context.Context, baseline uint64, wake chan struct{}, wait SnapshotPublicationWaiter, snapshot func(context.Context) (protocol.SnapshotResult, bool)) *snapshotSubscription {
	ctx, cancel := context.WithCancel(parent)
	s := &snapshotSubscription{active: true, sequence: baseline, wake: wake, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		for {
			if wait(ctx, s.currentSequence()) != nil {
				return
			}
			result, ok := snapshot(ctx)
			if !ok || !s.replace(result) {
				return
			}
		}
	}()
	return s
}

func (s *snapshotSubscription) currentSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sequence
}
func (s *snapshotSubscription) replace(result protocol.SnapshotResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || result.Sequence <= s.sequence {
		return s.active
	}
	copy := result
	s.pending = &copy
	s.sequence = result.Sequence
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
func (s *snapshotSubscription) close() {
	s.mu.Lock()
	active := s.active
	s.active = false
	s.pending = nil
	s.mu.Unlock()
	if active {
		s.cancel()
		<-s.done
	}
}
