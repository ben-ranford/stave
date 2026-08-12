package semantic

import (
	"encoding/json"
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

func TestNodeRejectsDELAndC1Controls(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "text", "control", "s"})
	for _, hostile := range []string{"unsafe\x7ftext", "unsafe\u009b31mtext", "unsafe\u009dtitle"} {
		if _, err := NewNode(NodeSpec{ID: id, Role: "text", Name: hostile}); err == nil {
			t.Fatalf("control text %q accepted", hostile)
		}
	}
}
