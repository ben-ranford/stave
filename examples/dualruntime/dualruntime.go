// Package dualruntime is a compiled local-checkout tutorial for one Stave app
// exposed through a line-oriented human runtime and the JSONL agent runtime.
package dualruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/ben-ranford/stave"
	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/runtime/agent"
	"github.com/ben-ranford/stave/runtime/human"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/surface"
	"github.com/ben-ranford/stave/theme"
)

type Model struct{ Count int }

type Application struct {
	Prepared *stave.Prepared[Model]
	Registry *action.Registry
}

func New(ctx context.Context) (*Application, error) {
	registry := action.NewRegistry()
	definition := action.Definition{ID: "example.increment.v1", Version: "1", Title: "Increment", InputSchema: action.Schema{ID: "empty", JSON: []byte(`{}`)}, OutputSchema: action.Schema{ID: "empty", JSON: []byte(`{}`)}, Safety: action.Reversible, Idempotency: action.Idempotent}
	var prepared *stave.Prepared[Model]
	if err := registry.Register(definition, func(_ context.Context, call action.Call, _ any) (any, error) {
		ev, err := event.New(event.ActionInvoked, event.ActionInvokedPayload{CallID: call.CallID, ActionID: string(call.ActionID)})
		if err != nil {
			return nil, err
		}
		if err := prepared.Session.Send(ev); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}
	program := stave.Program[Model]{Initial: Model{}, Actions: registry, Reduce: reduce, View: view, Theme: tutorialTheme()}
	prepared, err := program.NewSession(ctx, stave.SessionOptions{SessionID: "dual-runtime", RuntimeDetected: capability.DetectEnv(map[string]string{"TERM": "dumb"}, false, 80, 24)})
	if err != nil {
		return nil, err
	}
	return &Application{Prepared: prepared, Registry: registry}, nil
}

func reduce(_ stave.ReduceContext, model Model, ev event.Event) (Model, []effect.Request, error) {
	if ev.Kind == event.ActionInvoked || (ev.Kind == event.Text && ev.Payload.(event.TextPayload).Text == "inc") {
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

// HumanOptions keeps application event routing explicit. Construct a
// human.LineDriver with its own input/output ownership and pass it here.
func (a *Application) HumanOptions(driver human.Driver) human.Options {
	return human.Options{Driver: driver, Handle: func(_ context.Context, ev event.Event) error { return a.Prepared.Session.Send(ev) }, Draw: func(ctx context.Context, _ event.Event) (surface.Surface, surface.Patch, error) {
		snapshot, err := a.Prepared.Session.Snapshot()
		if err != nil {
			return surface.Surface{}, surface.Patch{}, err
		}
		result, err := render.Render(render.Request{Context: ctx, Tree: snapshot.Tree, Theme: a.Prepared.Theme, Capabilities: a.Prepared.Capabilities, Viewport: layout.Size{Width: 80, Height: 24}})
		return result.Surface, result.Patch, err
	}}
}

// AgentOptions binds only snapshot and cancellation plumbing. Callers must
// still provide any authorization and confirmation callbacks they require.
func (a *Application) AgentOptions(options agent.Options) (agent.Options, error) {
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
