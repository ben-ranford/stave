package semantic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatibilityGuideDocumentsSemanticSnapshotVersion(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "docs", "compatibility.md"))
	if err != nil {
		t.Fatal(err)
	}

	id, err := NodeIDFor(NodeKey{"compatibility", "snapshot", "text", "root", "version"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewNode(NodeSpec{ID: id, Role: "text", Name: "version"})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tree.Snapshot()
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}

	var schema struct {
		ID         string `json:"$id"`
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	schemaBytes, err := os.ReadFile(filepath.Join("..", "schema", "semantic", "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}

	if got := schema.Properties["schemaVersion"].Const; got != snapshot.SchemaVersion {
		t.Fatalf("schema snapshot version = %q, runtime version = %q", got, snapshot.SchemaVersion)
	}
	want := "| Semantic snapshot schema | Schema document ID `" + schema.ID + "`; serialized `schemaVersion` `" + snapshot.SchemaVersion + "` |"
	if !strings.Contains(string(document), want) {
		t.Fatalf("compatibility guide missing %q", want)
	}
}
