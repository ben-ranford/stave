package bubbletea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	staveinput "github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/semantic"
)

const (
	KeyKind                  Kind = event.Key
	TextKind                 Kind = event.Text
	ResizeKind               Kind = event.Resize
	ActionInvokedKind        Kind = event.ActionInvoked
	EffectResultKind         Kind = event.EffectResult
	CancelKind               Kind = event.Cancel
	FocusKind                Kind = event.Focus
	BlurKind                 Kind = event.Blur
	KeyboardEnhancementsKind Kind = "bubbletea.keyboard_enhancements"
)

type Kind = event.Kind

type Event = event.Event

type EffectSpec = effect.Spec

type EffectCall = effect.Call

type EffectOutcome = effect.Outcome

type EffectHandler = effect.Port

type Viewport struct {
	Width  int
	Height int
}

type RuntimeState struct {
	Viewport             Viewport
	TTY                  bool
	Focused              bool
	Environment          map[string]string
	KeyboardEnhancements KeyboardEnhancements
}

type KeyboardEnhancements struct {
	Supported            bool
	Disambiguation       bool
	EventTypes           bool
	AlternateKeys        bool
	AllKeysAsEscapeCodes bool
	AssociatedText       bool
}

type KeyboardEnhancementsEvent struct {
	KeyboardEnhancements KeyboardEnhancements
}

type ActionEvent struct {
	Call   action.Call
	Result action.Result
}

type Features struct {
	WindowTitle          string
	AltScreen            bool
	ReportFocus          bool
	MouseMode            tea.MouseMode
	BracketedPaste       bool
	KeyboardEnhancements tea.KeyboardEnhancements
}

type Frame struct {
	Tree           semantic.Tree
	Render         render.Result
	Capabilities   capability.Manifest
	Features       Features
	ActionManifest []action.Definition
	Runtime        RuntimeState
}

type Hooks[M any] struct {
	OnMessage     func(tea.Msg)
	OnEvent       func(event.Event)
	OnFrame       func(Frame)
	OnEffectBatch func([]effect.Call, []effect.Outcome)
}

type App[M any] struct {
	Initial      M
	Reduce       func(context.Context, M, event.Event) (M, []effect.Request, error)
	Snapshot     func(context.Context, M) (semantic.Tree, error)
	Render       func(context.Context, semantic.Tree, capability.Manifest, Viewport) (render.Result, error)
	Capabilities func(context.Context, M, RuntimeState) capability.Manifest
	Features     func(context.Context, M, capability.Manifest, RuntimeState) Features
	Actions      func(context.Context, M, semantic.Tree) *action.Registry
	HandleEffect EffectHandler
	Hooks        Hooks[M]
}

type Options struct {
	SessionID string
	Runtime   RuntimeState
}

type Model[M any] struct {
	updateMu sync.Mutex
	mu       sync.RWMutex
	ctx      context.Context
	app      App[M]
	opt      Options
	state    M
	frame    Frame
	lastErr  error
	seq      uint64
}

type ActionMsg struct {
	Call action.Call
}

type effectBatchMsg struct {
	calls    []effect.Call
	outcomes []effect.Outcome
}

type actionResultMsg struct {
	call   action.Call
	result action.Result
}

type contextCancelledMsg struct {
	err error
}

func New[M any](ctx context.Context, app App[M], opt Options) (*Model[M], error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if app.Reduce == nil {
		return nil, errors.New("bubbletea adapter: Reduce is required")
	}
	if app.Snapshot == nil {
		return nil, errors.New("bubbletea adapter: Snapshot is required")
	}
	if opt.SessionID == "" {
		opt.SessionID = "bubbletea"
	}
	if opt.Runtime.Environment == nil {
		opt.Runtime.Environment = envMap(os.Environ())
	} else {
		opt.Runtime.Environment = cloneEnv(opt.Runtime.Environment)
	}
	m := &Model[M]{
		ctx:   ctx,
		app:   app,
		opt:   opt,
		state: app.Initial,
	}
	if err := m.refreshFrame(); err != nil {
		return nil, err
	}
	return m, nil
}

func NewProgram[M any](ctx context.Context, app App[M], opt Options, teaOptions ...tea.ProgramOption) (*tea.Program, *Model[M], error) {
	model, err := New(ctx, app, opt)
	if err != nil {
		return nil, nil, err
	}
	merged := append([]tea.ProgramOption(nil), teaOptions...)
	merged = append(merged, tea.WithContext(model.ctx))
	return tea.NewProgram(model, merged...), model, nil
}

func (m *Model[M]) Init() tea.Cmd {
	return waitForContextDone(m.ctx)
}

func (m *Model[M]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	if m.app.Hooks.OnMessage != nil {
		m.app.Hooks.OnMessage(msg)
	}

	switch msg := msg.(type) {
	case contextCancelledMsg:
		m.applyEvent(event.Event{Kind: CancelKind, Revision: m.currentRevision()})
		return m, tea.Quit
	case effectBatchMsg:
		if m.app.Hooks.OnEffectBatch != nil {
			m.app.Hooks.OnEffectBatch(msg.calls, msg.outcomes)
		}
		var cmds []tea.Cmd
		for _, outcome := range msg.outcomes {
			cmd := m.applyEvent(effectOutcomeEvent(outcome))
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)
	case ActionMsg:
		return m, m.invokeAction(msg.Call)
	case actionResultMsg:
		cmd := m.applyEvent(event.Event{
			Kind:     ActionInvokedKind,
			Revision: m.currentRevision(),
			Payload:  event.ActionInvokedPayload{CallID: msg.call.CallID, ActionID: string(msg.call.ActionID), Target: &event.Target{NodeID: string(msg.call.Target.NodeID), Generation: msg.call.Target.Generation, ObservedRevision: msg.call.Target.ObservedRevision}, Arguments: map[string]any{"call": msg.call.Arguments, "result": msg.result.Output, "status": string(msg.result.Status)}},
		})
		return m, cmd
	case tea.WindowSizeMsg:
		m.updateRuntime(func(runtime *RuntimeState) {
			runtime.Viewport = Viewport{Width: msg.Width, Height: msg.Height}
		})
		return m, m.applyEvent(event.Event{
			Kind:     ResizeKind,
			Revision: m.currentRevision(),
			Payload:  event.ResizePayload{Width: msg.Width, Height: msg.Height},
		})
	case tea.FocusMsg:
		m.updateRuntime(func(runtime *RuntimeState) { runtime.Focused = true })
		return m, m.applyEvent(event.Event{Kind: FocusKind, Revision: m.currentRevision()})
	case tea.BlurMsg:
		m.updateRuntime(func(runtime *RuntimeState) { runtime.Focused = false })
		return m, m.applyEvent(event.Event{Kind: BlurKind, Revision: m.currentRevision()})
	case tea.PasteMsg:
		text, _, err := staveinput.NormalizeText(msg.Content, staveinput.DefaultMaxPasteBytes)
		if err != nil {
			m.setError(errors.New("input rejected"))
			return m, tea.Quit
		}
		return m, m.applyEvent(event.Event{
			Kind:     TextKind,
			Revision: m.currentRevision(),
			Payload:  event.TextPayload{Text: text.Value, Committed: true},
		})
	case tea.KeyboardEnhancementsMsg:
		m.updateRuntime(func(runtime *RuntimeState) {
			runtime.KeyboardEnhancements = KeyboardEnhancements{
				Supported:            msg.Flags != 0,
				Disambiguation:       msg.SupportsKeyDisambiguation(),
				EventTypes:           msg.SupportsEventTypes(),
				AlternateKeys:        msg.SupportsAlternateKeys(),
				AllKeysAsEscapeCodes: msg.SupportsAllKeysAsEscapeCodes(),
				AssociatedText:       msg.SupportsAssociatedText(),
			}
		})
		if err := m.refreshFrame(); err != nil {
			m.setError(err)
			return m, tea.Quit
		}
		return m, nil
	case tea.MouseMsg:
		return m, m.applyEvent(event.Event{
			Kind:     event.Pointer,
			Revision: m.currentRevision(),
			Payload:  newPointerPayload(msg),
		})
	case tea.KeyPressMsg:
		return m, m.applyEvent(event.Event{
			Kind:     KeyKind,
			Revision: m.currentRevision(),
			Payload:  newKeyPayload(msg.Key()),
		})
	case tea.KeyReleaseMsg:
		return m, m.applyEvent(event.Event{
			Kind:     KeyKind,
			Revision: m.currentRevision(),
			Payload:  newKeyPayload(msg.Key()),
		})
	case tea.KeyMsg:
		return m, m.applyEvent(event.Event{
			Kind:     KeyKind,
			Revision: m.currentRevision(),
			Payload:  newKeyPayload(msg.Key()),
		})
	default:
		return m, nil
	}
}

func (m *Model[M]) View() tea.View {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastErr != nil {
		return tea.NewView("Stave adapter error")
	}

	content := m.frame.Render.Terminal
	if content == "" {
		content = m.frame.Render.Plain
	}

	v := tea.NewView(content)
	v.WindowTitle = m.frame.Features.WindowTitle
	v.AltScreen = m.frame.Features.AltScreen
	v.ReportFocus = m.frame.Features.ReportFocus
	v.MouseMode = m.frame.Features.MouseMode
	v.DisableBracketedPasteMode = !m.frame.Features.BracketedPaste
	v.KeyboardEnhancements = m.frame.Features.KeyboardEnhancements
	return v
}

func (m *Model[M]) State() M {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Model[M]) Frame() Frame {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.frame
}

func (m *Model[M]) Err() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastErr
}

func (m *Model[M]) applyEvent(ev event.Event) tea.Cmd {
	m.mu.Lock()
	m.seq++
	seq := m.seq
	state := m.state
	frame := m.frame
	runtime := cloneRuntimeState(m.opt.Runtime)
	m.mu.Unlock()

	if ev.SchemaVersion == "" {
		ev.SchemaVersion = event.SchemaVersion
	}
	ev.Sequence = seq
	if ev.Revision == 0 {
		ev.Revision = frame.Tree.Revision()
	}
	if err := ev.Validate(); err != nil {
		m.setError(err)
		return tea.Quit
	}
	if cloned, err := ev.Clone(); err == nil {
		ev = cloned
	} else {
		m.setError(err)
		return tea.Quit
	}
	if m.app.Hooks.OnEvent != nil {
		m.app.Hooks.OnEvent(ev)
	}
	reducerEvent := ev
	if p, ok := ev.Payload.(event.KeyPayload); ok && p.Key == string(staveinput.KeyRune) && p.Rune != 0 {
		p.Key = string(p.Rune)
		reducerEvent.Payload = p
	}
	if p, ok := ev.Payload.(event.ActionInvokedPayload); ok {
		if envelope, ok := p.Arguments.(map[string]any); ok {
			reducerEvent.Payload = ActionEvent{Call: action.Call{CallID: p.CallID, ActionID: action.ID(p.ActionID)}, Result: action.Result{CallID: p.CallID, ActionID: action.ID(p.ActionID), Status: action.ResultStatus(fmt.Sprint(envelope["status"])), Output: rawJSON(envelope["result"])}}
		}
	}
	next, requests, err := m.app.Reduce(m.ctx, state, reducerEvent)
	if err != nil {
		m.setError(err)
		return tea.Quit
	}
	nextFrame, err := m.buildFrame(next, runtime)
	if err != nil {
		m.setError(err)
		return tea.Quit
	}
	m.mu.Lock()
	m.state = next
	m.frame = nextFrame
	m.mu.Unlock()
	if m.app.Hooks.OnFrame != nil {
		m.app.Hooks.OnFrame(nextFrame)
	}
	if len(requests) == 0 {
		return nil
	}
	calls, err := effect.Bind(m.opt.SessionID, seq, ev.Revision, requests)
	if err != nil {
		m.setError(err)
		return tea.Quit
	}
	return m.runEffects(calls)
}

func (m *Model[M]) invokeAction(call action.Call) tea.Cmd {
	m.mu.RLock()
	state := m.state
	frame := m.frame
	seq := m.seq
	m.mu.RUnlock()
	registry := m.registry(state, frame.Tree)
	if registry == nil {
		return nil
	}
	if call.SessionID == "" {
		call.SessionID = m.opt.SessionID
	}
	if call.CallID == "" {
		call.CallID = fmt.Sprintf("tea-action-%d", seq+1)
	}
	return func() tea.Msg {
		return actionResultMsg{
			call:   call,
			result: registry.Invoke(m.ctx, call),
		}
	}
}

func (m *Model[M]) registry(state M, tree semantic.Tree) *action.Registry {
	if m.app.Actions == nil {
		return nil
	}
	return m.app.Actions(m.ctx, state, tree)
}

func (m *Model[M]) runEffects(calls []effect.Call) tea.Cmd {
	if len(calls) == 0 {
		return nil
	}
	if m.app.HandleEffect == nil {
		return func() tea.Msg {
			outcomes := make([]effect.Outcome, len(calls))
			for i, call := range calls {
				outcomes[i] = effect.Outcome{
					ID:              call.ID,
					Sequence:        call.Sequence,
					Revision:        call.Revision,
					Ordinal:         call.Ordinal,
					Lane:            call.Spec.Lane,
					Sensitive:       call.Spec.Sensitive,
					Status:          effect.StatusFailed,
					Error:           "bubbletea adapter: effect handler is not configured",
					CompletionIndex: uint32(i),
				}
			}
			return effectBatchMsg{calls: cloneEffectCalls(calls), outcomes: outcomes}
		}
	}
	return func() tea.Msg {
		return effectBatchMsg{
			calls:    cloneEffectCalls(calls),
			outcomes: executeEffects(m.ctx, calls, m.app.HandleEffect),
		}
	}
}

func (m *Model[M]) refreshFrame() error {
	m.mu.RLock()
	state := m.state
	runtime := cloneRuntimeState(m.opt.Runtime)
	m.mu.RUnlock()
	frame, err := m.buildFrame(state, runtime)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.frame = frame
	m.mu.Unlock()
	if m.app.Hooks.OnFrame != nil {
		m.app.Hooks.OnFrame(frame)
	}
	return nil
}

func (m *Model[M]) buildFrame(state M, runtime RuntimeState) (Frame, error) {
	tree, err := m.app.Snapshot(m.ctx, state)
	if err != nil {
		return Frame{}, err
	}
	manifest := detectCapabilities(runtime)
	if m.app.Capabilities != nil {
		manifest = m.app.Capabilities(m.ctx, state, runtime)
	}
	rf := m.app.Render
	if rf == nil {
		rf = defaultRender
	}
	result, err := rf(m.ctx, tree, manifest, runtime.Viewport)
	if err != nil {
		return Frame{}, err
	}
	features := defaultFeatures(manifest, runtime)
	if m.app.Features != nil {
		features = m.app.Features(m.ctx, state, manifest, runtime)
	}
	frame := Frame{
		Tree:         tree,
		Render:       result,
		Capabilities: manifest,
		Features:     features,
		Runtime:      runtime,
	}
	if registry := m.registry(state, tree); registry != nil {
		frame.ActionManifest = registry.Manifest()
	}
	return frame, nil
}

func (m *Model[M]) currentRevision() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.frame.Tree.Revision()
}

func (m *Model[M]) setError(err error) {
	m.mu.Lock()
	m.lastErr = err
	m.mu.Unlock()
}

func (m *Model[M]) updateRuntime(update func(*RuntimeState)) {
	m.mu.Lock()
	update(&m.opt.Runtime)
	m.mu.Unlock()
}

func cloneRuntimeState(in RuntimeState) RuntimeState {
	out := in
	out.Environment = cloneEnv(in.Environment)
	return out
}

func defaultRender(ctx context.Context, tree semantic.Tree, manifest capability.Manifest, viewport Viewport) (render.Result, error) {
	_ = ctx
	_ = manifest
	plain := treePlain(tree.Root())
	size := layout.Size{Width: viewport.Width, Height: viewport.Height}
	return render.Result{
		Viewport: size,
		Plain:    plain,
		Terminal: plain,
	}, nil
}

func defaultFeatures(manifest capability.Manifest, runtime RuntimeState) Features {
	features := Features{
		AltScreen:      manifest.AlternateScreen && manifest.Interactive && manifest.TTY,
		MouseMode:      tea.MouseModeNone,
		BracketedPaste: manifest.BracketedPaste,
	}
	if manifest.Mouse {
		features.MouseMode = tea.MouseModeCellMotion
	}
	if runtime.KeyboardEnhancements.EventTypes {
		features.KeyboardEnhancements.ReportEventTypes = true
	}
	return features
}

func detectCapabilities(runtime RuntimeState) capability.Manifest {
	probe := capability.Probe{
		Env:    cloneEnv(runtime.Environment),
		TTY:    runtime.TTY,
		Width:  runtime.Viewport.Width,
		Height: runtime.Viewport.Height,
	}
	manifest := capability.Detect(probe)
	manifest.Width = runtime.Viewport.Width
	manifest.Height = runtime.Viewport.Height
	manifest.Interactive = runtime.TTY
	if runtime.KeyboardEnhancements.Supported {
		manifest.KeyboardLevel = "enhanced"
	}
	return manifest
}

func newKeyPayload(key tea.Key) event.KeyPayload {
	chord, err := staveinput.ParseKey(key.Keystroke())
	if err == nil {
		ev := staveinput.KeyEvent(chord)
		return ev.Payload.(event.KeyPayload)
	}
	return event.KeyPayload{Key: key.Keystroke()}
}

func newPointerPayload(msg tea.MouseMsg) event.PointerPayload {
	mouse := msg.Mouse()
	phase := "move"
	switch msg.(type) {
	case tea.MouseClickMsg:
		phase = "press"
	case tea.MouseReleaseMsg:
		phase = "release"
	case tea.MouseWheelMsg:
		phase = "scroll"
	case tea.MouseMotionMsg:
		phase = "move"
	}
	payload := event.PointerPayload{SourceID: "bubbletea", X: mouse.X, Y: mouse.Y, Phase: phase}
	if button := mouseButtonName(mouse.Button); button != "" {
		payload.Buttons = []string{button}
	}
	return payload
}

func mouseButtonName(button tea.MouseButton) string {
	switch button {
	case tea.MouseLeft:
		return "left"
	case tea.MouseMiddle:
		return "middle"
	case tea.MouseRight:
		return "right"
	case tea.MouseWheelUp:
		return "wheel-up"
	case tea.MouseWheelDown:
		return "wheel-down"
	case tea.MouseWheelLeft:
		return "wheel-left"
	case tea.MouseWheelRight:
		return "wheel-right"
	case tea.MouseBackward:
		return "backward"
	case tea.MouseForward:
		return "forward"
	default:
		return ""
	}
}

func rawJSON(v any) json.RawMessage {
	switch x := v.(type) {
	case json.RawMessage:
		return x
	case string:
		return json.RawMessage(x)
	default:
		b, _ := json.Marshal(x)
		return b
	}
}

func effectOutcomeEvent(outcome effect.Outcome) event.Event {
	return event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          EffectResultKind,
		Sequence:      outcome.Sequence,
		Revision:      outcome.Revision,
		Payload: event.EffectResultPayload{
			CallID:    outcome.ID,
			Ordinal:   outcome.Ordinal,
			Lane:      outcome.Lane,
			Status:    string(outcome.Status),
			Value:     outcome.Value,
			Error:     outcome.Error,
			Sensitive: outcome.Sensitive,
		},
		Meta: event.Metadata{
			Lane:            outcome.Lane,
			CompletionIndex: outcome.CompletionIndex,
		},
	}
}

func executeEffects(ctx context.Context, calls []effect.Call, handler EffectHandler) []effect.Outcome {
	ports := make(map[string]effect.Port)
	for _, call := range calls {
		ports[call.Spec.Kind] = handler
	}
	executor := effect.NewExecutor(effect.Options{Ports: ports, Parallelism: 4, Delivery: effect.DeclarationOrder, MaxBatch: 256, MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20})
	out := make([]effect.Outcome, 0, len(calls))
	var outMu sync.Mutex
	done := make(chan struct{})
	if err := executor.Deliver(ctx, calls, func(ev event.Event) error {
		payload, ok := ev.Payload.(event.EffectResultPayload)
		if !ok {
			return errors.New("invalid effect result")
		}
		outMu.Lock()
		out = append(out, effect.Outcome{ID: payload.CallID, Ordinal: payload.Ordinal, Lane: payload.Lane, Status: effect.Status(payload.Status), Value: payload.Value, Error: payload.Error, Sensitive: payload.Sensitive, CompletionIndex: ev.Meta.CompletionIndex})
		complete := len(out) == len(calls)
		outMu.Unlock()
		if complete {
			close(done)
		}
		return nil
	}); err != nil {
		return []effect.Outcome{{Status: effect.StatusFailed, Error: "effect request rejected"}}
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
	outMu.Lock()
	result := append([]effect.Outcome(nil), out...)
	outMu.Unlock()
	return result
}

func cloneEffectCalls(in []effect.Call) []effect.Call {
	out := make([]effect.Call, len(in))
	copy(out, in)
	return out
}

func waitForContextDone(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		<-ctx.Done()
		return contextCancelledMsg{err: ctx.Err()}
	}
}

func envMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, entry := range values {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func cloneEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func treePlain(root semantic.Node) string {
	lines := make([]string, 0, 8)
	var walk func(semantic.Node, int)
	walk = func(node semantic.Node, depth int) {
		lines = append(lines, strings.Repeat("  ", depth)+nodeLine(node))
		for _, child := range node.Children() {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return strings.Join(lines, "\n")
}

func nodeLine(node semantic.Node) string {
	line := node.Name()
	if line == "" {
		line = string(node.Role())
	}
	if value := node.Value(); value.HasValue && !value.Redacted && value.Text != "" {
		line += ": " + value.Text
	}
	if states := node.States(); len(states) > 0 {
		items := make([]string, len(states))
		for i, state := range states {
			items[i] = string(state)
		}
		line += " [" + strings.Join(items, ", ") + "]"
	}
	return line
}

func (f Frame) String() string {
	return fmt.Sprintf("tree=%s rev=%d plain=%q", f.Tree.Hash(), f.Tree.Revision(), f.Render.Plain)
}
