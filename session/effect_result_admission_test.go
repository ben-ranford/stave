package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/replay"
	"github.com/ben-ranford/stave/state"
)

func TestSessionPreservesCompletedEffectResultWhenFollowupAdmissionIsSaturated(t *testing.T) {
	fixture := newSaturatedEffectResultFixture(t)
	defer fixture.session.Close()

	fixture.startInitialBatch(t)
	fixture.queuePendingBatch(t)
	close(fixture.releaseComplete)

	fixture.assertCompletedResultWasPublished(t)
	fixture.assertUnadmittedWorkWasCancelled(t)
	fixture.assertTranscriptReplays(t)
}

type saturatedEffectResultFixture struct {
	session         *Session[model]
	releaseComplete chan struct{}
	initialStarted  chan struct{}
	holdCancelled   chan struct{}
	pendingStarted  chan struct{}
	followupStarted chan struct{}
	beforeResult    state.Hashes
}

func newSaturatedEffectResultFixture(t *testing.T) *saturatedEffectResultFixture {
	t.Helper()
	fixture := &saturatedEffectResultFixture{
		releaseComplete: make(chan struct{}),
		initialStarted:  make(chan struct{}, 2),
		holdCancelled:   make(chan struct{}),
		pendingStarted:  make(chan struct{}),
		followupStarted: make(chan struct{}),
	}
	fixture.session = newSession(t, Options[model]{
		QueueCapacity:     1,
		MaxActiveBatches:  1,
		EffectParallelism: 2,
		EffectPorts: map[string]effect.Port{
			"complete": effect.PortFunc(func(ctx context.Context, _ effect.Call) (any, error) {
				fixture.initialStarted <- struct{}{}
				select {
				case <-fixture.releaseComplete:
					return nil, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}),
			"hold": effect.PortFunc(func(ctx context.Context, _ effect.Call) (any, error) {
				fixture.initialStarted <- struct{}{}
				<-ctx.Done()
				close(fixture.holdCancelled)
				return nil, ctx.Err()
			}),
			"pending": effect.PortFunc(func(context.Context, effect.Call) (any, error) {
				close(fixture.pendingStarted)
				return nil, nil
			}),
			"followup": effect.PortFunc(func(context.Context, effect.Call) (any, error) {
				close(fixture.followupStarted)
				return nil, nil
			}),
		},
		Reduce: saturatedEffectResultReducer,
		View:   testView,
	})
	return fixture
}

func saturatedEffectResultReducer(_ context.Context, current model, ev event.Event) (model, []effect.Request, error) {
	switch ev.Kind {
	case event.Key:
		current.Count++
		if current.Count == 1 {
			return current, []effect.Request{{Spec: effect.Spec{Kind: "complete"}}, {Spec: effect.Spec{Kind: "hold"}}}, nil
		}
		return current, []effect.Request{{Spec: effect.Spec{Kind: "pending"}}}, nil
	case event.EffectResult:
		ordinal := ev.Payload.(event.EffectResultPayload).Ordinal
		current.Count += 10
		current.EffectOrdinals = append(current.EffectOrdinals, int(ordinal))
		return current, []effect.Request{{Spec: effect.Spec{Kind: "followup"}}}, nil
	default:
		return current, nil, nil
	}
}

func (fixture *saturatedEffectResultFixture) startInitialBatch(t *testing.T) {
	t.Helper()
	if err := fixture.session.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-fixture.initialStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("initial effects did not start")
		}
	}
}

func (fixture *saturatedEffectResultFixture) queuePendingBatch(t *testing.T) {
	t.Helper()
	if err := fixture.session.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "tab"})); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, fixture.session, func(snapshot state.State[model]) bool {
		return snapshot.Sequence == 2 && snapshot.Model.Count == 2
	})
	snapshot, err := fixture.session.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	fixture.beforeResult = snapshot.Hashes
}

func (fixture *saturatedEffectResultFixture) assertCompletedResultWasPublished(t *testing.T) {
	t.Helper()
	waitSnapshot(t, fixture.session, func(snapshot state.State[model]) bool {
		return snapshot.Sequence == 3 && snapshot.Model.Count == 12 && fmt.Sprint(snapshot.Model.EffectOrdinals) == "[0]"
	})
	select {
	case <-fixture.session.loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("saturated result admission did not close the session")
	}
	snapshot, err := fixture.session.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Hashes.EffectLedger == fixture.beforeResult.EffectLedger || snapshot.Hashes.Declarations == fixture.beforeResult.Declarations {
		t.Fatalf("completed result did not persist effect hashes: %#v", snapshot.Hashes)
	}
	assertDiagnostic(t, fixture.session.Diagnostics(), "EFFECT_ADMISSION_BACKPRESSURE")
}

func (fixture *saturatedEffectResultFixture) assertUnadmittedWorkWasCancelled(t *testing.T) {
	t.Helper()
	select {
	case <-fixture.holdCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("session close did not cancel held sibling effect")
	}
	assertNeverStarted(t, fixture.pendingStarted, "pending batch")
	assertNeverStarted(t, fixture.followupStarted, "result followup")
}

func assertNeverStarted(t *testing.T, started <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-started:
		t.Fatalf("%s started after overload close", name)
	default:
	}
}

func (fixture *saturatedEffectResultFixture) assertTranscriptReplays(t *testing.T) {
	t.Helper()
	transcript, err := fixture.session.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if len(transcript.Records) != 3 {
		t.Fatalf("records = %d, want key, key, completed result", len(transcript.Records))
	}
	recorded := transcript.Records[2]
	if recorded.Event.Kind != event.EffectResult || recorded.Result.Hashes.EffectLedger == fixture.beforeResult.EffectLedger || recorded.Result.Hashes.Declarations == fixture.beforeResult.Declarations {
		t.Fatalf("completed result was not durably recorded: %#v", transcript.Records[2])
	}
	if recorded.Prior.Hashes != fixture.beforeResult {
		t.Fatalf("completed result prior hashes = %#v, want %#v", recorded.Prior.Hashes, fixture.beforeResult)
	}
	snapshot, err := fixture.session.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Result.Hashes != snapshot.Hashes {
		t.Fatalf("transcript result hashes differ from snapshot: record=%#v snapshot=%#v", recorded.Result.Hashes, snapshot.Hashes)
	}
	original, err := fixture.session.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}

	playback := newSession(t, Options[model]{
		QueueCapacity: 8, MaxActiveBatches: 1, EffectParallelism: 2,
		Reduce: saturatedEffectResultReducer, View: testView,
		EffectPorts: blockedEffectPorts(),
	})
	defer playback.Close()
	_, err = replay.Execute(context.Background(), transcript, replayThroughSession(t, playback))
	if err != nil {
		t.Fatalf("completed result transcript is not replayable: %v", err)
	}
	checkpoint, err := playback.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Checksum != original.Checksum {
		t.Fatalf("replayed checkpoint checksum = %q, want %q", checkpoint.Checksum, original.Checksum)
	}
}

func blockedEffectPorts() map[string]effect.Port {
	blocked := effect.PortFunc(func(ctx context.Context, _ effect.Call) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	return map[string]effect.Port{"complete": blocked, "hold": blocked, "pending": blocked, "followup": blocked}
}

func replayThroughSession(t *testing.T, playback *Session[model]) replay.ApplyFunc {
	t.Helper()
	return func(_ context.Context, _ state.Checkpoint, recorded event.Event) (state.Checkpoint, error) {
		if err := playback.Send(recorded); err != nil {
			return state.Checkpoint{}, err
		}
		waitSnapshot(t, playback, func(snapshot state.State[model]) bool { return snapshot.Sequence == recorded.Sequence })
		return playback.Checkpoint()
	}
}
