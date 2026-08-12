package state

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/semantic"
)

type testModel struct {
	Name   string            `json:"name"`
	Secret string            `json:"secret,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}

func TestCheckpointUsesCanonicalHashesAndStableOrdering(t *testing.T) {
	tree := mustTree(t, 1, "screen")
	st, err := New("session-1", testModel{Name: "ok", Tags: map[string]string{"b": "2", "a": "1"}}, tree, Meta{
		Capabilities: capability.Manifest{Width: 80, Height: 24, Interactive: true},
		ConfigHash:   "cfg",
		ThemeHash:    "theme",
		SurfaceHash:  "surface",
	}, ModelPolicy[testModel]{})
	if err != nil {
		t.Fatal(err)
	}

	first, err := st.Checkpoint(ModelPolicy[testModel]{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Checkpoint(ModelPolicy[testModel]{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Checksum != second.Checksum {
		t.Fatalf("checkpoint checksum changed: %s != %s", first.Checksum, second.Checksum)
	}
	if first.Hashes.Model != second.Hashes.Model {
		t.Fatalf("model hash changed: %s != %s", first.Hashes.Model, second.Hashes.Model)
	}
}

func TestCheckpointSanitizeExcludesSecrets(t *testing.T) {
	tree := mustTree(t, 1, "screen")
	st, err := New("session-1", testModel{Name: "ok", Secret: "top-secret"}, tree, Meta{}, ModelPolicy[testModel]{
		Sanitize: func(model testModel) (testModel, error) {
			model.Secret = ""
			return model, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	checkpoint, err := st.Checkpoint(ModelPolicy[testModel]{
		Sanitize: func(model testModel) (testModel, error) {
			model.Secret = ""
			return model, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("expected serialized checkpoint")
	}
	if contains(data, "top-secret") {
		t.Fatalf("checkpoint leaked secret: %s", data)
	}
}

func TestStateCloneProtectsAgainstLaterMutation(t *testing.T) {
	tree := mustTree(t, 1, "screen")
	model := testModel{Name: "ok", Tags: map[string]string{"a": "1"}}
	st, err := New("session-1", model, tree, Meta{}, ModelPolicy[testModel]{})
	if err != nil {
		t.Fatal(err)
	}
	model.Tags["a"] = "mutated"

	cloned, err := st.Clone(ModelPolicy[testModel]{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cloned.Model.Tags["a"]; got != "1" {
		t.Fatalf("stored model mutated through external alias: %q", got)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	tree := mustTree(t, 3, "screen")
	st, err := New("session-1", testModel{Name: "ok"}, tree, Meta{
		Sequence:     4,
		Revision:     3,
		Capabilities: capability.Manifest{Width: 120, Height: 40},
	}, ModelPolicy[testModel]{})
	if err != nil {
		t.Fatal(err)
	}

	checkpoint, err := st.Checkpoint(ModelPolicy[testModel]{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := checkpoint.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Checkpoint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Sequence != checkpoint.Sequence || decoded.Revision != checkpoint.Revision {
		t.Fatalf("roundtrip changed checkpoint: %#v != %#v", decoded, checkpoint)
	}
}

func mustTree(t *testing.T, revision uint64, entity string) semantic.Tree {
	t.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{
		AppNamespace: "stave",
		View:         "test",
		Kind:         "root",
		Entity:       entity,
		Slot:         "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := semantic.NewNode(semantic.NodeSpec{
		ID:         id,
		Generation: 1,
		Role:       "application",
		Name:       "root",
		Flags:      semantic.Flags{Visible: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(revision, node)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func contains(data []byte, needle string) bool {
	return json.Valid(data) && strings.Contains(string(data), needle)
}
