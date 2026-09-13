package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
)

func TestEffectAdmissionReservationPublicationBoundary(t *testing.T) {
	q := newEffectAdmissionQueue(1)
	defer q.close()
	if _, err := q.reserve(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-q.committed:
		t.Fatal("unpublished reservation became available to the worker")
	default:
	}
	if _, err := q.reserve(); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("second reservation = %v, want backpressure", err)
	}
	q.commit([]effect.Call{{Sequence: 1}})
	calls, ok := q.next(context.Background())
	if !ok || len(calls) != 1 || calls[0].Sequence != 1 {
		t.Fatalf("committed batch = %#v, %t", calls, ok)
	}
	if _, err := q.reserve(); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("worker-held batch lost its reservation: %v", err)
	}
	q.release()
	if _, err := q.reserve(); err != nil {
		t.Fatalf("completed admission did not release capacity: %v", err)
	}
	q.commit([]effect.Call{{Sequence: 2}})
	calls, ok = q.next(context.Background())
	if !ok || len(calls) != 1 || calls[0].Sequence != 2 {
		t.Fatalf("next committed batch = %#v, %t", calls, ok)
	}
	q.release()
}

func TestEffectAdmissionCloseCancelsCommittedAndUnpublishedBatches(t *testing.T) {
	q := newEffectAdmissionQueue(2)
	for range 2 {
		if _, err := q.reserve(); err != nil {
			t.Fatal(err)
		}
	}
	q.commit([]effect.Call{{Sequence: 1}})
	q.close()
	q.close()
	q.commit([]effect.Call{{Sequence: 2}})
	q.release()
	if len(q.committed) != 0 || q.reserved != 0 {
		t.Fatalf("closed queue retained batches or reservations: %d, %d", len(q.committed), q.reserved)
	}
	if _, err := q.reserve(); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("closed queue reserve = %v", err)
	}
	if calls, ok := q.next(context.Background()); ok || calls != nil {
		t.Fatalf("closed queue produced %#v", calls)
	}
}

func TestEffectAdmissionWaitHonorsCancellation(t *testing.T) {
	q := newEffectAdmissionQueue(1)
	defer q.close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if calls, ok := q.next(ctx); ok || calls != nil {
		t.Fatalf("canceled wait produced %#v", calls)
	}
}

func TestSessionEffectsWaitForPublication(t *testing.T) {
	viewEntered := make(chan struct{})
	resumeView := make(chan struct{})
	started := make(chan struct{}, 1)
	observed := make(chan error, 1)
	var viewOnce sync.Once
	var s *Session[model]
	s = newSession(t, Options[model]{
		EffectPorts: map[string]effect.Port{"probe": effect.PortFunc(func(_ context.Context, call effect.Call) (any, error) {
			started <- struct{}{}
			snapshot, err := s.Snapshot()
			if err != nil {
				observed <- err
				return nil, err
			}
			transcript, err := s.Transcript()
			if err == nil && (snapshot.Sequence != call.Sequence || snapshot.Hashes.Declarations == "" || len(transcript.Records) != 1) {
				err = fmt.Errorf("unpublished effect: sequence=%d call=%d declarations=%q records=%d", snapshot.Sequence, call.Sequence, snapshot.Hashes.Declarations, len(transcript.Records))
			}
			if err == nil {
				record := transcript.Records[0]
				if record.Event.Kind != event.Key || record.Result.Sequence != call.Sequence || record.Result.Hashes.Declarations != snapshot.Hashes.Declarations {
					err = errors.New("producer transcript and published declaration disagree")
				}
			}
			observed <- err
			return nil, err
		})},
		Reduce: func(_ context.Context, m model, ev event.Event) (model, []effect.Request, error) {
			if ev.Kind != event.Key {
				return m, nil, nil
			}
			m.Count++
			return m, []effect.Request{{Spec: effect.Spec{Kind: "probe"}}}, nil
		},
		View: func(ctx context.Context, m model) (ViewResult, error) {
			if m.Count == 1 {
				viewOnce.Do(func() { close(viewEntered); <-resumeView })
			}
			return testView(ctx, m)
		},
	})
	defer s.Close()
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	<-viewEntered
	// Let rendering finish while publication is deliberately unable to acquire
	// its lock. The queue test above proves the reservation boundary without a
	// timing assumption; this integration check exercises the actual worker.
	s.mu.Lock()
	close(resumeView)
	select {
	case <-started:
		sequence, records := s.current.Sequence, len(s.transcript.Records)
		s.mu.Unlock()
		t.Fatalf("effect started before publication: sequence=%d records=%d", sequence, records)
	case <-time.After(50 * time.Millisecond):
		s.mu.Unlock()
	}
	select {
	case err := <-observed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("published effect did not reach its port")
	}
}

func TestEffectAdmissionDequeueObservesClosureBeforeDoneSignal(t *testing.T) {
	q := newEffectAdmissionQueue(1)
	if _, err := q.reserve(); err != nil {
		t.Fatal(err)
	}
	q.commit([]effect.Call{{Sequence: 1}})
	// Model the close operation after it marks the queue closed but before it
	// signals done or drains the buffer. Receiving alone cannot authorize work.
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	calls, ok := q.next(context.Background())
	close(q.done)
	if ok || calls != nil {
		t.Fatalf("closing queue returned pending batch %#v", calls)
	}
}

func TestSessionSaturatedTerminalEventsCloseWithEffectRequests(t *testing.T) {
	for _, kind := range []event.Kind{event.Cancel, event.Shutdown} {
		t.Run(string(kind), func(t *testing.T) {
			firstStarted := make(chan struct{})
			s := newSession(t, Options[model]{
				QueueCapacity: 1, MaxActiveBatches: 1,
				EffectPorts: map[string]effect.Port{"hold": effect.PortFunc(func(ctx context.Context, call effect.Call) (any, error) {
					if call.Sequence == 1 {
						close(firstStarted)
					}
					<-ctx.Done()
					return nil, ctx.Err()
				})},
				Reduce: func(_ context.Context, current model, _ event.Event) (model, []effect.Request, error) {
					current.Count++
					return current, []effect.Request{{Spec: effect.Spec{Kind: "hold"}}}, nil
				},
			})
			defer s.Close()
			startBlockedAndQueuePendingBatch(t, s, firstStarted)
			if err := s.Send(mustEvent(t, kind, nil)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-s.loopDone:
				if s.Lifecycle() != LifecycleClosed {
					t.Fatal("terminal event did not close the session")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("saturated terminal event did not stop the session")
			}
		})
	}
}

func effectAdmissionFailureOptions(stage string, started chan struct{}) Options[model] {
	return Options[model]{
		QueueCapacity: 1, MaxActiveBatches: 1,
		EffectPorts: map[string]effect.Port{"hold": effect.PortFunc(func(ctx context.Context, _ effect.Call) (any, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})},
		Reduce: func(_ context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			current.Count++
			kind := "hold"
			if ev.Payload.(event.KeyPayload).Key == "enter" {
				current.EffectOrdinals = []int{1}
				if stage == "bind" {
					kind = ""
				}
			}
			return current, []effect.Request{{Spec: effect.Spec{Kind: kind}}}, nil
		},
		View: func(ctx context.Context, current model) (ViewResult, error) {
			if stage == "view" && len(current.EffectOrdinals) > 0 {
				return ViewResult{}, errors.New("view rejected the proposal")
			}
			return testView(ctx, current)
		},
	}
}

func TestSessionEffectAdmissionReleasesReservationAfterFailure(t *testing.T) {
	for _, failure := range []struct{ stage, diagnostic string }{{"view", "VIEW_FAILED"}, {"bind", "EFFECT_BIND_FAILED"}} {
		t.Run(failure.stage, func(t *testing.T) {
			started := make(chan struct{})
			s := newSession(t, effectAdmissionFailureOptions(failure.stage, started))
			defer s.Close()
			if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
				t.Fatal(err)
			}
			waitDiagnostic(t, s, failure.diagnostic)
			if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "space"})); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
				snapshot, err := s.Snapshot()
				if err != nil || snapshot.Sequence != 2 || snapshot.Model.Count != 1 {
					t.Fatalf("valid retry state = %#v, %v", snapshot, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("failed proposal did not release admission capacity")
			}
		})
	}
}

func newHeldEffectSession(t *testing.T, ctx context.Context) (*Session[model], <-chan struct{}, <-chan struct{}) {
	t.Helper()
	started, canceled := make(chan struct{}), make(chan struct{})
	s, err := New(ctx, Options[model]{
		QueueCapacity: 1, MaxActiveBatches: 1, View: testView,
		EffectPorts: map[string]effect.Port{"hold": effect.PortFunc(func(ctx context.Context, call effect.Call) (any, error) {
			if call.Sequence == 1 {
				close(started)
			}
			<-ctx.Done()
			if call.Sequence == 1 {
				close(canceled)
			}
			return nil, ctx.Err()
		})},
		Reduce: func(_ context.Context, current model, _ event.Event) (model, []effect.Request, error) {
			current.Count++
			return current, []effect.Request{{Spec: effect.Spec{Kind: "hold"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, started, canceled
}

func TestSessionParentCancellationClosesPendingAdmissions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, started, canceled := newHeldEffectSession(t, ctx)
	startBlockedAndQueuePendingBatch(t, s, started)
	cancel()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("parent cancellation left a non-cancellable effect running")
	}
	select {
	case <-s.effectAdmissions.done:
	case <-time.After(2 * time.Second):
		t.Fatal("parent cancellation did not close admission queue")
	}
	s.effectAdmissions.mu.Lock()
	reserved, buffered := s.effectAdmissions.reserved, len(s.effectAdmissions.committed)
	s.effectAdmissions.mu.Unlock()
	if reserved != 0 || buffered != 0 {
		t.Fatalf("parent cancellation retained pending work: reservations=%d buffered=%d", reserved, buffered)
	}
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("send after parent cancellation = %v", err)
	}
}

func TestSessionClosesExecutorBeforeDrainingAdmissions(t *testing.T) {
	s, started, canceled := newHeldEffectSession(t, context.Background())
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	<-started
	// Pause admission cleanup to exercise an already-dequeued worker's delivery
	// attempt during shutdown. Executor closure must already be authoritative.
	s.effectAdmissions.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.effectAdmissions.mu.Unlock()
		}
	}()
	closed := make(chan struct{})
	go func() { s.beginClose(); close(closed) }()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("executor remained open while admission cleanup was blocked")
	}
	calls, err := effect.Bind("stave-session", 2, 1, []effect.Request{{Spec: effect.Spec{Kind: "hold"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.effects.Deliver(context.Background(), calls, func(event.Event) error { return nil }); !errors.Is(err, effect.ErrClosed) {
		t.Fatalf("delivery during admission cleanup = %v, want ErrClosed", err)
	}
	s.effectAdmissions.mu.Unlock()
	locked = false
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("session close did not finish")
	}
}

func TestEffectAdmissionCanceledWaitDiscardsBufferedBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 64 {
		q := newEffectAdmissionQueue(1)
		if _, err := q.reserve(); err != nil {
			t.Fatal(err)
		}
		q.commit([]effect.Call{{Sequence: 1}})
		calls, ok := q.next(ctx)
		q.close()
		if ok || calls != nil {
			t.Fatalf("canceled wait returned pending work: %#v", calls)
		}
	}
}
