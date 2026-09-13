// Package dualruntime is a compiled local-checkout tutorial for one Stave app
// exposed through a line-oriented human runtime and the JSONL agent runtime.
package dualruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ben-ranford/stave"
	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/runtime/agent"
	"github.com/ben-ranford/stave/runtime/human"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
	"github.com/ben-ranford/stave/surface"
	"github.com/ben-ranford/stave/theme"
)

type Model struct{ Count int }

const IncrementActionID action.ID = "example.increment.v1"

type incrementInput struct{}
type incrementOutput struct{}

type Application struct {
	Prepared           *stave.Prepared[Model]
	Registry           *action.Registry
	incrementGate      chan struct{}
	nextIncrementCount int
}

func New(ctx context.Context, runtimeDetected capability.Manifest) (*Application, error) {
	registry := action.NewRegistry()
	definition := action.Definition{
		ID: IncrementActionID, Version: "1", Title: "Increment",
		InputSchema: action.Schema{ID: "empty", JSON: []byte(`{}`)}, OutputSchema: action.Schema{ID: "empty", JSON: []byte(`{}`)},
		Safety: action.Reversible, Idempotency: action.NonIdempotent,
	}
	incrementGate := make(chan struct{}, 1)
	incrementGate <- struct{}{}
	application := &Application{Registry: registry, incrementGate: incrementGate}
	if err := action.Register(registry, definition, decodeIncrement, encodeIncrement, func(ctx context.Context, call action.Call, _ incrementInput) (incrementOutput, error) {
		return application.increment(ctx, call)
	}); err != nil {
		return nil, err
	}
	program := stave.Program[Model]{Initial: Model{}, Actions: registry, Reduce: reduce, View: view, Theme: tutorialTheme()}
	prepared, err := program.NewSession(ctx, stave.SessionOptions{SessionID: "dual-runtime", RuntimeDetected: runtimeDetected})
	if err != nil {
		return nil, err
	}
	application.Prepared = prepared
	return application, nil
}

func (a *Application) increment(ctx context.Context, call action.Call) (incrementOutput, error) {
	select {
	case <-ctx.Done():
		return incrementOutput{}, ctx.Err()
	case <-a.incrementGate:
	}
	defer func() { a.incrementGate <- struct{}{} }()
	ev, err := event.New(event.ActionInvoked, event.ActionInvokedPayload{CallID: call.CallID, ActionID: string(call.ActionID)})
	if err != nil {
		return incrementOutput{}, err
	}
	if err := a.Prepared.Session.Send(ev); err != nil {
		return incrementOutput{}, err
	}
	a.nextIncrementCount++
	target := a.nextIncrementCount
	if err := a.Prepared.Session.Wait(ctx, func(current state.State[Model]) bool { return current.Model.Count >= target }); err != nil {
		return incrementOutput{}, err
	}
	return incrementOutput{}, nil
}

func decodeIncrement(raw json.RawMessage) (incrementInput, error) {
	var input incrementInput
	return input, json.Unmarshal(raw, &input)
}

func encodeIncrement(output incrementOutput) (json.RawMessage, error) { return json.Marshal(output) }

// AgentManifest is the complete capability profile selected by the JSONL host.
func AgentManifest() capability.Manifest {
	return capability.Manifest{ProtocolVersions: []string{protocol.Version}, OutputMode: capability.OutputMachineJSONL, SnapshotModes: []string{"full"}}
}

func reduce(_ stave.ReduceContext, model Model, ev event.Event) (Model, []effect.Request, error) {
	if ev.Kind != event.ActionInvoked {
		return model, nil, nil
	}
	payload, ok := ev.Payload.(event.ActionInvokedPayload)
	if ok && payload.ActionID == string(IncrementActionID) {
		model.Count++
	}
	return model, nil, nil
}

func view(ctx stave.ViewContext, model Model) (semantic.Tree, error) {
	node, err := primitive.Text(primitive.Options{Namespace: "example", View: "counter", Entity: "count", Name: "Count"}, fmt.Sprintf("count: %d", model.Count))
	if err != nil {
		return semantic.Tree{}, err
	}
	return semantic.NewTree(max(1, ctx.Revision), node)
}

// HumanOptions keeps application event routing explicit. The inc command is
// dispatched through the same registry and authorization policy as JSONL.
func (a *Application) HumanOptions(driver human.Driver) human.Options {
	return human.Options{Driver: driver, Handle: a.handleHumanEvent, Draw: func(ctx context.Context, ev event.Event) (surface.Surface, surface.Patch, error) {
		if ev.Kind == event.Shutdown {
			return surface.New(0, 0), surface.Patch{}, nil
		}
		snapshot, err := a.Prepared.Session.Snapshot()
		if err != nil {
			return surface.Surface{}, surface.Patch{}, err
		}
		result, err := render.Render(render.Request{Context: ctx, Tree: snapshot.Tree, Theme: a.Prepared.Theme, Capabilities: a.Prepared.Capabilities, Viewport: layout.Size{Width: 80, Height: 24}})
		return result.Surface, result.Patch, err
	}}
}

func (a *Application) handleHumanEvent(ctx context.Context, ev event.Event) error {
	before, err := a.Prepared.Session.Snapshot()
	if err != nil {
		return err
	}
	if ev.Kind == event.Text {
		if payload, ok := ev.Payload.(event.TextPayload); ok && payload.Text == "inc" {
			if result := a.invoke(ctx, action.Call{CallID: fmt.Sprintf("human-%d", before.Sequence+1), ActionID: IncrementActionID, Arguments: json.RawMessage(`{}`), SessionID: before.SessionID}); result.Error != nil {
				return result.Error
			}
			return nil
		} else if err := a.Prepared.Session.Send(ev); err != nil {
			return err
		}
	} else if err := a.Prepared.Session.Send(ev); err != nil {
		return err
	}
	return a.Prepared.Session.Wait(ctx, func(current state.State[Model]) bool { return current.Sequence > before.Sequence })
}

func (a *Application) invoke(ctx context.Context, call action.Call) action.Result {
	if err := a.Authorize(ctx, call); err != nil {
		return action.Result{CallID: call.CallID, ActionID: call.ActionID, Status: action.ResultRejected, Error: err}
	}
	return a.Registry.Invoke(ctx, call)
}

// Authorize is the tutorial application's action policy shared by both hosts.
func (a *Application) Authorize(_ context.Context, call action.Call) *action.Error {
	if call.ActionID != IncrementActionID {
		return &action.Error{Code: action.Forbidden, Message: "action is not allowed", ActionID: call.ActionID}
	}
	return nil
}

// AgentOptions binds the session while retaining the tutorial's registered
// action and authorization policy. It projects validated agent limits and
// returns the manifest already bound to the session.
func (a *Application) AgentOptions(options agent.Options) (agent.Options, error) {
	projected, err := agent.OptionsFromConfig(a.Prepared.Config)
	if err != nil {
		return agent.Options{}, err
	}
	options.MaxMessageBytes = projected.MaxMessageBytes
	options.MaxTreeNodes = projected.MaxTreeNodes
	options.Actions = a.Registry
	options.Authorize = a.Authorize
	options.Negotiate = func(context.Context, map[string]any) (capability.Manifest, error) {
		return a.Prepared.Capabilities.Clone(), nil
	}
	return agent.BindSession(a.Prepared.Session, options)
}

func (a *Application) Close() { a.Prepared.Session.Close() }

func tutorialTheme() theme.Theme {
	tokens := theme.TokenSet{}
	for _, role := range theme.RequiredRoleIDs() {
		name := string(role)
		switch {
		case strings.HasPrefix(name, "motion.duration"):
			tokens[role] = theme.Value{Kind: theme.KindDuration, Literal: "120ms"}
		case strings.HasPrefix(name, "motion.easing"), strings.HasPrefix(name, "type."):
			tokens[role] = theme.Value{Kind: theme.KindString, Literal: "terminal"}
		case strings.HasPrefix(name, "space."), strings.HasPrefix(name, "radius."), strings.HasPrefix(name, "elevation."):
			tokens[role] = theme.Value{Kind: theme.KindNumber, Literal: 2}
		default:
			tokens[role] = theme.Value{Kind: theme.KindColor, Literal: "#f2f6f8"}
		}
	}
	return theme.Theme{ID: "dual-runtime", Version: "1", Modes: map[theme.Mode]theme.TokenSet{theme.ModeAuto: tokens}, Densities: map[theme.Density]theme.TokenSet{theme.DensityComfortable: {}}, Glyphs: map[string]theme.GlyphSet{"render": {"truncation": {Unicode: "…", ASCII: "...", Width: 3}}, "status": {"success": {Unicode: "✓", ASCII: "v", Width: 1}}}, Assets: map[string]theme.AssetRef{"brand.mark": {ID: "mark", Text: "DUAL"}, "brand.mark.ascii": {ID: "mark-ascii", Text: "D"}, "brand.banner.terminal": {ID: "banner", Text: "DUAL RUNTIME"}}}
}
