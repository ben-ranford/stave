package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/conformance"
	"github.com/ben-ranford/stave/keymap"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/testfixture"
)

func TestManifestModesCoverReleaseProfiles(t *testing.T) {
	want := []string{"tty-color", "tty-no-color", "ascii", "narrow", "non-tty", "reduced-motion"}
	for _, name := range want {
		mode, err := conformanceMode(name)
		if err != nil {
			t.Fatal(err)
		}
		if mode.Width <= 0 || mode.Height <= 0 {
			t.Fatalf("invalid mode %q: %+v", name, mode)
		}
	}
	if _, err := conformanceMode("unknown"); err == nil {
		t.Fatal("unknown mode was accepted")
	}
}

func TestLoadPrimitiveManifestRejectsMutations(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "primitive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest primitiveManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}

	t.Run("schema-version", func(t *testing.T) {
		mutated := manifest
		mutated.SchemaVersion = "broken"
		b, err := json.Marshal(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadPrimitiveManifest(b); err == nil {
			t.Fatal("mutated schema version was accepted")
		}
	})

	t.Run("unknown-invariant", func(t *testing.T) {
		mutated := manifest
		mutated.Invariants = append([]string(nil), mutated.Invariants...)
		mutated.Invariants[0] = "unknown-invariant"
		b, err := json.Marshal(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadPrimitiveManifest(b); err == nil {
			t.Fatal("unknown invariant was accepted")
		}
	})

	t.Run("duplicate-fixture", func(t *testing.T) {
		mutated := manifest
		mutated.Fixtures = append([]string(nil), mutated.Fixtures...)
		mutated.Fixtures[1] = mutated.Fixtures[0]
		b, err := json.Marshal(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadPrimitiveManifest(b); err == nil {
			t.Fatal("duplicate fixture was accepted")
		}
	})
}

func TestDeclaredPrimitiveInvariantsRejectBadAdapters(t *testing.T) {
	modes, modesByName, err := conformanceModes([]string{"tty-color", "tty-no-color"})
	if err != nil {
		t.Fatal(err)
	}
	_ = modes

	t.Run("colour-not-meaning", func(t *testing.T) {
		tree := testfixture.Must(testfixture.Primitive("text", "catalog"))
		adapter := stubAdapter{colorOutput: "colour output", plainOutput: "plain output"}
		err := enforceDeclaredPrimitiveInvariants(adapter, tree, []string{"colour-not-meaning"}, modesByName)
		if err == nil {
			t.Fatal("colour invariant was not enforced")
		}
	})

	t.Run("keyboard-agent-parity", func(t *testing.T) {
		tree := testfixture.Must(testfixture.Primitive("action-descriptor", "catalog"))
		adapter := stubAdapter{colorOutput: "same", plainOutput: "same"}
		err := enforceDeclaredPrimitiveInvariants(adapter, tree, []string{"keyboard-agent-parity"}, map[string]conformance.Mode{"tty-color": {TTY: true, Interactive: true, Color: true, Width: 80, Height: 24}})
		if err == nil {
			t.Fatal("keyboard parity invariant was not enforced")
		}
	})
}

func TestNormalizeRenderedMeaningIgnoresTerminalPadding(t *testing.T) {
	left := "Title: ready   \nStatus: ok   \n     \n"
	right := "Title: ready\nStatus: ok"
	if normalizeRenderedMeaning(left) != normalizeRenderedMeaning(right) {
		t.Fatal("terminal padding changed semantic output")
	}
}

type stubAdapter struct {
	colorOutput string
	plainOutput string
}

func (s stubAdapter) Name() string { return "stub" }
func (s stubAdapter) Render(_ semantic.Node, mode conformance.Mode) (string, error) {
	if mode.Color {
		return s.colorOutput, nil
	}
	return s.plainOutput, nil
}
func (s stubAdapter) ActionRegistry() *action.Registry { return nil }
func (s stubAdapter) Keymap() keymap.Map               { return keymap.Map{} }
