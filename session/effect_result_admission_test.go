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
	fixture := newSaturatedEffectResultFixture(t, "")
	defer fixture.session.Close()

	fixture.startInitialBatch(t)
	fixture.queuePendingBatch(t)
	close(fixture.releaseComplete)

	fixture.assertCompletedResultWasPublished(t)
	fixture.assertUnadmittedWorkWasCancelled(t)
	fixture.assertTranscriptReplays(t)
}

func TestSessionOverloadCloseDiscardsBufferedInput(t *testing.T) {
	fixture := newSaturatedEffectResultFixture(t, "")
	t.Cleanup(fixture.session.Close)
	release, resume := context.WithCancel(context.Background())
	t.Cleanup(resume)
	resultRendering := make(chan struct{})
	// Configure the view before sending any events. Pause after the result's
	// terminal admission decision, so another input is definitely buffered.
	view := fixture.session.view
	fixture.session.view = func(ctx context.Context, current model) (ViewResult, error) {
		if current.Count == 12 && release.Err() == nil {
			close(resultRendering)
			<-release.Done()
		}
		return view(ctx, current)
	}
	fixture.startInitialBatch(t)
	fixture.queuePendingBatch(t)
	close(fixture.releaseComplete)
	select {
	case <-resultRendering:
	case <-time.After(2 * time.Second):
		t.Fatal("completed result did not reach publication")
	}
	if err := fixture.session.Send(mustEvent(t, event.Text, event.TextPayload{Text: "buffered"})); err != nil {
		t.Fatal(err)
	}
	resume()
	select {
	case <-fixture.session.loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("overloaded session did not close")
	}
	snapshot, err := fixture.session.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Sequence != 3 {
		t.Fatalf("closed sequence = %d, want 3; buffered input was published", snapshot.Sequence)
	}
	fixture.assertCompletedResultWasPublished(t)
	fixture.assertUnadmittedWorkWasCancelled(t)
	fixture.assertTranscriptReplays(t)
}

func TestSessionClosesAfterSaturatedCompletedResultLaterFails(t *testing.T) {
	for _, failure := range []struct {
		name, diagnostic string
	}{
		{name: "view", diagnostic: "VIEW_FAILED"},
		{name: "bind", diagnostic: "EFFECT_BIND_FAILED"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			fixture := newSaturatedEffectResultFixture(t, failure.name)
			defer fixture.session.Close()

			fixture.startInitialBatch(t)
			fixture.queuePendingBatch(t)
			close(fixture.releaseComplete)

			fixture.assertRejectedResultClosed(t, failure.diagnostic)
			fixture.assertUnadmittedWorkWasCancelled(t)
		})
	}
}

type saturatedEffectResultFixture struct {
	session         *Session[model]
	releaseComplete chan struct{}
	initialStarted  chan struct{}
	holdCancelled   chan struct{}
	pendingStarted  chan struct{}
	followupStarted chan struct{}
	beforeResult    state.Hashes
	beforeState     state.State[model]
}

func newSaturatedEffectResultFixture(t *testing.T, failure string) *saturatedEffectResultFixture {
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
		Reduce: saturatedEffectResultReducer(failure),
		View:   saturatedEffectResultView(failure),
	})
	return fixture
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
	fixture.beforeState = snapshot
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

func (fixture *saturatedEffectResultFixture) assertRejectedResultClosed(t *testing.T, diagnostic string) {
	t.Helper()
	waitSnapshot(t, fixture.session, func(snapshot state.State[model]) bool {
		return snapshot.Sequence == 3
	})
	select {
	case <-fixture.session.loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("failed saturated result admission did not close the session")
	}
	if fixture.session.Lifecycle() != LifecycleClosed {
		t.Fatalf("lifecycle = %v, want closed", fixture.session.Lifecycle())
	}
	snapshot, err := fixture.session.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Model.Count != fixture.beforeState.Model.Count || fmt.Sprint(snapshot.Model.EffectOrdinals) != fmt.Sprint(fixture.beforeState.Model.EffectOrdinals) || snapshot.Revision != fixture.beforeState.Revision {
		t.Fatalf("rejected result changed model or revision: %#v, want %#v", snapshot, fixture.beforeState)
	}
	if snapshot.Hashes.EffectLedger != fixture.beforeResult.EffectLedger || snapshot.Hashes.Declarations != fixture.beforeResult.Declarations {
		t.Fatalf("rejected result changed effect hashes: %#v, want %#v", snapshot.Hashes, fixture.beforeResult)
	}
	assertDiagnostic(t, fixture.session.Diagnostics(), diagnostic)
	assertDiagnostic(t, fixture.session.Diagnostics(), "EFFECT_ADMISSION_BACKPRESSURE")
	fixture.assertRejectedResultRecorded(t, snapshot)
}

func (fixture *saturatedEffectResultFixture) assertRejectedResultRecorded(t *testing.T, snapshot state.State[model]) {
	t.Helper()
	transcript, err := fixture.session.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if len(transcript.Records) != 3 {
		t.Fatalf("records = %d, want key, key, rejected result", len(transcript.Records))
	}
	recorded := transcript.Records[2]
	if recorded.Event.Kind != event.EffectResult || recorded.Event.Sequence != 3 || recorded.Result.Sequence != 3 || recorded.Result.Revision != fixture.beforeState.Revision {
		t.Fatalf("rejected result record = %#v", recorded)
	}
	if recorded.Prior.Hashes != fixture.beforeResult || recorded.Result.Hashes != snapshot.Hashes {
		t.Fatalf("rejected result hashes = prior %#v result %#v", recorded.Prior.Hashes, recorded.Result.Hashes)
	}
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
		Reduce: saturatedEffectResultReducer(""), View: testView,
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

func saturatedEffectResultReducer(failure string) func(context.Context, model, event.Event) (model, []effect.Request, error) {
	return func(_ context.Context, current model, ev event.Event) (model, []effect.Request, error) {
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
			if failure == "bind" {
				return current, []effect.Request{{Spec: effect.Spec{Kind: ""}}}, nil
			}
			return current, []effect.Request{{Spec: effect.Spec{Kind: "followup"}}}, nil
		default:
			return current, nil, nil
		}
	}
}

func saturatedEffectResultView(failure string) func(context.Context, model) (ViewResult, error) {
	return func(ctx context.Context, current model) (ViewResult, error) {
		if failure == "view" && current.Count == 12 {
			return ViewResult{}, fmt.Errorf("result view rejected proposal")
		}
		return testView(ctx, current)
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
