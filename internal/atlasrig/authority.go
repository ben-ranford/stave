package atlasrig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/keymap"
)

const (
	InspectAction action.ID = "stave.atlas.route.inspect.v1"
	RetireAction  action.ID = "stave.atlas.route.retire.v1"
)

type InspectInput struct {
	RouteID string `json:"routeId"`
}

type InspectOutput struct {
	RouteID    string `json:"routeId"`
	Status     string `json:"status"`
	QueueDepth int    `json:"queueDepth"`
}

type RetireInput struct {
	RouteID string `json:"routeId"`
	Reason  string `json:"reason"`
}

type RetireOutput struct {
	RouteID  string `json:"routeId"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason"`
}

var atlasActionInventory = [...]action.ID{
	InspectAction,
	RetireAction,
	"stave.primitive.core.activate.v1",
	"stave.primitive.core.back_to_master.v1",
	"stave.primitive.core.cancel.v1",
	"stave.primitive.core.close_modal.v1",
	"stave.primitive.core.confirm.v1",
	"stave.primitive.core.edit.v1",
	"stave.primitive.core.expand_row.v1",
	"stave.primitive.core.goto_page.v1",
	"stave.primitive.core.next_page.v1",
	"stave.primitive.core.open.v1",
	"stave.primitive.core.open_detail.v1",
	"stave.primitive.core.open_row.v1",
	"stave.primitive.core.previous_page.v1",
	"stave.primitive.core.select_option.v1",
	"stave.primitive.core.select_row.v1",
	"stave.primitive.core.select_tab.v1",
	"stave.primitive.core.set-value.v1",
	"stave.primitive.core.sort_column.v1",
	"stave.primitive.core.submit_form.v1",
	"stave.primitive.core.toggle.v1",
}

func buildAuthority() (*action.Registry, keymap.Map, error) {
	ordered := atlasActionInventory[:]

	registry := action.NewRegistry()
	for _, id := range ordered {
		if err := registerAction(registry, id); err != nil {
			return nil, keymap.Map{}, err
		}
	}
	mappings := make([]keymap.Mapping, 0, len(ordered))
	for index, id := range ordered {
		chord := input.KeyChord{Code: input.KeyRune, Rune: rune('a' + index%26), Mods: input.ModAlt}
		if index >= 26 {
			chord.Mods |= input.ModShift
		}
		arguments := json.RawMessage(`{}`)
		switch id {
		case InspectAction:
			arguments = json.RawMessage(`{"routeId":"north-gate"}`)
		case RetireAction:
			arguments = json.RawMessage(`{"routeId":"north-gate","reason":"operator request"}`)
		}
		mappings = append(mappings, keymap.Mapping{
			Binding: keymap.Binding{Sequence: []input.KeyChord{chord}, Command: keymap.CommandID(fmt.Sprintf("atlas.action.%02d.v1", index))},
			Route:   keymap.Route{Kind: keymap.RouteAction, ActionID: id, Arguments: arguments},
			Hint:    fmt.Sprintf("alt+%c", 'a'+index%26),
		})
	}
	km, err := keymap.New("atlas-proof-rig", mappings)
	if err != nil {
		return nil, keymap.Map{}, err
	}
	return registry, km, nil
}

func registerAction(registry *action.Registry, id action.ID) error {
	switch id {
	case InspectAction:
		return action.Register(registry, action.Definition{
			ID: id, Version: "1", Title: "Inspect route", Description: "Read one Atlas route.",
			InputSchema:  action.Schema{ID: "atlas.route.inspect.input", JSON: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"routeId":{"type":"string","minLength":1}},"required":["routeId"]}`)},
			OutputSchema: action.Schema{ID: "atlas.route.inspect.output", JSON: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"routeId":{"type":"string"},"status":{"type":"string"},"queueDepth":{"type":"integer"}},"required":["routeId","status","queueDepth"]}`)},
			Safety:       action.ReadOnly, Idempotency: action.Idempotent,
		}, decodeJSON[InspectInput], encodeJSON[InspectOutput], func(_ context.Context, _ action.Call, in InspectInput) (InspectOutput, error) {
			return InspectOutput{RouteID: in.RouteID, Status: "steady", QueueDepth: 4}, nil
		})
	case RetireAction:
		return action.Register(registry, action.Definition{
			ID: id, Version: "1", Title: "Retire route", Description: "Retire one Atlas route after confirmation.",
			InputSchema:  action.Schema{ID: "atlas.route.retire.input", JSON: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"routeId":{"type":"string","minLength":1},"reason":{"type":"string","minLength":1}},"required":["routeId","reason"]}`)},
			OutputSchema: action.Schema{ID: "atlas.route.retire.output", JSON: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"routeId":{"type":"string"},"accepted":{"type":"boolean"},"reason":{"type":"string"}},"required":["routeId","accepted","reason"]}`)},
			Safety:       action.Consequential, Idempotency: action.NonIdempotent,
			Confirmation: action.ConfirmationPolicy{Required: true, SingleUse: true},
		}, decodeJSON[RetireInput], encodeJSON[RetireOutput], func(_ context.Context, _ action.Call, in RetireInput) (RetireOutput, error) {
			return RetireOutput{RouteID: in.RouteID, Accepted: true, Reason: in.Reason}, nil
		})
	default:
		return registry.Register(action.Definition{
			ID: id, Version: "1", Title: string(id),
			InputSchema:  action.Schema{ID: string(id) + ".input", JSON: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
			OutputSchema: action.Schema{ID: string(id) + ".output", JSON: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
			Safety:       action.Reversible, Idempotency: action.Idempotent,
		}, func(context.Context, action.Call, any) (any, error) { return map[string]any{}, nil })
	}
}

func decodeJSON[T any](raw json.RawMessage) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return value, err
	}
	return value, nil
}

func encodeJSON[T any](value T) (json.RawMessage, error) {
	return json.Marshal(value)
}
