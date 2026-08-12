package stave

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/config"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/observer"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/session"
	"github.com/ben-ranford/stave/state"
	"github.com/ben-ranford/stave/theme"
)

// ReduceContext is the immutable framework context for one reducer call.
type ReduceContext struct {
	Context   context.Context
	SessionID string
	Sequence  uint64
	Revision  uint64
}

func (c ReduceContext) Deadline() (deadline time.Time, ok bool) { return c.Context.Deadline() }
func (c ReduceContext) Done() <-chan struct{}                   { return c.Context.Done() }
func (c ReduceContext) Err() error                              { return c.Context.Err() }
func (c ReduceContext) Value(key any) any                       { return c.Context.Value(key) }

// ViewContext describes the resolved, immutable environment for a view call.
type ViewContext struct {
	Context      context.Context
	SessionID    string
	Revision     uint64
	Capabilities capability.Manifest
	Theme        theme.Resolved
	Viewport     layout.Size
}

func (c ViewContext) Deadline() (deadline time.Time, ok bool) { return c.Context.Deadline() }
func (c ViewContext) Done() <-chan struct{}                   { return c.Context.Done() }
func (c ViewContext) Err() error                              { return c.Context.Err() }
func (c ViewContext) Value(key any) any                       { return c.Context.Value(key) }

type Reducer[M any] func(ReduceContext, M, event.Event) (M, []effect.Request, error)
type View[M any] func(ViewContext, M) (semantic.Tree, error)

// Program is the production composition root. It owns no adapter-specific
// dependencies: human, agent, SSH, and optional UI stacks compose around the
// same session, semantic tree, action registry, and resolved theme.
type Program[M any] struct {
	Initial      M
	Reduce       Reducer[M]
	View         View[M]
	Actions      *action.Registry
	Theme        theme.Theme
	Config       config.Config
	Capabilities capability.Policy
	Effects      effect.Options
	Observer     observer.Observer
	ModelPolicy  state.ModelPolicy[M]
}

type SessionOptions struct {
	SessionID       string
	RuntimeDetected capability.Manifest
	ClientOffered   capability.Manifest
	ClientPresent   bool
	UserOverrides   capability.Overrides
	Security        capability.SecurityPolicy
	Viewport        layout.Size
}

type Prepared[M any] struct {
	Session      *session.Session[M]
	Actions      *action.Registry
	Capabilities capability.Manifest
	Theme        theme.Resolved
	Config       config.Config
}

func (p Program[M]) NewSession(ctx context.Context, opts SessionOptions) (*Prepared[M], error) {
	if p.Reduce == nil {
		return nil, errors.New("stave: reducer is required")
	}
	if p.View == nil {
		return nil, errors.New("stave: view is required")
	}
	if strings.TrimSpace(opts.SessionID) == "" {
		opts.SessionID = "stave-session"
	}
	cfg := p.Config
	if cfg.SchemaVersion == "" {
		cfg = config.Defaults()
	}
	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("stave: config: %w", err)
	}
	manifest, capabilityDiagnostics := (capability.Negotiation{
		RuntimeDetected: opts.RuntimeDetected,
		ClientOffered:   opts.ClientOffered,
		ClientPresent:   opts.ClientPresent,
		Application:     p.Capabilities,
		UserOverrides:   opts.UserOverrides,
		Security:        opts.Security,
	}).Resolve()
	if opts.Viewport.Width > 0 {
		manifest.Width = opts.Viewport.Width
	}
	if opts.Viewport.Height > 0 {
		manifest.Height = opts.Viewport.Height
	}
	resolved, err := p.Theme.Resolve(theme.Mode(cfg.Theme.Mode), theme.Density(cfg.Theme.Density), manifest)
	if err != nil {
		return nil, fmt.Errorf("stave: theme: %w", err)
	}
	actions := p.Actions
	if actions == nil {
		actions = action.NewRegistry()
	}
	obs := p.Observer
	if obs == nil {
		obs = observer.Nop{}
	}
	for _, diagnostic := range capabilityDiagnostics {
		obs.Observe(ctx, observer.Event{Name: "capability.degraded", Attributes: map[string]string{"code": diagnostic.Code, "field": diagnostic.Field}, Redacted: true})
	}

	var lastRevision atomic.Uint64
	lastRevision.Store(1)
	viewport := layout.Size{Width: manifest.Width, Height: manifest.Height}
	view := func(viewCtx context.Context, model M) (session.ViewResult, error) {
		tree, err := p.View(ViewContext{
			Context:      viewCtx,
			SessionID:    opts.SessionID,
			Revision:     lastRevision.Load(),
			Capabilities: manifest.Clone(),
			Theme:        resolved,
			Viewport:     viewport,
		}, model)
		if err != nil {
			return session.ViewResult{}, err
		}
		if err := validateActionReferences(tree, actions); err != nil {
			return session.ViewResult{}, err
		}
		return session.ViewResult{Tree: tree}, nil
	}
	reduce := func(reduceCtx context.Context, model M, ev event.Event) (M, []effect.Request, error) {
		lastRevision.Store(max(1, ev.Revision))
		return p.Reduce(ReduceContext{Context: reduceCtx, SessionID: opts.SessionID, Sequence: ev.Sequence, Revision: ev.Revision}, model, ev)
	}
	cfgHash := config.Hash(cfg)
	effectOpts := p.Effects
	created, err := session.New(ctx, session.Options[M]{
		SessionID:         opts.SessionID,
		Initial:           p.Initial,
		Reduce:            reduce,
		View:              view,
		QueueCapacity:     cfg.Runtime.InputQueue,
		EffectParallelism: effectOpts.Parallelism,
		EffectDelivery:    effectOpts.Delivery,
		EffectPorts:       effectOpts.Ports,
		MaxActiveBatches:  effectOpts.MaxActiveBatches,
		ModelPolicy:       p.ModelPolicy,
		Capabilities:      manifest,
		ConfigHash:        hex.EncodeToString(cfgHash[:]),
		ThemeHash:         resolved.HashString(),
	})
	if err != nil {
		return nil, err
	}
	obs.Observe(ctx, observer.Event{Name: "session.started", Attributes: map[string]string{"sessionId": opts.SessionID}, Redacted: true})
	return &Prepared[M]{Session: created, Actions: actions, Capabilities: manifest.Clone(), Theme: resolved, Config: cfg}, nil
}

func validateActionReferences(tree semantic.Tree, registry *action.Registry) error {
	if err := tree.Validate(); err != nil {
		return err
	}
	known := make(map[semantic.ActionID]struct{})
	for _, definition := range registry.Manifest() {
		known[semantic.ActionID(definition.ID)] = struct{}{}
	}
	stack := []semantic.Node{tree.Root()}
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		for _, ref := range node.Actions() {
			if _, ok := known[ref.ID]; !ok {
				return fmt.Errorf("stave: semantic action %s is not registered", ref.ID)
			}
		}
		stack = append(stack, node.Children()...)
	}
	return nil
}
