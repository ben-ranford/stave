// Package human contains the terminal-facing runtime boundary.  It deliberately
// keeps terminal ownership behind small interfaces so applications and tests can
// provide a PTY, a plain writer, or a deterministic simulator.
package human

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/focus"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/keymap"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/secret"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/surface"
)

type Profile string

const (
	ProfileFullScreen     Profile = "full-screen"
	ProfileLine           Profile = "line"
	ProfileAccessibleLine Profile = "accessible-line"
	ProfilePlain          Profile = "plain"
	ProfileNonTTY         Profile = "non-tty"
)

// Driver is the complete terminal ownership boundary. Drivers must make Restore
// and Close safe to call repeatedly.
type Driver interface {
	Open(context.Context, capability.Policy) (capability.Manifest, error)
	Events() <-chan event.Event
	Draw(context.Context, surface.Surface, surface.Patch) error
	ReadSecret(context.Context, input.SecretPrompt) (secret.Handle, error)
	Restore(context.Context) error
	Close() error
}

type Backend = Driver
type EventSource interface{ Events() <-chan event.Event }
type OutputSink interface {
	Draw(context.Context, surface.Surface, surface.Patch) error
}
type SecretProvider interface {
	ReadSecret(context.Context, input.SecretPrompt) (secret.Handle, error)
}

var (
	ErrNoDriver          = errors.New("human runtime driver is required")
	ErrNotInteractive    = errors.New("human input is unavailable in noninteractive mode")
	ErrSecureInputDenied = errors.New("human secure input is unavailable without negotiated secure input")
	ErrStopped           = errors.New("human runtime stopped")
	ErrEventSourceClosed = io.EOF
	ErrOpenFailed        = errors.New("human runtime open failed")
	ErrRuntimePanic      = errors.New("human runtime panic recovered")
	ErrBackpressure      = errors.New("human runtime protected event queue saturated")
	ErrInvalidEvent      = errors.New("human runtime driver emitted an invalid event")
)

type Options struct {
	Driver         Driver
	Policy         capability.Policy
	QueueCapacity  int
	RestoreTimeout time.Duration
	Draw           func(context.Context, event.Event) (surface.Surface, surface.Patch, error)
	Handle         func(context.Context, event.Event) error
	Diagnostics    func(input.Diagnostic)
	Keymap         keymap.Map
	FocusGraph     *focus.Graph
	FocusState     *focus.State
	OnResolution   func(context.Context, keymap.Resolution) error
}

type Runtime struct {
	driver     Driver
	opt        Options
	mu         sync.Mutex
	state      lifecycleState
	manifest   capability.Manifest
	profile    Profile
	openDone   chan struct{}
	dispatcher *keymap.Dispatcher
	previous   surface.Surface
}

type lifecycleState uint8

const (
	lifecycleNew lifecycleState = iota
	lifecycleOpening
	lifecycleOpen
	lifecycleRestoring
	lifecycleClosed
)

func New(opts Options) (*Runtime, error) {
	if opts.Driver == nil {
		return nil, ErrNoDriver
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = 256
	}
	if opts.RestoreTimeout <= 0 {
		opts.RestoreTimeout = 2 * time.Second
	}
	r := &Runtime{driver: opts.Driver, opt: opts, openDone: make(chan struct{})}
	if opts.Keymap.Profile() != "" {
		r.dispatcher = keymap.NewDispatcher(opts.Keymap)
	}
	return r, nil
}

func (r *Runtime) Manifest() capability.Manifest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.manifest.Clone()
}
func (r *Runtime) Profile() Profile { r.mu.Lock(); defer r.mu.Unlock(); return r.profile }

func profileFor(m capability.Manifest) Profile {
	if m.OutputMode == capability.OutputAccessibleLine {
		return ProfileAccessibleLine
	}
	if !m.TTY || !m.Interactive {
		return ProfileNonTTY
	}
	if m.OutputMode == capability.OutputPlain || m.OutputMode == capability.OutputDumb {
		return ProfilePlain
	}
	if m.AlternateScreen && m.CursorAddressing {
		return ProfileFullScreen
	}
	return ProfileLine
}

// Open negotiates capabilities and records the selected profile. It is safe to
// call once; subsequent calls return the original manifest.
func (r *Runtime) Open(ctx context.Context) (capability.Manifest, error) {
	for {
		r.mu.Lock()
		if r.state == lifecycleRestoring {
			r.mu.Unlock()
			return capability.Manifest{}, ErrOpenFailed
		}
		if r.state == lifecycleOpening {
			done := r.openDone
			r.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return capability.Manifest{}, ctx.Err()
			}
		}
		if r.state != lifecycleNew {
			m := r.manifest.Clone()
			r.mu.Unlock()
			return m, nil
		}
		r.state = lifecycleOpening
		r.openDone = make(chan struct{})
		r.mu.Unlock()
		m, err := r.driver.Open(ctx, r.opt.Policy)
		if err != nil {
			r.mu.Lock()
			r.state = lifecycleNew
			close(r.openDone)
			r.mu.Unlock()
			_ = r.driver.Close()
			r.diag("DRIVER_OPEN_FAILED", "human runtime open failed", map[string]string{"stage": "open"})
			return capability.Manifest{}, ErrOpenFailed
		}
		if !containsVersion(m.ProtocolVersions, protocol.Version) && len(m.ProtocolVersions) > 0 {
			r.mu.Lock()
			r.state = lifecycleRestoring
			r.mu.Unlock()
			restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.opt.RestoreTimeout)
			defer cancel()
			_ = r.driver.Restore(restoreCtx)
			_ = r.driver.Close()
			r.mu.Lock()
			r.state = lifecycleNew
			close(r.openDone)
			r.mu.Unlock()
			r.diag("CAPABILITY_PROTOCOL_MISMATCH", "human runtime capability negotiation rejected unsupported protocol version", map[string]string{"protocol": protocol.Version})
			return capability.Manifest{}, ErrOpenFailed
		}
		resolved, diagnostics := capability.Negotiation{
			RuntimeDetected: m,
			Application:     r.opt.Policy,
		}.Resolve()
		for _, diagnostic := range diagnostics {
			r.diag("CAPABILITY_"+strings.ToUpper(strings.ReplaceAll(diagnostic.Code, ".", "_")), diagnostic.Message, map[string]string{"field": diagnostic.Field})
		}
		r.mu.Lock()
		r.manifest = resolved.Clone()
		r.profile = profileFor(resolved)
		r.state = lifecycleOpen
		close(r.openDone)
		r.mu.Unlock()
		return resolved, nil
	}
}

// Run owns event delivery until cancellation, EOF, shutdown, or an unrecoverable
// handler/draw failure. Restore is attempted on every path, including panics.
func (r *Runtime) Run(ctx context.Context) (err error) {
	if _, err = r.Open(ctx); err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			r.diag("PANIC_RECOVERED", "human runtime panic recovered", map[string]string{"stage": "run"})
			err = ErrRuntimePanic
		}
		if restoreErr := r.restore(context.Background()); err == nil && restoreErr != nil {
			err = restoreErr
		}
		if closeErr := r.driver.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	queue := newQueue(r.opt.QueueCapacity, r.opt.Diagnostics)
	events := r.driver.Events()
	if events == nil {
		if err := queue.pushStrict(shutdownEvent()); err != nil {
			return err
		}
	}
	sourceClosed := false
	for {
		if events != nil && !sourceClosed {
			if err := queue.drain(ctx, events); err != nil {
				if errors.Is(err, io.EOF) {
					sourceClosed = true
					if pushErr := queue.pushStrict(shutdownEvent()); pushErr != nil {
						return pushErr
					}
				} else {
					return err
				}
			}
		}
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			ev, ok := queue.pop()
			if !ok {
				break
			}
			if e := r.routeEvent(ctx, ev); e != nil {
				return e
			}
			if r.opt.Handle != nil {
				if e := r.opt.Handle(ctx, ev); e != nil {
					return e
				}
			}
			if r.opt.Draw != nil {
				next, patch, e := r.opt.Draw(ctx, ev)
				if e != nil {
					return e
				}
				if e = r.driver.Draw(ctx, next, patch); e != nil {
					return e
				}
				r.previous = next
			}
			if ev.Kind == event.Shutdown {
				return nil
			}
		}
		if sourceClosed {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				sourceClosed = true
				if pushErr := queue.pushStrict(shutdownEvent()); pushErr != nil {
					return pushErr
				}
				continue
			}
			if pushErr := queue.pushStrict(ev); pushErr != nil {
				return pushErr
			}
		}
	}
}

// routeEvent provides the production human path for keymap/focus ownership.
// Applications can observe the typed resolution while the runtime owns focus
// movement for the built-in navigation commands.
func (r *Runtime) routeEvent(ctx context.Context, ev event.Event) error {
	if ev.Kind != event.Key || r.dispatcher == nil {
		return nil
	}
	p, ok := ev.Payload.(event.KeyPayload)
	if !ok {
		return nil
	}
	keyName := p.Key
	if p.Key == string(input.KeyRune) && p.Rune != 0 {
		keyName = string(p.Rune)
	}
	chord, err := input.ParseKey(strings.Join(append(append([]string{}, p.Modifiers...), keyName), "+"))
	if err != nil {
		return nil
	}
	var target semantic.Target
	if r.opt.FocusState != nil {
		target = r.opt.FocusState.Active
	}
	res, err := r.dispatcher.Feed(chord, focusScope(r.opt.FocusState), target, time.Time{})
	if err != nil {
		return err
	}
	if res.Status != keymap.StatusMatch {
		return nil
	}
	if r.opt.FocusGraph != nil && r.opt.FocusState != nil && res.Call != nil {
		id := string(res.Call.ActionID)
		var next focus.State
		var moved bool
		switch {
		case strings.HasSuffix(id, "focus.next.v1"):
			next, moved = r.opt.FocusGraph.Next(*r.opt.FocusState)
		case strings.HasSuffix(id, "focus.previous.v1"):
			next, moved = r.opt.FocusGraph.Previous(*r.opt.FocusState)
		case strings.HasSuffix(id, "focus.first.v1"):
			var t semantic.Target
			t, moved = r.opt.FocusGraph.First(r.opt.FocusState.Scope)
			next = *r.opt.FocusState
			next.Active = t
		case strings.HasSuffix(id, "focus.last.v1"):
			var t semantic.Target
			t, moved = r.opt.FocusGraph.Last(r.opt.FocusState.Scope)
			next = *r.opt.FocusState
			next.Active = t
		}
		if moved {
			*r.opt.FocusState = next
		}
	}
	if r.opt.OnResolution != nil {
		return r.opt.OnResolution(ctx, res)
	}
	return nil
}

func focusScope(state *focus.State) semantic.NodeID {
	if state == nil {
		return ""
	}
	return state.Scope
}

func (r *Runtime) restore(parent context.Context) error {
	r.mu.Lock()
	if r.state == lifecycleClosed || r.state == lifecycleNew {
		r.mu.Unlock()
		return nil
	}
	r.state = lifecycleRestoring
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, r.opt.RestoreTimeout)
	defer cancel()
	err := r.driver.Restore(ctx)
	r.mu.Lock()
	r.state = lifecycleClosed
	r.mu.Unlock()
	return err
}

func (r *Runtime) ReadSecret(ctx context.Context, prompt input.SecretPrompt) (secret.Handle, error) {
	r.mu.Lock()
	interactive := r.manifest.Interactive && r.manifest.TTY && r.manifest.SecureInput && r.state == lifecycleOpen
	r.mu.Unlock()
	if !interactive {
		if !r.Manifest().SecureInput {
			return secret.Handle{}, ErrSecureInputDenied
		}
		return secret.Handle{}, ErrNotInteractive
	}
	return r.driver.ReadSecret(ctx, prompt)
}

// RunFunc is convenient for applications that only need event handling.
func Run(ctx context.Context, opts Options) error {
	r, err := New(opts)
	if err != nil {
		return err
	}
	return r.Run(ctx)
}

type queue struct {
	mu          sync.Mutex
	capacity    int
	items       []event.Event
	diagnostics func(input.Diagnostic)
}

func newQueue(capacity int, d func(input.Diagnostic)) *queue {
	return &queue{capacity: capacity, diagnostics: d}
}
func (q *queue) push(ev event.Event) bool {
	return q.pushStrict(ev) == nil
}

func (q *queue) pushStrict(ev event.Event) error {
	clone, err := ev.Clone()
	if err != nil {
		if q.diagnostics != nil {
			q.diagnostics(input.Diagnostic{Code: "INVALID_DRIVER_EVENT", Message: "driver emitted an invalid event"})
		}
		return ErrInvalidEvent
	}
	ev = clone
	q.mu.Lock()
	if ev.Coalescible() {
		key := ev.CoalescingKey()
		for i := len(q.items) - 1; i >= 0; i-- {
			if q.items[i].CoalescingKey() == key {
				q.items[i] = ev
				q.items[i].Meta.Coalesced = true
				q.mu.Unlock()
				return nil
			}
		}
	}
	if len(q.items) >= q.capacity {
		diagnostic := q.diagnostics
		q.mu.Unlock()
		if diagnostic != nil {
			code := "BACKPRESSURE"
			if !ev.Coalescible() {
				code = "PROTECTED_EVENT_REJECTED"
			}
			diagnostic(input.Diagnostic{Code: code, Message: "human event queue saturated"})
		}
		if ev.Coalescible() {
			return nil
		}
		return ErrBackpressure
	}
	q.items = append(q.items, ev)
	q.mu.Unlock()
	return nil
}
func (q *queue) pop() (event.Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return event.Event{}, false
	}
	ev := q.items[0]
	copy(q.items, q.items[1:])
	q.items = q.items[:len(q.items)-1]
	return ev, true
}
func (q *queue) drain(ctx context.Context, in <-chan event.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ev, ok := <-in:
		if !ok {
			return io.EOF
		}
		return q.pushStrict(ev)
	}
}

// NewEvent converts a normalized key into the canonical event schema.
func NewEvent(chord input.KeyChord) event.Event {
	chord = chord.Normalize()
	ev, err := event.New(event.Key, event.KeyPayload{Key: string(chord.Code), Rune: chord.Rune, Modifiers: mods(chord.Mods)})
	if err != nil {
		panic(err)
	}
	return ev
}
func mods(m input.Mod) []string {
	out := []string{}
	if m&input.ModShift != 0 {
		out = append(out, "shift")
	}
	if m&input.ModCtrl != 0 {
		out = append(out, "ctrl")
	}
	if m&input.ModAlt != 0 {
		out = append(out, "alt")
	}
	if m&input.ModMeta != 0 {
		out = append(out, "meta")
	}
	return out
}

func (r *Runtime) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fmt.Sprintf("human runtime profile=%s state=%d", r.profile, r.state)
}

func (r *Runtime) diag(code, message string, fields map[string]string) {
	if r.opt.Diagnostics == nil {
		return
	}
	r.opt.Diagnostics(input.Diagnostic{Code: code, Message: message, Fields: fields})
}

func containsVersion(versions []string, want string) bool {
	for _, version := range versions {
		if version == want {
			return true
		}
	}
	return false
}

func shutdownEvent() event.Event {
	ev, err := event.New(event.Shutdown, nil)
	if err != nil {
		panic(err)
	}
	return ev
}
