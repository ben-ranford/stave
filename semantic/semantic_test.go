package semantic

import (
	"encoding/base32"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestNodeIdentityStable(t *testing.T) {
	k := NodeKey{"app", "view", "row", "entity", "slot"}
	a, e := NodeIDFor(k)
	if e != nil || !a.Valid() {
		t.Fatal(a, e)
	}
	b, _ := NodeIDFor(k)
	if a != b {
		t.Fatal("unstable")
	}
}
func TestNodeIDLowercaseAndNormalizationPolicy(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "k", "e", "s"})
	if id != NodeID(strings.ToLower(string(id))) {
		t.Fatal(id)
	}
	if NodeID(strings.ToUpper(string(id))).Valid() {
		t.Fatal("uppercase accepted")
	}
	if NodeIDNormalizationPolicy != "stave-node-id-v1:utf8-identity" {
		t.Fatal(NodeIDNormalizationPolicy)
	}
}

func TestNodeIDValidMatchesLegacyASCIIValidation(t *testing.T) {
	valid, err := NodeIDFor(NodeKey{"app", "view", "row", "entity", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	candidates := []NodeID{"", "n1_", valid, NodeID(strings.ToUpper(string(valid))), NodeID("n1_" + strings.Repeat("a", nodeIDEncodedLength))}
	for i := 0; i < len(valid); i++ {
		for c := byte(0); c < 0x80; c++ {
			mutated := []byte(valid)
			mutated[i] = c
			candidates = append(candidates, NodeID(mutated))
		}
	}
	for _, id := range candidates {
		if got, want := id.Valid(), legacyNodeIDValid(id); got != want {
			t.Fatalf("NodeID(%q).Valid() = %v, want legacy result %v", id, got, want)
		}
	}
}

func TestNodeIDValidRejectsNonCanonicalInputWithoutAllocations(t *testing.T) {
	valid, err := NodeIDFor(NodeKey{"app", "view", "row", "entity", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []NodeID{
		NodeID("n1_" + strings.Repeat("a", 25) + "\n"),
		NodeID("n1_" + strings.Repeat("a", 24) + "é"),
	} {
		if id.Valid() {
			t.Fatalf("non-canonical node ID %q accepted", id)
		}
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if !valid.Valid() {
			t.Fatal("valid node ID rejected")
		}
	}); allocations != 0 {
		t.Fatalf("NodeID.Valid allocations = %v, want 0", allocations)
	}
}

func legacyNodeIDValid(id NodeID) bool {
	if !strings.HasPrefix(string(id), "n1_") {
		return false
	}
	s := strings.TrimPrefix(string(id), "n1_")
	if s != strings.ToLower(s) {
		return false
	}
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(s))
	return err == nil && len(b) == 16
}
func TestNodeIDGoldenVectors(t *testing.T) {
	cases := []struct {
		k    NodeKey
		want NodeID
	}{{NodeKey{"app", "view", "row", "entity", "slot"}, "n1_msr5r6hywojwaevcx5ga2rmieq"}, {NodeKey{"应用", "ビュー", "行", "实体", "スロット"}, "n1_4a6yfc3o3p22y6bpxlqb7n6ury"}}
	for _, tc := range cases {
		got, e := NodeIDFor(tc.k)
		if e != nil || got != tc.want {
			t.Fatalf("%v: %s %v", tc.k, got, e)
		}
	}
}
func TestSnapshotNodeRoundTripAndRedaction(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "k", "e", "s"})
	child, _ := NewNode(NodeSpec{ID: id, Generation: 2, Role: "text", Name: "child", Value: SecretValue()})
	rootID, _ := NodeIDFor(NodeKey{"a", "v", "k", "root", "s"})
	root, _ := NewNode(NodeSpec{ID: rootID, Role: "group", Name: "root", Children: []Node{child}})
	b, e := json.Marshal(root)
	if e != nil || !strings.Contains(string(b), `"generation":2`) || strings.Contains(string(b), "secret") {
		t.Fatal(string(b), e)
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m["id"] == nil {
		t.Fatal(string(b))
	}
}
func TestTreeSnapshotRedaction(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "k", "root", "s"})
	n, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "x", Value: SecretValue()})
	tr, e := NewTree(1, n)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []any{tr, tr.Snapshot()} {
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "secret") || strings.Contains(string(b), `"text":"`) {
			t.Fatal(string(b))
		}
	}
}
func TestTreeHashIncludesLayoutStyle(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "k", "hash", "s"})
	a, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "x"})
	b, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "x", Layout: LayoutSpec{Width: 4}})
	c, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "x", Style: StyleIntent{Role: "accent"}})
	ta, _ := NewTree(1, a)
	tb, _ := NewTree(1, b)
	tc, _ := NewTree(1, c)
	for _, tr := range []Tree{tb, tc} {
		if tr.Hash() == ta.Hash() {
			t.Fatal("hash unchanged")
		}
		raw, _ := json.Marshal(tr)
		if !strings.Contains(string(raw), "width") && !strings.Contains(string(raw), "accent") {
			t.Fatal(string(raw))
		}
	}
}
func TestTreeValidationAndPatch(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "b", "e", "s"})
	n, e := NewNode(NodeSpec{ID: id, Role: "button", Name: "x"})
	if e != nil {
		t.Fatal(e)
	}
	a, _ := NewTree(1, n)
	n2, _ := NewNode(NodeSpec{ID: id, Generation: 1, Role: "button", Name: "y"})
	b, _ := NewTree(2, n2)
	p := Diff(a, b)
	if len(p.GenerationChanged) != 1 {
		t.Fatal(p)
	}
	if _, e := json.Marshal(a); e != nil {
		t.Fatal(e)
	}
}

func TestTreeRelationValidationPreservesPreorderErrors(t *testing.T) {
	rootID, err := NodeIDFor(NodeKey{"app", "relations", "group", "root", "main"})
	if err != nil {
		t.Fatal(err)
	}
	childID, err := NodeIDFor(NodeKey{"app", "relations", "text", "child", "main"})
	if err != nil {
		t.Fatal(err)
	}
	danglingID, err := NodeIDFor(NodeKey{"app", "relations", "text", "missing", "main"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewNode(NodeSpec{ID: childID, Role: "text", Name: "child"})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid", func(t *testing.T) {
		root, err := NewNode(NodeSpec{ID: rootID, Role: "group", Name: "root", Relations: []Relation{{Kind: "described-by", Target: childID}}, Children: []Node{child}})
		if err != nil {
			t.Fatal(err)
		}
		tree, err := NewTree(1, root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tree.WithRevision(2); err != nil {
			t.Fatal(err)
		}
		if err := tree.Snapshot().Validate(); err != nil {
			t.Fatal(err)
		}
		relations := tree.Root().Relations()
		relations[0].Target = danglingID
		if err := tree.Validate(); err != nil {
			t.Fatalf("relation accessor mutation changed tree validation: %v", err)
		}
	})

	t.Run("invalid target", func(t *testing.T) {
		root, err := NewNode(NodeSpec{ID: rootID, Role: "group", Name: "root", Relations: []Relation{{Kind: "described-by", Target: "invalid"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewTree(1, root); err == nil || err.Error() != "invalid relation target" {
			t.Fatalf("NewTree() error = %v", err)
		}
	})

	t.Run("dangling relation", func(t *testing.T) {
		root, err := NewNode(NodeSpec{ID: rootID, Role: "group", Name: "root", Relations: []Relation{{Kind: "described-by", Target: danglingID}}})
		if err != nil {
			t.Fatal(err)
		}
		want := "dangling relation " + danglingID.String()
		if _, err := NewTree(1, root); err == nil || err.Error() != want {
			t.Fatalf("NewTree() error = %v, want %q", err, want)
		}
		invalid := Tree{schemaVersion: "stave-semantic-v1", revision: 1, root: root}
		if _, err := invalid.WithRevision(2); err == nil || err.Error() != want {
			t.Fatalf("WithRevision() error = %v, want %q", err, want)
		}
		snapshot := Snapshot{SchemaVersion: "stave-semantic-v1", Revision: 1, TreeHash: "hash", Root: root}
		if err := snapshot.Validate(); err == nil || err.Error() != want {
			t.Fatalf("Snapshot.Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("duplicate precedes dangling relation", func(t *testing.T) {
		duplicate, err := NewNode(NodeSpec{ID: rootID, Role: "text", Name: "duplicate"})
		if err != nil {
			t.Fatal(err)
		}
		root, err := NewNode(NodeSpec{ID: rootID, Role: "group", Name: "root", Relations: []Relation{{Kind: "described-by", Target: danglingID}}, Children: []Node{duplicate}})
		if err != nil {
			t.Fatal(err)
		}
		want := "duplicate node id " + rootID.String() + " generations 0/0"
		if _, err := NewTree(1, root); err == nil || err.Error() != want {
			t.Fatalf("NewTree() error = %v, want %q", err, want)
		}
	})
}

func BenchmarkTreeValidateRelationRich(b *testing.B) {
	for _, nodes := range []int{1000, 2000} {
		b.Run(strconv.Itoa(nodes), func(b *testing.B) {
			tree := relationRichTree(b, nodes)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := tree.Validate(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func relationRichTree(tb testing.TB, nodes int) Tree {
	tb.Helper()
	rootID, err := NodeIDFor(NodeKey{"benchmark", "relations", "group", "root", "main"})
	if err != nil {
		tb.Fatal(err)
	}
	ids := make([]NodeID, nodes)
	for i := range nodes {
		id, err := NodeIDFor(NodeKey{"benchmark", "relations", "text", strconv.Itoa(i), "main"})
		if err != nil {
			tb.Fatal(err)
		}
		ids[i] = id
	}
	children := make([]Node, 0, nodes)
	for _, id := range ids {
		node, err := NewNode(NodeSpec{ID: id, Role: "text", Name: "node", Relations: []Relation{{Kind: "described-by", Target: ids[len(ids)-1]}}})
		if err != nil {
			tb.Fatal(err)
		}
		children = append(children, node)
	}
	root, err := NewNode(NodeSpec{ID: rootID, Role: "group", Name: "root", Children: children})
	if err != nil {
		tb.Fatal(err)
	}
	return Tree{schemaVersion: "stave-semantic-v1", revision: 1, root: root}
}

func TestNodeRejectsDELAndC1Controls(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "text", "control", "s"})
	for _, hostile := range []string{"unsafe\x7ftext", "unsafe\u009b31mtext", "unsafe\u009dtitle"} {
		if _, err := NewNode(NodeSpec{ID: id, Role: "text", Name: hostile}); err == nil {
			t.Fatalf("control text %q accepted", hostile)
		}
	}
}
