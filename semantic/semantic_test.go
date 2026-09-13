package semantic

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/internal/canonical"
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

func TestPatchDetailValidateRequiresCanonicalSupportedFields(t *testing.T) {
	first, _ := NodeIDFor(NodeKey{"detail", "v", "node", "first", "slot"})
	second, _ := NodeIDFor(NodeKey{"detail", "v", "node", "second", "slot"})
	if first > second {
		first, second = second, first
	}
	valid := PatchDetail{
		SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2,
		Added:   []NodeID{first, second},
		Changed: []NodeChange{{NodeID: first, Fields: []FieldChange{{Path: "/metadata", Before: json.RawMessage(`1e0`), After: json.RawMessage(`2e0`)}}}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid detail rejected: %v", err)
	}
	for _, mutate := range []func(*PatchDetail){
		func(detail *PatchDetail) { detail.Added = []NodeID{second, first} },
		func(detail *PatchDetail) { detail.Changed[0].Fields[0].Path = "metadata" },
		func(detail *PatchDetail) { detail.Changed[0].Fields[0].Path = "/metadata/key" },
		func(detail *PatchDetail) { detail.Changed[0].Fields[0].Path = "/bogus" },
		func(detail *PatchDetail) { detail.Changed[0].Fields[0].Before = json.RawMessage(`1.0`) },
		func(detail *PatchDetail) { detail.Changed[0].Fields[0].After = json.RawMessage(`{"b":1,"a":2}`) },
	} {
		detail := valid
		detail.Added = append([]NodeID(nil), valid.Added...)
		detail.Changed = append([]NodeChange(nil), valid.Changed...)
		detail.Changed[0].Fields = append([]FieldChange(nil), valid.Changed[0].Fields...)
		mutate(&detail)
		if err := detail.Validate(); err == nil {
			t.Fatal("invalid patch detail accepted")
		}
	}
}

func TestPatchDetailCanonicalValueTransitionsRedactBothEndpoints(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"detail", "v", "node", "value", "slot"})
	beforeNode, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "before", Value: Value{Text: "secret-before", HasValue: true}, Flags: Flags{Visible: true}})
	afterNode, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "after", Value: SecretValue(), Flags: Flags{Visible: true, Sensitive: true}})
	before, _ := NewTree(1, beforeNode)
	after, _ := NewTree(2, afterNode)
	detail, err := DiffDetail(before, after, PatchDetailV1)
	if err != nil {
		t.Fatal(err)
	}
	fields := detail.Changed[0].Fields
	var value FieldChange
	for _, field := range fields {
		if field.Path == "/value" {
			value = field
		}
	}
	if string(value.Before) != `{"hasValue":true,"redacted":true}` || string(value.After) != `{"hasValue":true,"redacted":true}` {
		t.Fatalf("value endpoints = %s -> %s", value.Before, value.After)
	}
	for _, field := range fields {
		for _, endpoint := range []json.RawMessage{field.Before, field.After} {
			canonicalized, err := canonical.JSON(endpoint)
			if err != nil || !bytes.Equal(endpoint, canonicalized) {
				t.Fatalf("non-canonical generated field %s: %s", field.Path, endpoint)
			}
		}
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "secret-before") {
		t.Fatalf("detail leaked redacted endpoint: %s", encoded)
	}
}

func TestPatchDetailValueAndFlagOnlyRedactionTransitions(t *testing.T) {
	id, _ := NodeIDFor(NodeKey{"detail", "v", "node", "value-transition", "slot"})
	ordinaryBefore, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "value", Value: Value{Text: "before", HasValue: true}, Flags: Flags{Visible: true}})
	ordinaryAfter, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "value", Value: Value{Text: "after", HasValue: true}, Flags: Flags{Visible: true}})
	before, _ := NewTree(1, ordinaryBefore)
	after, _ := NewTree(2, ordinaryAfter)
	ordinary, err := DiffDetail(before, after, PatchDetailV1)
	if err != nil {
		t.Fatal(err)
	}
	if got := ordinary.Changed[0].Fields; len(got) != 1 || got[0].Path != "/value" || string(got[0].Before) != `{"hasValue":true,"text":"before"}` || string(got[0].After) != `{"hasValue":true,"text":"after"}` {
		t.Fatalf("ordinary value transition = %+v", got)
	}
	flagBefore, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "value", Value: Value{HasValue: true}, Flags: Flags{Visible: true}})
	flagAfter, _ := NewNode(NodeSpec{ID: id, Role: "text", Name: "value", Value: Value{HasValue: true}, Flags: Flags{Visible: true, Sensitive: true}})
	before, _ = NewTree(1, flagBefore)
	after, _ = NewTree(2, flagAfter)
	redacted, err := DiffDetail(before, after, PatchDetailV1)
	if err != nil {
		t.Fatal(err)
	}
	if got := redacted.Changed[0].Fields; len(got) != 2 || got[0].Path != "/flags" || got[1].Path != "/value" || string(got[1].Before) != `{"hasValue":true,"redacted":true}` || string(got[1].After) != `{"hasValue":true,"redacted":true}` {
		t.Fatalf("flag-only redaction transition = %+v", got)
	}
}

func TestPatchDetailNegotiatedBytesKeepLegacyPatchUnchanged(t *testing.T) {
	ids := make([]NodeID, 4)
	for i, entity := range []string{"root", "removed", "generation", "added"} {
		ids[i], _ = NodeIDFor(NodeKey{"detail", "v", "node", entity, "slot"})
	}
	removed, _ := NewNode(NodeSpec{ID: ids[1], Role: "text", Name: "removed"})
	beforeGeneration, _ := NewNode(NodeSpec{ID: ids[2], Role: "text", Name: "generation"})
	added, _ := NewNode(NodeSpec{ID: ids[3], Role: "text", Name: "added"})
	afterGeneration, _ := NewNode(NodeSpec{ID: ids[2], Generation: 1, Role: "text", Name: "generation"})
	beforeRoot, _ := NewNode(NodeSpec{ID: ids[0], Role: "group", Name: "root", Children: []Node{removed, beforeGeneration}})
	afterRoot, _ := NewNode(NodeSpec{ID: ids[0], Role: "group", Name: "root", Children: []Node{afterGeneration, added}})
	before, _ := NewTree(1, beforeRoot)
	after, _ := NewTree(2, afterRoot)
	legacy, _ := json.Marshal(Diff(before, after))
	wantLegacy := fmt.Sprintf(`{"fromRevision":1,"toRevision":2,"added":[%q],"removed":[%q],"generationChanged":[%q]}`, ids[3], ids[1], ids[2])
	if string(legacy) != wantLegacy {
		t.Fatalf("legacy patch = %s, want %s", legacy, wantLegacy)
	}
	if _, ok := NegotiatePatchDetailVersion([]PatchDetailVersion{"other"}); ok {
		t.Fatal("unnegotiated detail version accepted")
	}
	detail, err := DiffDetail(before, after, PatchDetailV1)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(detail)
	wantDetail := fmt.Sprintf(`{"schemaVersion":"stave.semantic.patch-detail/v1","fromRevision":1,"toRevision":2,"added":[%q],"removed":[%q],"generationChanged":[%q],"changed":[{"nodeId":%q,"fields":[{"path":"/children","before":[%q,%q],"after":[%q,%q]}]}]}`, ids[3], ids[1], ids[2], ids[0], ids[1], ids[2], ids[2], ids[3])
	if string(encoded) != wantDetail {
		t.Fatalf("negotiated detail = %s, want %s", encoded, wantDetail)
	}
	legacyAfter, _ := json.Marshal(Diff(before, after))
	if !bytes.Equal(legacy, legacyAfter) {
		t.Fatalf("detail generation changed legacy bytes: %s != %s", legacy, legacyAfter)
	}
}
