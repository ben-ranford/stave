package atlasrig

import (
	"context"
	"fmt"

	"github.com/ben-ranford/stave"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/semantic"
)

type Model struct {
	Scenario  string `json:"scenario"`
	Events    int    `json:"events"`
	LastEvent string `json:"lastEvent,omitempty"`
}

func (r *Rig) Program(scenario string) (stave.Program[Model], error) {
	if !r.hasScenario(scenario) {
		return stave.Program[Model]{}, fmt.Errorf("unsupported atlas scenario %q", scenario)
	}
	return stave.Program[Model]{
		Initial: Model{Scenario: scenario},
		Reduce: func(_ stave.ReduceContext, current Model, incoming event.Event) (Model, []effect.Request, error) {
			current.Events++
			current.LastEvent = string(incoming.Kind)
			return current, nil, nil
		},
		View: func(ctx stave.ViewContext, model Model) (semantic.Tree, error) {
			root, err := buildScenario(model.Scenario, model.Events)
			if err != nil {
				return semantic.Tree{}, err
			}
			return semantic.NewTree(max(1, ctx.Revision), root)
		},
		Actions: r.registry,
		Theme:   r.theme,
	}, nil
}

func (r *Rig) NewSession(ctx context.Context, scenario, profile, sessionID string) (*stave.Prepared[Model], error) {
	selected, ok := r.findProfile(profile)
	if !ok {
		return nil, fmt.Errorf("unsupported atlas profile %q", profile)
	}
	program, err := r.Program(scenario)
	if err != nil {
		return nil, err
	}
	program.Capabilities.OutputMode = selected.Capabilities.OutputMode
	return program.NewSession(ctx, stave.SessionOptions{
		SessionID:       sessionID,
		RuntimeDetected: selected.Capabilities.Clone(),
	})
}
