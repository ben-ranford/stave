package session

import (
	"context"
	"testing"
	"time"

	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/state"
)

func TestSessionCoalescesEffectAdmissionSaturationDiagnostics(t *testing.T) {
	s := newSession(t, effectAdmissionFailureOptions("none", make(chan struct{})))
	defer s.Close()
	producer := mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})
	for episode := 1; episode <= 2; episode++ {
		if got := s.reserveEffectBatch(producer, 1); got != effectAdmissionReserved {
			t.Fatalf("reserve = %v", got)
		}
		for range 1000 {
			if got := s.reserveEffectBatch(producer, 1); got != effectAdmissionRejected {
				t.Fatalf("saturated admission = %v", got)
			}
		}
		if got := len(s.Diagnostics()); got != episode {
			t.Fatalf("diagnostics = %d, want one per episode (%d)", got, episode)
		}
		s.effectAdmissions.release()
	}
}

func TestSessionClosedExecutorDoesNotReportDeliveryFailure(t *testing.T) {
	s := newSession(t, effectAdmissionFailureOptions("none", make(chan struct{})))
	defer s.Close()
	// Model the shutdown interval after executor closure, before session cancel.
	s.effects.Close()
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, s, func(snapshot state.State[model]) bool { return snapshot.Sequence == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Wait(ctx, func(state.State[model]) bool {
		s.effectAdmissions.mu.Lock()
		defer s.effectAdmissions.mu.Unlock()
		return s.effectAdmissions.reserved == 0
	}); err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range s.Diagnostics() {
		if diagnostic.Code == "EFFECT_DELIVER_FAILED" {
			t.Fatal("normal executor closure reported a delivery failure")
		}
	}
}
