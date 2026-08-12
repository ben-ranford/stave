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

func TestCheckClientSupportsIndependentApplicationProfiles(t *testing.T) {
	for _, namespace := range []string{"atlas", "third-party"} {
		client, tree := testClient(t, namespace)
		report := CheckClient(client, []ClientFixture{{
			Name: "primary",
			Tree: tree,
			Modes: []Mode{
				{Width: 80, Height: 24, TTY: true, Interactive: true, Color: true, Unicode: true},
				{Width: 30, Height: 12},
			},
		}})
		if len(report.Failures) != 0 {
			t.Fatalf("%s failures=%v", namespace, report.Failures)
		}
		if report.Client != namespace || len(report.Fixtures) != 1 || report.Fixtures[0].Modes != 2 {
			t.Fatalf("%s report=%+v", namespace, report)
		}
	}
}

func TestCheckClientRejectsMissingFixtureCoverage(t *testing.T) {
	client, tree := testClient(t, "empty")
	if report := CheckClient(client, nil); len(report.Failures) != 1 || report.Failures[0].Rule != "client-fixtures" {
		t.Fatalf("empty fixture report=%+v", report)
	}
	if report := CheckClient(client, []ClientFixture{{Name: "primary", Tree: tree}}); len(report.Failures) != 1 || report.Failures[0].Rule != "fixture-modes" {
		t.Fatalf("empty mode report=%+v", report)
	}
}

type clientFixtureAdapter struct {
	name     string
	registry *action.Registry
	keymap   keymap.Map
}

func (c clientFixtureAdapter) Name() string { return c.name }
func (c clientFixtureAdapter) Render(semantic.Node, Mode) (string, error) {
	return c.name, nil
}
func (c clientFixtureAdapter) ActionRegistry() *action.Registry { return c.registry }
func (c clientFixtureAdapter) Keymap() keymap.Map               { return c.keymap }

func testClient(t *testing.T, namespace string) (clientFixtureAdapter, semantic.Node) {
	t.Helper()
	button, err := primitive.Button(primitive.Options{Namespace: namespace, View: "main", Entity: "run", Name: "Run"}, "Run")
	if err != nil {
		t.Fatal(err)
	}
	root, err := primitive.Stack(primitive.Options{Namespace: namespace, View: "main", Entity: "root", Name: "Root"}, button)
	if err != nil {
		t.Fatal(err)
	}
	registry := action.NewRegistry()
	id := action.ID(primitive.CanonicalActionID("activate"))
	if err := registry.Register(action.Definition{
		ID:           id,
		Version:      "1",
		Title:        "Activate",
		InputSchema:  action.Schema{ID: "activate-input", JSON: json.RawMessage(`{}`)},
		OutputSchema: action.Schema{ID: "activate-output", JSON: json.RawMessage(`{}`)},
		Safety:       action.ReadOnly,
	}, func(context.Context, action.Call, any) (any, error) { return json.RawMessage(`{}`), nil }); err != nil {
		t.Fatal(err)
	}
	enter, err := input.ParseKey("enter")
	if err != nil {
		t.Fatal(err)
	}
	km, err := keymap.New(namespace, []keymap.Mapping{{
		Binding: keymap.Binding{Sequence: []input.KeyChord{enter}, Command: keymap.CommandActivate},
		Route:   keymap.Route{Kind: keymap.RouteAction, ActionID: id},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return clientFixtureAdapter{name: namespace, registry: registry, keymap: km}, root
}
