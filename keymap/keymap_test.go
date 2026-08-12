package keymap

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func TestNewRejectsAmbiguousBindings(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	_, err := New("test", []Mapping{
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandFocusNext},
			Route:   Route{Kind: RouteEvent, EventKind: "focus"},
		},
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandFocusPrevious},
			Route:   Route{Kind: RouteEvent, EventKind: "focus"},
		},
	})
	if err == nil {
		t.Fatal("expected ambiguous binding validation error")
	}
}

func TestDispatcherRoutesActivateToTypedAction(t *testing.T) {
	keymap, err := Default()
	if err != nil {
		t.Fatalf("default keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	enter, _ := input.ParseKey("enter")
	target := semantic.Target{NodeID: semantic.NodeID("n1_target"), Generation: 1, ObservedRevision: 9}
	result, err := dispatcher.Feed(enter, "", target, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != StatusMatch || result.Call == nil {
		t.Fatalf("expected matched action route, got %#v", result)
	}
	if result.Call.ActionID != action.ID("stave.primitive.core.activate.v1") {
		t.Fatalf("unexpected action id: %s", result.Call.ActionID)
	}
	if result.Call.Target != target {
		t.Fatal("expected target to flow through action call")
	}
}

func TestDispatcherKeepsAllDefaultBindingsMapped(t *testing.T) {
	keymap, err := Default()
	if err != nil {
		t.Fatalf("default keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	target := semantic.Target{NodeID: semantic.NodeID("n1_target"), Generation: 1, ObservedRevision: 3}
	for _, mapping := range keymap.Bindings() {
		dispatcher.Reset()
		result, err := dispatcher.Feed(mapping.Binding.Sequence[0], mapping.Binding.Scope, target, time.Unix(20, 0))
		if err != nil {
			t.Fatalf("dispatch %s: %v", mapping.Binding.Command, err)
		}
		if result.Status != StatusMatch {
			t.Fatalf("binding %s did not resolve, got %s", mapping.Binding.Command, result.Status)
		}
		if result.Call == nil && result.Event == nil {
			t.Fatalf("binding %s has no typed action or event parity", mapping.Binding.Command)
		}
	}
}

func TestDispatcherReturnsPendingForPrefixSequence(t *testing.T) {
	ctrlX, _ := input.ParseKey("ctrl+x")
	ctrlS, _ := input.ParseKey("ctrl+s")
	keymap, err := New("custom", []Mapping{
		{
			Binding: Binding{
				Sequence: []input.KeyChord{ctrlX, ctrlS},
				Command:  CommandActivate,
			},
			Route: Route{Kind: RouteAction, ActionID: action.ID("stave.save.v1")},
		},
	})
	if err != nil {
		t.Fatalf("new keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	result, err := dispatcher.Feed(ctrlX, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatalf("dispatch prefix: %v", err)
	}
	if result.Status != StatusPending {
		t.Fatalf("expected pending prefix, got %s", result.Status)
	}
	result, err = dispatcher.Feed(ctrlS, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatalf("dispatch completion: %v", err)
	}
	if result.Status != StatusMatch || result.Call == nil {
		t.Fatalf("expected completed action match, got %#v", result)
	}
}

func TestActionArgumentsRemainCanonicalBytes(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	args := json.RawMessage(`{"direction":"next"}`)
	keymap, err := New("custom", []Mapping{
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandActivate},
			Route:   Route{Kind: RouteAction, ActionID: action.ID("stave.focus.move.v1"), Arguments: args},
		},
	})
	if err != nil {
		t.Fatalf("new keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	result, err := dispatcher.Feed(tab, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(result.Call.Arguments) != string(args) {
		t.Fatalf("expected raw arguments to survive mapping, got %s", result.Call.Arguments)
	}
}

func TestInventoryIsImmutableAndExposesVersionedActions(t *testing.T) {
	m, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	inventory := m.Inventory()
	if len(inventory) == 0 {
		t.Fatal("expected built-in inventory")
	}
	ids := m.ActionIDs()
	if len(ids) == 0 {
		t.Fatalf("action ids=%v", ids)
	}
	foundActivate := false
	for _, id := range ids {
		if id == action.ID("stave.primitive.core.activate.v1") {
			foundActivate = true
		}
	}
	if !foundActivate {
		t.Fatalf("activate action missing from ids=%v", ids)
	}
	inventory[0].Sequence[0] = input.KeyChord{Code: input.KeyDelete}
	if m.Inventory()[0].Sequence[0].Code == input.KeyDelete {
		t.Fatal("inventory leaked mutable sequence")
	}
	for _, item := range inventory {
		if item.Command == "" {
			t.Fatal("unnamed command")
		}
		if item.RouteKind == RouteAction && item.ActionID == "" {
			t.Fatalf("action route missing id: %#v", item)
		}
	}
}

func TestNewRejectsInvalidRouteIDsAndPayloads(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	for _, tc := range []Mapping{
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandActivate},
			Route:   Route{Kind: RouteAction, ActionID: action.ID("invalid")},
		},
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandShutdown},
			Route:   Route{Kind: RouteEvent, EventKind: "shutdown", Payload: map[string]string{"reason": "nope"}},
		},
	} {
		if _, err := New("invalid", []Mapping{tc}); err == nil {
			t.Fatalf("expected validation error for %#v", tc.Route)
		}
	}
}

func TestDispatcherClonesRouteArgumentsAndPayloadAtIngress(t *testing.T) {
	chord, _ := input.ParseKey("enter")
	args := json.RawMessage(`{"x":1}`)
	payload := map[string]string{"key": "enter"}
	km, err := New("test", []Mapping{{Binding: Binding{Sequence: []input.KeyChord{chord}, Command: CommandActivate}, Route: Route{Kind: RouteAction, ActionID: action.ID(primitive.CanonicalActionID("activate")), Arguments: args}}})
	if err != nil {
		t.Fatal(err)
	}
	args[0] = 'x'
	d := NewDispatcher(km)
	r, err := d.Feed(chord, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Call.Arguments) != `{"x":1}` {
		t.Fatalf("route args were aliased: %s", r.Call.Arguments)
	}
	_ = payload
}
