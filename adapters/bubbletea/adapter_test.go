package bubbletea

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/testfixture"
)

type fixtureState struct {
	Width        int
	Height       int
	Focused      bool
	Pasted       string
	LastKey      string
	Events       []string
	EffectValues []string
	ActionValues []string
	Count        int
}

func makeTree(t *testing.T, state fixtureState) semantic.Tree {
	t.Helper()
	button, err := primitive.Button(primitive.Options{
		Namespace: "fixture",
		View:      "main",
		Entity:    "primary",
		Slot:      "content",
		Name:      "Primary",
	}, "Primary")
	if err != nil {
		t.Fatal(err)
	}
	root, err := primitive.Section(primitive.Options{
		Namespace: "fixture",
		View:      "main",
		Entity:    "root",
		Slot:      "content",
		Name:      "Fixture",
	}, button)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(uint64(state.Count+1), root)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func makeApp(t *testing.T, hooks Hooks[fixtureState], effectHandler EffectHandler) App[fixtureState] {
	t.Helper()

	registry := action.NewRegistry()
	err := registry.Register(action.Definition{
		ID:      "activate",
		Version: "v1",
		Title:   "Activate",
		InputSchema: action.Schema{
			ID:   "fixture.activate.input",
			JSON: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		},
		OutputSchema: action.Schema{
			ID:   "fixture.activate.output",
			JSON: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"ok":{"type":"boolean"}},"required":["ok"]}`),
		},
		Safety: action.Reversible,
	}, func(ctx context.Context, call action.Call, input any) (any, error) {
		return json.RawMessage(`{"ok":true}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return App[fixtureState]{
		Initial: fixtureState{},
		Reduce: func(ctx context.Context, state fixtureState, ev event.Event) (fixtureState, []effect.Request, error) {
			state.Count++
			state.Events = append(state.Events, string(ev.Kind))
			switch ev.Kind {
			case ResizeKind:
				msg := ev.Payload.(event.ResizePayload)
				state.Width = msg.Width
				state.Height = msg.Height
			case TextKind:
				state.Pasted = ev.Payload.(event.TextPayload).Text
			case KeyKind:
				state.LastKey = ev.Payload.(event.KeyPayload).Key
				if state.LastKey == "enter" {
					return state, []effect.Request{{
						Spec:  effect.Spec{Kind: "work", Cancellable: true},
						Input: state.LastKey,
					}}, nil
				}
			case EffectResultKind:
				result := ev.Payload.(event.EffectResultPayload)
				if result.Error == "" {
					state.EffectValues = append(state.EffectValues, result.Value.(string))
				} else {
					state.EffectValues = append(state.EffectValues, result.Error)
				}
			case ActionInvokedKind:
				result := ev.Payload.(ActionEvent)
				state.ActionValues = append(state.ActionValues, string(result.Result.Status))
			case FocusKind:
				state.Focused = true
			case BlurKind:
				state.Focused = false
			case CancelKind:
				state.Events = append(state.Events, "cancelled")
			}
			return state, nil, nil
		},
		Snapshot: func(ctx context.Context, state fixtureState) (semantic.Tree, error) {
			return makeTree(t, state), nil
		},
		Actions: func(ctx context.Context, state fixtureState, tree semantic.Tree) *action.Registry {
			return registry
		},
		HandleEffect: effectHandler,
		Hooks:        hooks,
	}
}

func TestModelImplementsTeaModel(t *testing.T) {
	var _ tea.Model = (*Model[fixtureState])(nil)
}

func TestConformanceHooksAndMessageMapping(t *testing.T) {
	var gotEvents []event.Event
	var gotFrames []Frame
	model, err := New(context.Background(), makeApp(t, Hooks[fixtureState]{
		OnEvent: func(ev event.Event) { gotEvents = append(gotEvents, ev) },
		OnFrame: func(frame Frame) { gotFrames = append(gotFrames, frame) },
	}, nil), Options{
		SessionID: "fixture",
		Runtime: RuntimeState{
			TTY:         true,
			Environment: map[string]string{"TERM": "xterm-256color"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(gotFrames) != 1 {
		t.Fatalf("initial frame hooks = %d", len(gotFrames))
	}

	_, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	_, _ = model.Update(tea.FocusMsg{})
	_, _ = model.Update(tea.BlurMsg{})
	_, _ = model.Update(tea.PasteMsg{Content: "hello"})
	_, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))

	if len(gotEvents) != 5 {
		t.Fatalf("got %d events", len(gotEvents))
	}
	if gotEvents[0].Kind != ResizeKind {
		t.Fatalf("first event = %s", gotEvents[0].Kind)
	}
	if gotEvents[1].Kind != FocusKind {
		t.Fatalf("second event = %s", gotEvents[1].Kind)
	}
	if gotEvents[2].Kind != BlurKind {
		t.Fatalf("third event = %s", gotEvents[2].Kind)
	}
	if gotEvents[3].Kind != TextKind {
		t.Fatalf("fourth event = %s", gotEvents[3].Kind)
	}
	if gotEvents[4].Kind != KeyKind {
		t.Fatalf("fifth event = %s", gotEvents[4].Kind)
	}

	state := model.State()
	if state.Width != 120 || state.Height != 40 || state.Focused || state.Pasted != "hello" || state.LastKey != "a" {
		t.Fatalf("mapped state = %+v", state)
	}

	view := model.View()
	if !view.AltScreen {
		t.Fatal("expected alt screen declaration from manifest")
	}
	if view.MouseMode == tea.MouseModeNone {
		t.Fatal("expected mouse declaration from manifest")
	}
}

func TestMouseMessagesBecomeCanonicalPointerEvents(t *testing.T) {
	var got event.Event
	model, err := New(context.Background(), makeApp(t, Hooks[fixtureState]{
		OnEvent: func(ev event.Event) { got = ev },
	}, nil), Options{Runtime: RuntimeState{TTY: true, Environment: map[string]string{"TERM": "xterm-256color"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = model.Update(tea.MouseClickMsg{X: 7, Y: 3, Button: tea.MouseLeft})
	if err := got.Validate(); err != nil {
		t.Fatalf("translated pointer event is invalid: %v", err)
	}
	payload, ok := got.Payload.(event.PointerPayload)
	if got.Kind != event.Pointer || !ok || payload.SourceID != "bubbletea" || payload.X != 7 || payload.Y != 3 || payload.Phase != "press" || len(payload.Buttons) != 1 || payload.Buttons[0] != "left" {
		t.Fatalf("unexpected pointer translation: %#v", got)
	}
}

func TestEffectResultsStayInDeclarationOrder(t *testing.T) {
	app := makeApp(t, Hooks[fixtureState]{}, effect.PortFunc(func(ctx context.Context, call effect.Call) (any, error) {
		switch call.Ordinal {
		case 0:
			time.Sleep(20 * time.Millisecond)
			return "slow", nil
		default:
			return "fast", nil
		}
	}))
	app.Reduce = func(ctx context.Context, state fixtureState, ev event.Event) (fixtureState, []effect.Request, error) {
		state.Events = append(state.Events, string(ev.Kind))
		switch ev.Kind {
		case KeyKind:
			return state, []effect.Request{
				{Spec: effect.Spec{Kind: "first"}, Input: "first"},
				{Spec: effect.Spec{Kind: "second"}, Input: "second"},
			}, nil
		case EffectResultKind:
			result := ev.Payload.(event.EffectResultPayload)
			state.EffectValues = append(state.EffectValues, result.Value.(string))
			return state, nil, nil
		default:
			return state, nil, nil
		}
	}

	model, err := New(context.Background(), app, Options{})
	if err != nil {
		t.Fatal(err)
	}

	_, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("expected effect command")
	}
	msg := cmd()
	_, _ = model.Update(msg)

	if got := model.State().EffectValues; len(got) != 2 || got[0] != "slow" || got[1] != "fast" {
		t.Fatalf("effect order = %#v", got)
	}
}

func TestOutcomeTranslationUsesRootEffectPayload(t *testing.T) {
	ev := effect.Outcome{
		ID:              "fx1_1234",
		Sequence:        7,
		Revision:        9,
		Ordinal:         2,
		Lane:            "io",
		Status:          effect.StatusCompleted,
		Value:           "ok",
		Sensitive:       true,
		CompletionIndex: 4,
	}.Event()

	payload, ok := ev.Payload.(event.EffectResultPayload)
	if !ok {
		t.Fatalf("payload type = %T", ev.Payload)
	}
	if ev.Kind != EffectResultKind || ev.Sequence != 0 || ev.Revision != 9 {
		t.Fatalf("event envelope = %+v", ev)
	}
	if payload.CallID != "fx1_1234" || payload.Ordinal != 2 || payload.Status != string(effect.StatusCompleted) || payload.Value != "ok" || !payload.Sensitive {
		t.Fatalf("payload = %+v", payload)
	}
	if ev.Meta.CompletionIndex != 4 || ev.Meta.Lane != "io" {
		t.Fatalf("meta = %+v", ev.Meta)
	}
}

func TestDeterministicFramesAcrossEquivalentInputs(t *testing.T) {
	app := makeApp(t, Hooks[fixtureState]{}, nil)
	one, err := New(context.Background(), app, Options{Runtime: RuntimeState{TTY: true, Environment: map[string]string{"TERM": "xterm-256color"}}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := New(context.Background(), app, Options{Runtime: RuntimeState{TTY: true, Environment: map[string]string{"TERM": "xterm-256color"}}})
	if err != nil {
		t.Fatal(err)
	}

	inputs := []tea.Msg{
		tea.WindowSizeMsg{Width: 80, Height: 24},
		tea.FocusMsg{},
		tea.PasteMsg{Content: "x"},
		tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}),
	}
	for _, input := range inputs {
		_, cmd := one.Update(input)
		if cmd != nil {
			_, _ = one.Update(cmd())
		}
		_, cmd = two.Update(input)
		if cmd != nil {
			_, _ = two.Update(cmd())
		}
	}

	frameOne := one.Frame()
	frameTwo := two.Frame()
	if frameOne.Tree.Hash() != frameTwo.Tree.Hash() || frameOne.Render.Plain != frameTwo.Render.Plain {
		t.Fatalf("frames diverged:\n%v\n%v", frameOne, frameTwo)
	}
}

func TestActionDispatchUsesRegistryAsAuthority(t *testing.T) {
	model, err := New(context.Background(), makeApp(t, Hooks[fixtureState]{}, nil), Options{})
	if err != nil {
		t.Fatal(err)
	}

	_, cmd := model.Update(ActionMsg{
		Call: action.Call{
			ActionID:  "activate",
			Arguments: json.RawMessage(`{}`),
		},
	})
	if cmd == nil {
		t.Fatal("expected action command")
	}
	_, _ = model.Update(cmd())

	if got := model.State().ActionValues; len(got) != 1 || got[0] != "ok" {
		t.Fatalf("action results = %#v", got)
	}
	if len(model.Frame().ActionManifest) != 1 {
		t.Fatalf("action manifest = %#v", model.Frame().ActionManifest)
	}
}

func TestProgramCancellationCancelsRunningEffects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var started atomic.Bool
	var cancelled atomic.Bool
	app := makeApp(t, Hooks[fixtureState]{}, effect.PortFunc(func(ctx context.Context, call effect.Call) (any, error) {
		started.Store(true)
		<-ctx.Done()
		cancelled.Store(true)
		return nil, ctx.Err()
	}))

	program, _, err := NewProgram(ctx, app, Options{}, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignals())
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, runErr := program.Run()
		done <- runErr
	}()

	program.Send(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	deadline := time.Now().Add(2 * time.Second)
	for !started.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !started.Load() {
		t.Fatal("effect did not start")
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("run err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not exit after cancellation")
	}

	if !cancelled.Load() {
		t.Fatal("effect did not observe context cancellation")
	}
}

func TestConcurrentProgramSendDoesNotRaceModelState(t *testing.T) {
	app := makeApp(t, Hooks[fixtureState]{}, nil)
	program, model, err := NewProgram(context.Background(), app, Options{}, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignals())
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, runErr := program.Run()
		done <- runErr
	}()
	defer func() {
		program.Quit()
		<-done
	}()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			program.Send(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
		}()
	}
	wg.Wait()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if model.State().Count >= 8 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("count = %d", model.State().Count)
}

func TestApplicationCallbacksCanReadModelWithoutDeadlock(t *testing.T) {
	var model *Model[fixtureState]
	app := makeApp(t, Hooks[fixtureState]{}, nil)
	reduce := app.Reduce
	app.Reduce = func(ctx context.Context, state fixtureState, ev event.Event) (fixtureState, []effect.Request, error) {
		_ = model.State()
		_ = model.Frame()
		_ = model.Err()
		return reduce(ctx, state, ev)
	}
	app.Hooks.OnEvent = func(event.Event) {
		_ = model.State()
		_ = model.Frame()
		_ = model.Err()
	}
	app.Hooks.OnFrame = func(Frame) {
		if model == nil {
			return
		}
		_ = model.State()
		_ = model.Frame()
		_ = model.Err()
	}

	var err error
	model, err = New(context.Background(), app, Options{})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = model.Update(tea.FocusMsg{})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("application callback deadlocked while reading model")
	}
}

func TestViewDoesNotExposeApplicationErrorText(t *testing.T) {
	app := makeApp(t, Hooks[fixtureState]{}, nil)
	app.Reduce = func(context.Context, fixtureState, event.Event) (fixtureState, []effect.Request, error) {
		return fixtureState{}, nil, errors.New("secret terminal payload \x1b[31m")
	}
	model, err := New(context.Background(), app, Options{})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = model.Update(tea.FocusMsg{})
	if model.Err() == nil {
		t.Fatal("expected the application error to remain available through Err")
	}
	if got := model.View().Content; got != "Stave adapter error" {
		t.Fatalf("terminal-visible error = %q", got)
	}
}

func TestSharedAdapterSurfaceFixture(t *testing.T) {
	shared, err := testfixture.Surface()
	if err != nil {
		t.Fatal(err)
	}
	app := makeApp(t, Hooks[fixtureState]{}, nil)
	app.Render = func(context.Context, semantic.Tree, capability.Manifest, Viewport) (render.Result, error) {
		return render.Result{Surface: shared, Plain: shared.String(), Terminal: shared.String()}, nil
	}
	model, err := New(context.Background(), app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Frame().Render.Surface.Hash(); got != shared.Hash() {
		t.Fatalf("shared surface hash changed: %x != %x", got, shared.Hash())
	}
	if got := model.View().Content; got != shared.String() {
		t.Fatalf("shared surface output = %q", got)
	}
}
