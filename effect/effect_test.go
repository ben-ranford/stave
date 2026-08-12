package effect

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ben-ranford/stave/event"
)

func TestIDDeterminism(t *testing.T) {
	first := ID("session-a", 4, 2, "load")
	if first != ID("session-a", 4, 2, "load") {
		t.Fatal("effect id changed between invocations")
	}
	if first == ID("session-a", 5, 2, "load") {
		t.Fatal("effect id ignored sequence")
	}
}

func TestBindAssignsDeterministicIDsAndOrdinals(t *testing.T) {
	calls, err := Bind("session-a", 7, 3, []Request{
		{Spec: Spec{Kind: "first"}},
		{Spec: Spec{Kind: "second"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Ordinal != 0 || calls[1].Ordinal != 1 {
		t.Fatalf("unexpected bound calls %#v", calls)
	}
	if calls[0].ID == calls[1].ID {
		t.Fatalf("distinct requests received same id: %#v", calls)
	}
}

func TestExecutorDeliversDeclarationOrderDespiteParallelCompletion(t *testing.T) {
	exec := NewExecutor(Options{
		Parallelism: 2,
		Delivery:    DeclarationOrder,
		Ports: map[string]Port{
			"slow": PortFunc(func(ctx context.Context, call Call) (any, error) {
				if call.Ordinal == 0 {
					time.Sleep(25 * time.Millisecond)
				}
				return call.Ordinal, nil
			}),
		},
	})

	calls, err := Bind("s", 1, 1, []Request{{Spec: Spec{Kind: "slow"}}, {Spec: Spec{Kind: "slow"}}})
	if err != nil {
		t.Fatal(err)
	}

	var (
		mu     sync.Mutex
		events []event.Event
		done   = make(chan struct{})
	)
	if err := exec.Deliver(context.Background(), calls, func(ev event.Event) error {
		mu.Lock()
		events = append(events, ev)
		if len(events) == len(calls) {
			close(done)
		}
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for declaration-order delivery")
	}

	mu.Lock()
	defer mu.Unlock()
	if got := events[0].Payload.(event.EffectResultPayload).Ordinal; got != 0 {
		t.Fatalf("first delivered ordinal = %d, want 0", got)
	}
	if got := events[1].Payload.(event.EffectResultPayload).Ordinal; got != 1 {
		t.Fatalf("second delivered ordinal = %d, want 1", got)
	}
}

func TestExecutorRecordsCompletionOrderWhenRequested(t *testing.T) {
	exec := NewExecutor(Options{
		Parallelism: 2,
		Delivery:    CompletionOrder,
		Ports: map[string]Port{
			"job": PortFunc(func(ctx context.Context, call Call) (any, error) {
				if call.Ordinal == 0 {
					time.Sleep(30 * time.Millisecond)
				}
				return nil, nil
			}),
		},
	})

	calls, err := Bind("s", 1, 1, []Request{{Spec: Spec{Kind: "job"}}, {Spec: Spec{Kind: "job"}}})
	if err != nil {
		t.Fatal(err)
	}

	var (
		mu     sync.Mutex
		events []event.Event
		done   = make(chan struct{})
	)
	if err := exec.Deliver(context.Background(), calls, func(ev event.Event) error {
		mu.Lock()
		events = append(events, ev)
		if len(events) == len(calls) {
			close(done)
		}
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion-order delivery")
	}

	mu.Lock()
	defer mu.Unlock()
	if got := events[0].Payload.(event.EffectResultPayload).Ordinal; got != 1 {
		t.Fatalf("first completion ordinal = %d, want 1", got)
	}
	if got := events[0].Meta.CompletionIndex; got != 0 {
		t.Fatalf("first completion index = %d, want 0", got)
	}
}

func TestExecutorClassifiesCancellationAndTimeout(t *testing.T) {
	timeoutExec := NewExecutor(Options{
		Parallelism: 1,
		Ports: map[string]Port{
			"timeout": PortFunc(func(ctx context.Context, call Call) (any, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}),
		},
	})
	timeoutCalls, err := Bind("s", 1, 1, []Request{{Spec: Spec{Kind: "timeout", Timeout: 10 * time.Millisecond}}})
	if err != nil {
		t.Fatal(err)
	}

	timeoutDone := make(chan event.Event, 1)
	if err := timeoutExec.Deliver(context.Background(), timeoutCalls, func(ev event.Event) error {
		timeoutDone <- ev
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	timeoutEvent := <-timeoutDone
	if got := timeoutEvent.Payload.(event.EffectResultPayload).Status; got != string(StatusTimedOut) {
		t.Fatalf("timeout status = %q, want %q", got, StatusTimedOut)
	}

	cancelExec := NewExecutor(Options{
		Parallelism: 1,
		Ports: map[string]Port{
			"cancel": PortFunc(func(ctx context.Context, call Call) (any, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}),
		},
	})
	cancelCalls, err := Bind("s", 1, 1, []Request{{Spec: Spec{Kind: "cancel", Cancellable: true}}})
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelDone := make(chan event.Event, 1)
	if err := cancelExec.Deliver(cancelCtx, cancelCalls, func(ev event.Event) error {
		cancelDone <- ev
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cancel()
	cancelEvent := <-cancelDone
	if got := cancelEvent.Payload.(event.EffectResultPayload).Status; got != string(StatusCancelled) {
		t.Fatalf("cancel status = %q, want %q", got, StatusCancelled)
	}
}

func TestExecutorCloseRejectsNewBatches(t *testing.T) {
	exec := NewExecutor(Options{
		Ports: map[string]Port{
			"job": PortFunc(func(ctx context.Context, call Call) (any, error) { return nil, nil }),
		},
	})
	exec.Close()

	calls, err := Bind("s", 1, 1, []Request{{Spec: Spec{Kind: "job"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Deliver(context.Background(), calls, func(event.Event) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Deliver error = %v, want %v", err, ErrClosed)
	}
}

func TestExecutorBoundsBatchAndRedactsSensitiveFailure(t *testing.T) {
	exec := NewExecutor(Options{MaxBatch: 1, Ports: map[string]Port{"secret": PortFunc(func(context.Context, Call) (any, error) { panic("credential") })}})
	calls, err := Bind("s", 1, 1, []Request{{Spec: Spec{Kind: "secret", Sensitive: true}}, {Spec: Spec{Kind: "secret", Sensitive: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Deliver(context.Background(), calls, func(event.Event) error { return nil }); err == nil {
		t.Fatal("expected batch bound error")
	}
	calls, err = Bind("s", 1, 1, []Request{{Spec: Spec{Kind: "secret", Sensitive: true}}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan event.Event, 1)
	if err := exec.Deliver(context.Background(), calls, func(ev event.Event) error { done <- ev; return nil }); err != nil {
		t.Fatal(err)
	}
	payload := (<-done).Payload.(event.EffectResultPayload)
	if payload.Error == "" || payload.Error == "credential" {
		t.Fatalf("sensitive error leaked: %q", payload.Error)
	}
}

func TestOutcomeEventSanitizesUntrustedErrorControls(t *testing.T) {
	ev := (Outcome{ID: "fx1_test", Status: StatusFailed, Error: "bad\x1b[31m\u009bsecret"}).Event()
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := ev.Payload.(event.EffectResultPayload)
	if payload.Error != "bad[31msecret" {
		t.Fatalf("sanitized error = %q", payload.Error)
	}
}
