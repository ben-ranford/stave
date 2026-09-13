package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/replay"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
)

type model struct {
	Count          int   `json:"count"`
	EffectOrdinals []int `json:"effectOrdinals,omitempty"`
}

func TestSessionSerializesReducerOwnership(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32

	s := newSession(t, Options[model]{
		QueueCapacity: 64,
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			n := atomic.AddInt32(&concurrent, 1)
			for {
				prev := atomic.LoadInt32(&maxConcurrent)
				if n <= prev || atomic.CompareAndSwapInt32(&maxConcurrent, prev, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			current.Count++
			atomic.AddInt32(&concurrent, -1)
			return current, nil, nil
		},
		View: testView,
	})
	defer s.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
				t.Errorf("Send() error = %v", err)
			}
		}()
	}
	wg.Wait()
	waitSnapshot(t, s, func(snapshot state.State[model]) bool {
		return snapshot.Sequence == 20
	})
	if got := atomic.LoadInt32(&maxConcurrent); got != 1 {
		t.Fatalf("max concurrent reducer calls = %d, want 1", got)
	}
}

func TestSessionWaitKeepsPollingTimeVaryingPredicate(t *testing.T) {
	s := newSession(t, Options[model]{
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			return current, nil, nil
		},
		View: testView,
	})
	defer s.Close()

	deadline := time.Now().Add(5 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var evaluations atomic.Int32
	if err := s.Wait(ctx, func(state.State[model]) bool {
		evaluations.Add(1)
		return !time.Now().Before(deadline)
	}); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got := evaluations.Load(); got < 2 {
		t.Fatalf("Wait predicate evaluations = %d, want polling to re-evaluate time-varying predicate", got)
	}
}

func TestSessionWaitForPublicationWakesOnSequenceOnlyPublication(t *testing.T) {
	s := newSession(t, Options[model]{
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			return current, nil, errors.New("reject")
		},
		View: testView,
	})
	defer s.Close()

	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- s.WaitForPublication(context.Background(), func(snapshot state.State[model]) bool {
			if snapshot.Sequence == 0 {
				close(entered)
			}
			return snapshot.Sequence == 1
		})
	}()
	<-entered
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("WaitForPublication() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForPublication did not observe sequence-only publication")
	}
	assertDiagnostic(t, s.Diagnostics(), "REDUCE_FAILED")
}

func TestSessionWaitForPublicationWakesOnTransientDiagnostic(t *testing.T) {
	s := newSession(t, Options[model]{
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			return current, nil, nil
		},
		View: testView,
	})
	defer s.Close()

	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- s.WaitForPublication(context.Background(), func(state.State[model]) bool {
			diagnostics := s.Diagnostics()
			if len(diagnostics) == 0 {
				close(entered)
			}
			return len(diagnostics) == 1
		})
	}()
	<-entered
	s.addTransientDiagnostic("TEST", "test", nil)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("WaitForPublication() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForPublication did not observe transient diagnostic")
	}
}

func TestSessionWaitForPublicationAvoidsIdleClonePolling(t *testing.T) {
	var clones atomic.Int32
	s := newSession(t, Options[model]{
		ModelPolicy: state.ModelPolicy[model]{
			Clone: func(current model) (model, error) {
				clones.Add(1)
				return current, nil
			},
		},
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			return current, nil, nil
		},
		View: testView,
	})
	defer s.Close()
	clones.Store(0)

	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- s.WaitForPublication(ctx, func(state.State[model]) bool {
			close(entered)
			return false
		})
	}()
	<-entered
	if got := clones.Load(); got != 1 {
		t.Fatalf("initial WaitForPublication clones = %d, want 1", got)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitForPublication() error = %v, want context.Canceled", err)
	}
	if got := clones.Load(); got != 1 {
		t.Fatalf("idle WaitForPublication clones = %d, want 1", got)
	}
}

func TestSessionWaitForPublicationWakesConcurrentWaitersAndClose(t *testing.T) {
	s := newSession(t, Options[model]{
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			current.Count++
			return current, nil, nil
		},
		View: testView,
	})

	const waiters = 4
	ready := make(chan struct{}, waiters)
	results := make(chan error, waiters)
	for range waiters {
		go func() {
			results <- s.WaitForPublication(context.Background(), func(snapshot state.State[model]) bool {
				if snapshot.Sequence == 0 {
					ready <- struct{}{}
				}
				return snapshot.Sequence == 1
			})
		}()
	}
	for range waiters {
		<-ready
	}
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	for range waiters {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("WaitForPublication() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent WaitForPublication waiter did not wake")
		}
	}

	closeReady := make(chan struct{})
	closed := make(chan error, 1)
	go func() {
		closed <- s.WaitForPublication(context.Background(), func(state.State[model]) bool {
			close(closeReady)
			return false
		})
	}()
	<-closeReady
	s.Close()
	if err := <-closed; !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("WaitForPublication() close error = %v, want ErrSessionClosed", err)
	}
}

func TestSessionQueueBackpressureAndCoalescing(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var (
		mu        sync.Mutex
		kinds     []event.Kind
		widths    []int
		coalesced bool
	)
	s := newSession(t, Options[model]{
		QueueCapacity: 2,
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			mu.Lock()
			kinds = append(kinds, ev.Kind)
			if payload, ok := ev.Payload.(event.ResizePayload); ok {
				widths = append(widths, payload.Width)
				coalesced = coalesced || ev.Meta.Coalesced
			}
			mu.Unlock()
			if ev.Sequence == 1 {
				close(started)
				<-release
			}
			current.Count++
			return current, nil, nil
		},
		View: testView,
	})
	defer s.Close()

	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.Send(mustEvent(t, event.Resize, event.ResizePayload{Width: 10, Height: 5})); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(mustEvent(t, event.Resize, event.ResizePayload{Width: 20, Height: 5})); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "tab"})); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "escape"})); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("Send() error = %v, want %v", err, ErrBackpressure)
	}
	close(release)

	waitSnapshot(t, s, func(snapshot state.State[model]) bool {
		return snapshot.Sequence == 3
	})

	mu.Lock()
	defer mu.Unlock()
	if got, want := len(kinds), 3; got != want {
		t.Fatalf("processed events = %d, want %d", got, want)
	}
	if kinds[1] != event.Resize || widths[0] != 20 || !coalesced {
		t.Fatalf("coalescing failed, kinds=%v widths=%v coalesced=%v", kinds, widths, coalesced)
	}
}

func TestSessionViewFailureRejectsLateMutationAndPublishesDiagnostic(t *testing.T) {
	s := newSession(t, Options[model]{
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			current.Count = 1
			return current, nil, nil
		},
		View: func(ctx context.Context, current model) (ViewResult, error) {
			if current.Count == 1 {
				return ViewResult{}, errors.New("boom")
			}
			return testView(ctx, current)
		},
	})
	defer s.Close()

	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, s, func(snapshot state.State[model]) bool {
		return snapshot.Sequence == 1
	})
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 1 || snapshot.Tree.Revision() != snapshot.Revision || snapshot.Model.Count != 0 {
		t.Fatalf("rejected view mutated state: %#v", snapshot)
	}
	assertDiagnostic(t, s.Diagnostics(), "VIEW_FAILED")
}

func TestSessionSendFreezesEventPayload(t *testing.T) {
	seen := make(chan string, 1)
	s := newSession(t, Options[model]{
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			payload := ev.Payload.(event.KeyPayload)
			seen <- payload.Modifiers[0]
			return current, nil, nil
		},
		View: testView,
	})
	defer s.Close()
	payload := event.KeyPayload{Key: "enter", Modifiers: []string{"ctrl"}}
	ev := mustEvent(t, event.Key, payload)
	if err := s.Send(ev); err != nil {
		t.Fatal(err)
	}
	payload.Modifiers[0] = "alt"
	select {
	case got := <-seen:
		if got != "ctrl" {
			t.Fatalf("reducer observed mutated event payload %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reducer")
	}
}

func TestSessionDeliversEffectResultsInDeclarationOrder(t *testing.T) {
	s := newSession(t, Options[model]{
		EffectParallelism: 2,
		EffectDelivery:    effect.DeclarationOrder,
		EffectPorts: map[string]effect.Port{
			"job": effect.PortFunc(func(ctx context.Context, call effect.Call) (any, error) {
				if call.Ordinal == 0 {
					time.Sleep(25 * time.Millisecond)
				}
				return nil, nil
			}),
		},
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			switch ev.Kind {
			case event.Key:
				current.Count++
				return current, []effect.Request{{Spec: effect.Spec{Kind: "job"}}, {Spec: effect.Spec{Kind: "job"}}}, nil
			case event.EffectResult:
				payload := ev.Payload.(event.EffectResultPayload)
				current.EffectOrdinals = append(current.EffectOrdinals, int(payload.Ordinal))
				return current, nil, nil
			default:
				return current, nil, nil
			}
		},
		View: testView,
	})
	defer s.Close()

	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, s, func(snapshot state.State[model]) bool {
		return len(snapshot.Model.EffectOrdinals) == 2
	})
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(snapshot.Model.EffectOrdinals) != "[0 1]" {
		t.Fatalf("effect ordinals = %v, want [0 1]", snapshot.Model.EffectOrdinals)
	}
}

func TestSessionRecordsCompletionOrderWhenConfigured(t *testing.T) {
	s := newSession(t, Options[model]{
		EffectParallelism: 2,
		EffectDelivery:    effect.CompletionOrder,
		EffectPorts: map[string]effect.Port{
			"job": effect.PortFunc(func(ctx context.Context, call effect.Call) (any, error) {
				if call.Ordinal == 0 {
					time.Sleep(30 * time.Millisecond)
				}
				return nil, nil
			}),
		},
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			switch ev.Kind {
			case event.Key:
				return current, []effect.Request{{Spec: effect.Spec{Kind: "job"}}, {Spec: effect.Spec{Kind: "job"}}}, nil
			case event.EffectResult:
				payload := ev.Payload.(event.EffectResultPayload)
				current.EffectOrdinals = append(current.EffectOrdinals, int(payload.Ordinal))
				return current, nil, nil
			default:
				return current, nil, nil
			}
		},
		View: testView,
	})
	defer s.Close()

	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, s, func(snapshot state.State[model]) bool {
		return len(snapshot.Model.EffectOrdinals) == 2
	})
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(snapshot.Model.EffectOrdinals) != "[1 0]" {
		t.Fatalf("effect ordinals = %v, want [1 0]", snapshot.Model.EffectOrdinals)
	}
	transcript, err := s.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if got := transcript.Records[len(transcript.Records)-2].CompletionIndex; got != 0 {
		t.Fatalf("first completion index = %d, want 0", got)
	}
}

func TestSessionCancellationDiscardsLateEffectResults(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s := newSession(t, Options[model]{
		EffectPorts: map[string]effect.Port{
			"late": effect.PortFunc(func(ctx context.Context, call effect.Call) (any, error) {
				close(started)
				<-release
				return "late", nil
			}),
		},
		Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
			switch ev.Kind {
			case event.Key:
				current.Count++
				return current, []effect.Request{{Spec: effect.Spec{Kind: "late"}}}, nil
			case event.EffectResult:
				current.Count += 100
				return current, nil, nil
			default:
				return current, nil, nil
			}
		},
		View: testView,
	})

	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	<-started
	waitSnapshot(t, s, func(snapshot state.State[model]) bool {
		return snapshot.Sequence == 1
	})
	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	<-closed
	close(release)
	waitDiagnostic(t, s, "LATE_EFFECT_RESULT")

	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Sequence != 1 || snapshot.Model.Count != 1 {
		t.Fatalf("late effect mutated closed session: %#v", snapshot)
	}
}

func TestSessionCheckpointAndReplayStayDeterministic(t *testing.T) {
	makeSession := func() *Session[model] {
		return newSession(t, Options[model]{
			SessionID: "deterministic-session",
			Reduce: func(ctx context.Context, current model, ev event.Event) (model, []effect.Request, error) {
				current.Count++
				return current, nil, nil
			},
			View: testView,
		})
	}
	s1 := makeSession()
	defer s1.Close()
	s2 := makeSession()
	defer s2.Close()

	for i := 0; i < 3; i++ {
		ev := mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})
		if err := s1.Send(ev); err != nil {
			t.Fatal(err)
		}
		if err := s2.Send(ev); err != nil {
			t.Fatal(err)
		}
	}

	waitSnapshot(t, s1, func(snapshot state.State[model]) bool { return snapshot.Sequence == 3 })
	waitSnapshot(t, s2, func(snapshot state.State[model]) bool { return snapshot.Sequence == 3 })

	cp1, err := s1.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	cp2, err := s2.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if cp1.Checksum != cp2.Checksum {
		t.Fatalf("checkpoint checksums differ: %s != %s", cp1.Checksum, cp2.Checksum)
	}

	tr1, err := s1.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	tr2, err := s2.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.Validate(tr1, tr2); err != nil {
		t.Fatalf("replay validation failed: %v", err)
	}
}

func newSession(t *testing.T, opts Options[model]) *Session[model] {
	t.Helper()
	if opts.View == nil {
		opts.View = testView
	}
	s, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testView(ctx context.Context, current model) (ViewResult, error) {
	id, err := semantic.NodeIDFor(semantic.NodeKey{
		AppNamespace: "stave",
		View:         "session-test",
		Kind:         "root",
		Entity:       fmt.Sprintf("%d", current.Count),
		Slot:         fmt.Sprintf("%d", len(current.EffectOrdinals)),
	})
	if err != nil {
		return ViewResult{}, err
	}
	node, err := semantic.NewNode(semantic.NodeSpec{
		ID:         id,
		Generation: 1,
		Role:       "application",
		Name:       fmt.Sprintf("count-%d", current.Count),
		Flags:      semantic.Flags{Visible: true},
	})
	if err != nil {
		return ViewResult{}, err
	}
	tree, err := semantic.NewTree(uint64(current.Count+1), node)
	if err != nil {
		return ViewResult{}, err
	}
	return ViewResult{Tree: tree, SurfaceHash: fmt.Sprintf("surface-%d-%d", current.Count, len(current.EffectOrdinals))}, nil
}

func mustEvent(t *testing.T, kind event.Kind, payload any) event.Event {
	t.Helper()
	ev, err := event.New(kind, payload)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func waitSnapshot(t *testing.T, s *Session[model], predicate func(state.State[model]) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Wait(ctx, predicate); err != nil {
		t.Fatal(err)
	}
}

func assertDiagnostic(t *testing.T, diagnostics []Diagnostic, code string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("missing diagnostic %q in %#v", code, diagnostics)
}

func waitDiagnostic(t *testing.T, s *Session[model], code string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, diagnostic := range s.Diagnostics() {
			if diagnostic.Code == code {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("missing diagnostic %q in %#v", code, s.Diagnostics())
}
