package human

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/secret"
	"github.com/ben-ranford/stave/surface"
)

func TestRuntimeRestoresOnEOFAndClosesOnce(t *testing.T) {
	d := newFakeDriver(capability.Manifest{TTY: true, Interactive: true, AlternateScreen: true, CursorAddressing: true})
	d.events <- event.Event{Kind: event.Resize, Payload: event.ResizePayload{Width: 80, Height: 24}}
	close(d.events)
	var seen []event.Kind
	r, err := New(Options{Driver: d, Handle: func(_ context.Context, ev event.Event) error {
		seen = append(seen, ev.Kind)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != event.Resize || seen[1] != event.Shutdown {
		t.Fatalf("seen=%v", seen)
	}
	if d.restoreCount != 1 || d.closeCount != 1 {
		t.Fatalf("restore=%d close=%d", d.restoreCount, d.closeCount)
	}
	if r.Profile() != ProfileFullScreen {
		t.Fatalf("profile=%s", r.Profile())
	}
}

func TestRuntimeDrawFailureRestores(t *testing.T) {
	d := newFakeDriver(capability.Manifest{TTY: true, Interactive: true})
	d.events <- event.Event{Kind: event.Text, Payload: event.TextPayload{Text: "x"}}
	r, _ := New(Options{Driver: d, Draw: func(context.Context, event.Event) (surface.Surface, surface.Patch, error) {
		return surface.New(1, 1), surface.Patch{}, errors.New("draw failed")
	}})
	if err := r.Run(context.Background()); err == nil || err.Error() != "draw failed" {
		t.Fatalf("err=%v", err)
	}
	if d.restoreCount != 1 {
		t.Fatalf("restore count=%d", d.restoreCount)
	}
}

func TestRuntimeSignalAndPanicRestore(t *testing.T) {
	t.Run("signal", func(t *testing.T) {
		d := newFakeDriver(capability.Manifest{TTY: true, Interactive: true})
		d.events <- event.Event{Kind: event.Shutdown}
		if err := Run(context.Background(), Options{Driver: d}); err != nil {
			t.Fatal(err)
		}
		if d.restoreCount != 1 {
			t.Fatalf("restore=%d", d.restoreCount)
		}
	})
	t.Run("panic", func(t *testing.T) {
		d := newFakeDriver(capability.Manifest{TTY: true, Interactive: true})
		d.events <- event.Event{Kind: event.Key, Payload: event.KeyPayload{Key: "rune", Rune: 'x'}}
		var diagnostics []input.Diagnostic
		r, _ := New(Options{Driver: d, Handle: func(context.Context, event.Event) error { panic("boom") }})
		r.opt.Diagnostics = func(d input.Diagnostic) { diagnostics = append(diagnostics, d) }
		if err := r.Run(context.Background()); !errors.Is(err, ErrRuntimePanic) {
			t.Fatalf("err=%v", err)
		}
		if d.restoreCount != 1 || d.closeCount != 1 {
			t.Fatalf("restore=%d close=%d", d.restoreCount, d.closeCount)
		}
		if len(diagnostics) == 0 || diagnostics[0].Code != "PANIC_RECOVERED" {
			t.Fatalf("diagnostics=%v", diagnostics)
		}
	})
}

func TestNonTTYSecretDoesNotPrompt(t *testing.T) {
	d := newFakeDriver(capability.Manifest{TTY: false, Interactive: false})
	r, _ := New(Options{Driver: d})
	if _, err := r.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadSecret(context.Background(), input.SecretPrompt{ID: "x"}); !errors.Is(err, ErrSecureInputDenied) {
		t.Fatalf("err=%v", err)
	}
}

func TestRuntimeClassifiesNonTTYAccessibleOutput(t *testing.T) {
	d := newFakeDriver(capability.Manifest{TTY: false, Interactive: false, OutputMode: capability.OutputAccessibleLine, ScreenReader: true})
	r, err := New(Options{Driver: d})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.Profile() != ProfileAccessibleLine {
		t.Fatalf("profile=%s", r.Profile())
	}
}

func TestQueueCoalescesResizeAndDropsOnlyWhenFull(t *testing.T) {
	var ds []input.Diagnostic
	q := newQueue(2, func(d input.Diagnostic) { ds = append(ds, d) })
	q.push(event.Event{Kind: event.Resize, Payload: event.ResizePayload{Width: 1, Height: 1}})
	q.push(event.Event{Kind: event.Resize, Payload: event.ResizePayload{Width: 2, Height: 2}})
	q.push(event.Event{Kind: event.Key, Payload: event.KeyPayload{Key: "rune", Rune: 'a'}})
	q.push(event.Event{Kind: event.Pointer, Payload: event.PointerPayload{SourceID: "mouse", X: 1, Y: 1}})
	if len(ds) != 1 {
		t.Fatalf("diagnostics=%v", ds)
	}
	ev, _ := q.pop()
	p := ev.Payload.(event.ResizePayload)
	if p.Width != 2 {
		t.Fatal("resize was not coalesced")
	}
	if next, _ := q.pop(); next.Kind != event.Key {
		t.Fatalf("next kind=%s", next.Kind)
	}
}

func TestQueueRejectsProtectedEventBeforeAdmission(t *testing.T) {
	q := newQueue(1, nil)
	if err := q.pushStrict(event.Event{Kind: event.Key, Payload: event.KeyPayload{Key: "enter"}}); err != nil {
		t.Fatal(err)
	}
	if err := q.pushStrict(event.Event{Kind: event.Key, Payload: event.KeyPayload{Key: "escape"}}); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("protected saturation error = %v", err)
	}
	first, ok := q.pop()
	if !ok || first.Payload.(event.KeyPayload).Key != "enter" {
		t.Fatalf("admitted protected event was displaced: %#v", first)
	}
}

func TestQueueCoalescesPointerBySource(t *testing.T) {
	q := newQueue(4, nil)
	q.push(event.Event{Kind: event.Pointer, Payload: event.PointerPayload{SourceID: "mouse", X: 1}})
	q.push(event.Event{Kind: event.Pointer, Payload: event.PointerPayload{SourceID: "other", X: 2}})
	q.push(event.Event{Kind: event.Pointer, Payload: event.PointerPayload{SourceID: "mouse", X: 3}})
	if len(q.items) != 2 {
		t.Fatalf("items=%d", len(q.items))
	}
	first, _ := q.pop()
	if first.Payload.(event.PointerPayload).X != 3 {
		t.Fatal("pointer was not coalesced")
	}
}

func TestQueueClonesUntrustedDriverEvent(t *testing.T) {
	q := newQueue(2, nil)
	p := event.PointerPayload{SourceID: "mouse", X: 1, Buttons: []string{"left"}}
	if !q.push(event.Event{Kind: event.Pointer, Payload: p}) {
		t.Fatal("push failed")
	}
	p.Buttons[0] = "right"
	ev, ok := q.pop()
	if !ok || ev.Payload.(event.PointerPayload).Buttons[0] != "left" {
		t.Fatalf("event was not cloned: %#v", ev)
	}
}

func TestNewEventCanonicalKey(t *testing.T) {
	ev := NewEvent(input.KeyChord{Code: input.KeyRune, Rune: 'A', Mods: input.ModCtrl})
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
	if ev.Payload.(event.KeyPayload).Key != "rune" {
		t.Fatal(ev.Payload)
	}
}

func TestRuntimeOpenFailureClosesAndReportsSafeDiagnostic(t *testing.T) {
	d := newFakeDriver(capability.Manifest{})
	d.openErr = errors.New("raw failure detail")
	var diagnostics []input.Diagnostic
	r, err := New(Options{Driver: d, Diagnostics: func(diag input.Diagnostic) { diagnostics = append(diagnostics, diag) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Open(context.Background()); !errors.Is(err, ErrOpenFailed) {
		t.Fatalf("err=%v", err)
	}
	if d.closeCount != 1 {
		t.Fatalf("close=%d", d.closeCount)
	}
	if len(diagnostics) == 0 || diagnostics[0].Code != "DRIVER_OPEN_FAILED" {
		t.Fatalf("diagnostics=%v", diagnostics)
	}
}

func TestRuntimeOpenIsSingleFlight(t *testing.T) {
	d := newFakeDriver(capability.Manifest{TTY: true, Interactive: true})
	d.openGate = make(chan struct{})
	r, _ := New(Options{Driver: d})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := r.Open(context.Background()); results <- err }()
	}
	deadline := time.After(time.Second)
	for {
		d.mu.Lock()
		count := d.openCount
		d.mu.Unlock()
		if count == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Open did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(d.openGate)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.openCount != 1 {
		t.Fatalf("driver Open calls=%d", d.openCount)
	}
}

type fakeDriver struct {
	events                   chan event.Event
	manifest                 capability.Manifest
	openErr                  error
	openGate                 chan struct{}
	openCount                int
	mu                       sync.Mutex
	restoreCount, closeCount int
}

func newFakeDriver(m capability.Manifest) *fakeDriver {
	return &fakeDriver{events: make(chan event.Event, 8), manifest: m}
}
func (d *fakeDriver) Open(context.Context, capability.Policy) (capability.Manifest, error) {
	d.mu.Lock()
	d.openCount++
	gate := d.openGate
	d.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if d.openErr != nil {
		return capability.Manifest{}, d.openErr
	}
	return d.manifest, nil
}
func (d *fakeDriver) Events() <-chan event.Event                                 { return d.events }
func (d *fakeDriver) Draw(context.Context, surface.Surface, surface.Patch) error { return nil }
func (d *fakeDriver) ReadSecret(context.Context, input.SecretPrompt) (secret.Handle, error) {
	return secret.Handle{ID: "opaque"}, nil
}
func (d *fakeDriver) Restore(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.restoreCount++
	return nil
}
func (d *fakeDriver) Close() error { d.mu.Lock(); defer d.mu.Unlock(); d.closeCount++; return nil }

var _ = time.Second
