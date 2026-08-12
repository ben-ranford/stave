package conformance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/keymap"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func TestValidateTreeRejectsHiddenFocusableAndMissingFieldErrorRelation(t *testing.T) {
	hidden, err := semantic.NewNode(semantic.NodeSpec{
		Key: &semantic.NodeKey{
			AppNamespace: "test",
			View:         "v",
			Kind:         "button",
			Entity:       "hidden",
			Slot:         "default",
		},
		Role:  "button",
		Name:  "Hidden",
		Flags: semantic.Flags{Visible: false, Focusable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := semantic.NewNode(semantic.NodeSpec{
		Key: &semantic.NodeKey{
			AppNamespace: "test",
			View:         "v",
			Kind:         "textbox",
			Entity:       "invalid",
			Slot:         "default",
		},
		Role: "textbox",
		Name: "Invalid",
		Flags: semantic.Flags{
			Visible:   true,
			Focusable: true,
		},
		Metadata: map[string]string{"invalid": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := primitive.Stack(primitive.Options{Namespace: "test", View: "v", Entity: "root", Name: "Root"}, hidden, invalid)
	if err != nil {
		t.Fatal(err)
	}
	failures := ValidateTree(root)
	if len(failures) < 2 {
		t.Fatalf("failures=%v", failures)
	}
}

func TestCheckAdapterUsesRegistryAndKeymapAuthority(t *testing.T) {
	button, err := primitive.Button(primitive.Options{Namespace: "test", View: "v", Entity: "button", Name: "Run"}, "Run")
	if err != nil {
		t.Fatal(err)
	}
	root, err := primitive.Stack(primitive.Options{Namespace: "test", View: "v", Entity: "root", Name: "Root"}, button)
	if err != nil {
		t.Fatal(err)
	}
	registry := action.NewRegistry()
	if err := registry.Register(action.Definition{
		ID:           action.ID(primitive.CanonicalActionID("activate")),
		Version:      "1",
		Title:        "Activate",
		InputSchema:  action.Schema{ID: "activate-input", JSON: json.RawMessage(`{}`)},
		OutputSchema: action.Schema{ID: "activate-output", JSON: json.RawMessage(`{}`)},
		Safety:       action.ReadOnly,
	}, func(context.Context, action.Call, any) (any, error) {
		return json.RawMessage(`{}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	km, err := keymap.New("test", []keymap.Mapping{{
		Binding: keymap.Binding{
			Sequence: []input.KeyChord{inputChord(t, "enter")},
			Command:  keymap.CommandActivate,
		},
		Route: keymap.Route{Kind: keymap.RouteAction, ActionID: action.ID(primitive.CanonicalActionID("activate"))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	report := CheckAdapter(fakeAdapter{registry: registry, keymap: km}, root, []Mode{{Width: 80, Height: 24}})
	if len(report.Failures) != 0 {
		t.Fatalf("failures=%v", report.Failures)
	}

	report = CheckAdapter(fakeAdapter{registry: action.NewRegistry(), keymap: km}, root, []Mode{{Width: 80, Height: 24}})
	if len(report.Failures) == 0 {
		t.Fatal("expected authority failures")
	}
}

type fakeAdapter struct {
	registry *action.Registry
	keymap   keymap.Map
}

func (f fakeAdapter) Name() string                               { return "fake" }
func (f fakeAdapter) Render(semantic.Node, Mode) (string, error) { return "ok", nil }
func (f fakeAdapter) ActionRegistry() *action.Registry           { return f.registry }
func (f fakeAdapter) Keymap() keymap.Map                         { return f.keymap }

func inputChord(t *testing.T, raw string) input.KeyChord {
	t.Helper()
	chord, err := input.ParseKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return chord
}
