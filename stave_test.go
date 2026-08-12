package stave

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
	"github.com/ben-ranford/stave/theme"
)

type programModel struct{ Count int }

func TestProgramComposesProductionSession(t *testing.T) {
	program := Program[programModel]{
		Initial: programModel{},
		Reduce: func(_ ReduceContext, model programModel, ev event.Event) (programModel, []effect.Request, error) {
			if ev.Kind == event.Key {
				model.Count++
			}
			return model, nil, nil
		},
		View: func(ctx ViewContext, model programModel) (semantic.Tree, error) {
			if ctx.Theme.HashString() == "" || ctx.SessionID != "program-test" {
				t.Fatal("view context was not resolved")
			}
			root, err := primitive.Text(primitive.Options{Namespace: "test", View: "main", Entity: "count", Name: "Count"}, fmt.Sprintf("%d", model.Count))
			if err != nil {
				return semantic.Tree{}, err
			}
			return semantic.NewTree(max(1, ctx.Revision), root)
		},
		Theme: programTheme(t),
	}
	prepared, err := program.NewSession(context.Background(), SessionOptions{
		SessionID:       "program-test",
		RuntimeDetected: capability.Manifest{Width: 80, Height: 24, OutputMode: capability.OutputPlain, Unicode: capability.UnicodeASCII},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(prepared.Session.Close)
	key, err := event.New(event.Key, event.KeyPayload{Key: "enter"})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Session.Send(key); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Session.Wait(context.Background(), func(snapshot state.State[programModel]) bool { return snapshot.Model.Count == 1 }); err != nil {
		t.Fatal(err)
	}
	if prepared.Capabilities.Color != capability.ColorNone || prepared.Theme.HashString() == "" {
		t.Fatalf("unexpected resolved program environment: %+v", prepared.Capabilities)
	}
}

func TestProgramRejectsUnregisteredSemanticActions(t *testing.T) {
	program := Program[int]{
		Reduce: func(_ ReduceContext, model int, _ event.Event) (int, []effect.Request, error) { return model, nil, nil },
		View: func(ctx ViewContext, _ int) (semantic.Tree, error) {
			button, err := primitive.Button(primitive.Options{Namespace: "test", View: "main", Entity: "save", Name: "Save"}, "Save")
			if err != nil {
				return semantic.Tree{}, err
			}
			return semantic.NewTree(max(1, ctx.Revision), button)
		},
		Theme: programTheme(t),
	}
	_, err := program.NewSession(context.Background(), SessionOptions{RuntimeDetected: capability.Manifest{OutputMode: capability.OutputPlain, Unicode: capability.UnicodeASCII}})
	if err == nil || !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("unregistered semantic action accepted: %v", err)
	}
}

func programTheme(t testing.TB) theme.Theme {
	t.Helper()
	tokens := theme.TokenSet{}
	for _, role := range theme.RequiredRoleIDs() {
		name := string(role)
		switch {
		case strings.HasPrefix(name, "motion.duration"):
			tokens[role] = theme.Value{Kind: theme.KindDuration, Literal: "0ms"}
		case strings.HasPrefix(name, "motion.easing"):
			tokens[role] = theme.Value{Kind: theme.KindString, Literal: "none"}
		case strings.HasPrefix(name, "space."), strings.HasPrefix(name, "radius."), strings.HasPrefix(name, "elevation."):
			tokens[role] = theme.Value{Kind: theme.KindNumber, Literal: 1}
		case strings.HasPrefix(name, "type."):
			tokens[role] = theme.Value{Kind: theme.KindString, Literal: "terminal"}
		default:
			tokens[role] = theme.Value{Kind: theme.KindColor, Literal: "#ffffff"}
		}
	}
	return theme.Theme{
		ID: "program-test", Version: "v1",
		Modes:     map[theme.Mode]theme.TokenSet{theme.ModeAuto: tokens},
		Densities: map[theme.Density]theme.TokenSet{theme.DensityComfortable: {}},
		Glyphs:    map[string]theme.GlyphSet{"render": {"truncation": {Unicode: "…", ASCII: "...", Width: 3}}},
		Assets: map[string]theme.AssetRef{
			"brand.mark": {ID: "test.mark", Text: "T"}, "brand.mark.ascii": {ID: "test.mark.ascii", Text: "T"}, "brand.banner.terminal": {ID: "test.banner", Text: "Test"},
		},
	}
}
