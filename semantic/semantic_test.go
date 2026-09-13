package semantic

import (
	"encoding/base32"
	"encoding/json"
	"reflect"
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

func TestNodeRejectsDELAndC1Controls(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"a", "v", "text", "control", "s"})
	for _, hostile := range []string{"unsafe\x7ftext", "unsafe\u009b31mtext", "unsafe\u009dtitle"} {
		if _, err := NewNode(NodeSpec{ID: id, Role: "text", Name: hostile}); err == nil {
			t.Fatalf("control text %q accepted", hostile)
		}
	}
}

func TestPatchDetailV1ReportsCanonicalChangedFields(t *testing.T) {
	ids := make([]NodeID, 3)
	for i, entity := range []string{"root", "left", "right"} {
		ids[i], _ = NodeIDFor(NodeKey{"detail", "v", "node", entity, "slot"})
	}
	left, _ := NewNode(NodeSpec{ID: ids[1], Role: "text", Name: "left"})
	right, _ := NewNode(NodeSpec{ID: ids[2], Role: "text", Name: "right"})
	before, _ := NewNode(NodeSpec{ID: ids[0], Role: "group", Name: "before", Description: "old", Value: SecretValue(), States: []State{"old"}, Relations: []Relation{{Kind: "owns", Target: ids[1]}}, Actions: []ActionRef{{ID: "old"}}, Layout: LayoutSpec{Width: 1}, Style: StyleIntent{Role: "old"}, Flags: Flags{Visible: true}, Metadata: map[string]string{"a": "old"}, Children: []Node{left, right}})
	after, _ := NewNode(NodeSpec{ID: ids[0], Role: "region", Name: "after", Description: "new", Value: SecretValue(), States: []State{"new"}, Relations: []Relation{{Kind: "owns", Target: ids[2]}}, Actions: []ActionRef{{ID: "new", Default: true}}, Layout: LayoutSpec{Width: 2}, Style: StyleIntent{Role: "new"}, Flags: Flags{Visible: true, Disabled: true}, Metadata: map[string]string{"a": "new"}, Children: []Node{right, left}})
	a, _ := NewTree(1, before)
	b, _ := NewTree(2, after)
	detail, err := DiffDetail(a, b, PatchDetailV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := detail.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(detail.Changed) != 1 || detail.Changed[0].NodeID != ids[0] {
		t.Fatalf("changes=%+v", detail.Changed)
	}
	var paths []string
	for _, field := range detail.Changed[0].Fields {
		paths = append(paths, field.Path)
	}
	want := []string{"/actions", "/children", "/description", "/flags", "/layout", "/metadata", "/name", "/relations", "/role", "/states", "/style"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths=%v want=%v", paths, want)
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), `"text":"`) {
		t.Fatalf("detail leaked value: %s", encoded)
	}
}

func TestPatchDetailNegotiationAndLegacyPatchCompatibility(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"detail", "v", "node", "root", "slot"})
	aNode, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "a"})
	bNode, _ := NewNode(NodeSpec{ID: id, Generation: 1, Role: "text", Name: "b"})
	a, _ := NewTree(1, aNode)
	b, _ := NewTree(2, bNode)
	before, _ := json.Marshal(Diff(a, b))
	if got := string(before); got != `{"fromRevision":1,"toRevision":2,"generationChanged":["`+string(id)+`"]}` {
		t.Fatalf("legacy patch=%s", got)
	}
	if version, ok := NegotiatePatchDetailVersion([]PatchDetailVersion{"other", PatchDetailV1}); !ok || version != PatchDetailV1 {
		t.Fatalf("negotiation=%q,%v", version, ok)
	}
	if _, ok := NegotiatePatchDetailVersion([]PatchDetailVersion{"other"}); ok {
		t.Fatal("unsupported detail negotiated")
	}
	if _, err := DiffDetail(a, b, "other"); err == nil {
		t.Fatal("unsupported detail version accepted")
	}
	after, _ := json.Marshal(Diff(a, b))
	if string(before) != string(after) {
		t.Fatalf("legacy patch changed: %s != %s", before, after)
	}
	detail, err := DiffDetail(a, b, PatchDetailV1)
	if err != nil || len(detail.Changed) != 1 || detail.Changed[0].Fields[0].Path != "/name" {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
}

func TestPatchDetailNoOpAndDeterministicOrdering(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"detail", "v", "node", "root", "slot"})
	node, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "same"})
	a, _ := NewTree(1, node)
	b, _ := NewTree(2, node)
	detail, err := DiffDetail(a, b, PatchDetailV1)
	if err != nil || len(detail.Changed) != 0 || len(detail.Added) != 0 || len(detail.Removed) != 0 || len(detail.GenerationChanged) != 0 {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	first, _ := json.Marshal(detail)
	second, _ := json.Marshal(detail)
	if string(first) != string(second) {
		t.Fatalf("detail is not deterministic: %s != %s", first, second)
	}
}
