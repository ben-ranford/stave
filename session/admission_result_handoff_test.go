package session

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/state"
)

func TestSessionImmediateResultFollowupsReleaseAdmissionCapacity(t *testing.T) {
	const (
		chainsPerRun = 8
		runs         = 16
	)
	for run := 0; run < runs; run++ {
		testImmediateResultFollowupChain(t, run, chainsPerRun)
	}
}

func testImmediateResultFollowupChain(t *testing.T, run, chains int) {
	t.Helper()
	var delivered atomic.Int32
	s := newSession(t, Options[model]{
		QueueCapacity:    1,
		MaxActiveBatches: 1,
		EffectPorts: map[string]effect.Port{
			"immediate": effect.PortFunc(func(context.Context, effect.Call) (any, error) {
				delivered.Add(1)
				return nil, nil
			}),
		},
		Reduce: immediateResultFollowupReducer(chains),
	})
	defer s.Close()
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, s, func(snapshot state.State[model]) bool {
		return snapshot.Model.Count == chains
	})
	if got := delivered.Load(); got != int32(chains) {
		t.Fatalf("run %d delivered %d immediate effects, want %d", run, got, chains)
	}
	if s.Lifecycle() != LifecycleRunning {
		t.Fatalf("run %d lifecycle = %v, want running", run, s.Lifecycle())
	}
	for _, diagnostic := range s.Diagnostics() {
		if diagnostic.Code == "EFFECT_ADMISSION_BACKPRESSURE" {
			t.Fatalf("run %d falsely saturated admission: %#v", run, diagnostic)
		}
	}
}

func immediateResultFollowupReducer(chains int) func(context.Context, model, event.Event) (model, []effect.Request, error) {
	return func(_ context.Context, current model, ev event.Event) (model, []effect.Request, error) {
		switch ev.Kind {
		case event.Key:
			return current, []effect.Request{{Spec: effect.Spec{Kind: "immediate"}}}, nil
		case event.EffectResult:
			current.Count++
			if current.Count < chains {
				return current, []effect.Request{{Spec: effect.Spec{Kind: "immediate"}}}, nil
			}
		}
		return current, nil, nil
	}
}
