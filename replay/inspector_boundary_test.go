package replay

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/semantic"
)

func TestDecodeTranscriptRejectsPayloadForPayloadlessEvent(t *testing.T) {
	transcript := mustTranscript(t)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"kind":"key"`), []byte(`"kind":"focus"`), 1)
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted a payload for a focus event")
	}
}

func TestValidateUniqueJSONKeysBoundsNestedInput(t *testing.T) {
	withinLimit := []byte(strings.Repeat("[", maxGenericJSONDepth+1) + "0" + strings.Repeat("]", maxGenericJSONDepth+1))
	if err := validateUniqueJSONKeys(withinLimit, 0); err != nil {
		t.Fatalf("validateUniqueJSONKeys() rejected the exact scalar boundary: %v", err)
	}
	overLimit := []byte(strings.Repeat("[", maxGenericJSONDepth+2) + "0" + strings.Repeat("]", maxGenericJSONDepth+2))
	if err := validateUniqueJSONKeys(overLimit, 0); err == nil {
		t.Fatal("validateUniqueJSONKeys() accepted excessive nesting")
	}
}

func TestDecodeTranscriptAcceptsMaximumSemanticTreeDepth(t *testing.T) {
	tree, err := deepSemanticTree(maxSemanticTreeEdges)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tree.Snapshot()
	transcript := transcriptWithSemanticSnapshot(t, snapshot)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTranscript(data); err != nil {
		t.Fatalf("DecodeTranscript() rejected a maximum-depth semantic tree: %v", err)
	}
}

func TestDecodeTranscriptRejectsSemanticTreeBeyondJSONDepth(t *testing.T) {
	tree, err := deepSemanticTree(maxSemanticTreeEdges)
	if err != nil {
		t.Fatal(err)
	}
	wire := semanticTreeWire(t, tree.Snapshot()).(map[string]any)
	node := wire["root"].(map[string]any)
	for {
		children, ok := node["children"].([]any)
		if !ok || len(children) == 0 {
			break
		}
		node = children[0].(map[string]any)
	}
	child := make(map[string]any)
	if err := json.Unmarshal(mustJSON(t, node), &child); err != nil {
		t.Fatal(err)
	}
	child["actions"] = []any{map[string]any{"id": "deep.action"}}
	node["children"] = []any{child}

	transcript := mustTranscript(t)
	transcript.Initial.Tree = wire
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTranscript(data); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("DecodeTranscript() error = %v, want JSON nesting limit", err)
	}
}

func TestDecodeTranscriptKeepsGenericDepthLimitInsideInitialModel(t *testing.T) {
	transcript := mustTranscript(t)
	var model any = "leaf"
	for range maxGenericJSONDepth {
		model = map[string]any{"initial": map[string]any{"tree": model}}
	}
	transcript.Initial.Model = model
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTranscript(data); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("DecodeTranscript() error = %v, want generic JSON nesting limit inside initial.model", err)
	}
}

func TestSemanticTreeRejectsDepthBeyondMaximum(t *testing.T) {
	if _, err := deepSemanticTree(maxSemanticTreeEdges + 1); err == nil {
		t.Fatal("semantic.NewTree() accepted a tree deeper than the supported maximum")
	}
}

func TestSemanticSnapshotRelationWorkBound(t *testing.T) {
	tree := semanticTreeWithRelation(t)
	var wire semanticSnapshotWire
	if err := json.Unmarshal(mustJSON(t, tree.Snapshot()), &wire); err != nil {
		t.Fatal(err)
	}
	if err := wire.Root.validateRelationWork(2); err != nil {
		t.Fatalf("validateRelationWork() rejected exact boundary: %v", err)
	}
	wire.Root.Relations = append(wire.Root.Relations, wire.Root.Relations[0])
	if err := wire.Root.validateRelationWork(2); err == nil || !strings.Contains(err.Error(), "relation work") {
		t.Fatalf("validateRelationWork() error = %v, want relation work limit", err)
	}
}

func TestDecodeTranscriptAcceptsAndValidatesSemanticSnapshotRelations(t *testing.T) {
	tree := semanticTreeWithRelation(t)
	snapshot := tree.Snapshot()
	transcript := transcriptWithSemanticSnapshot(t, snapshot)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTranscript(data); err != nil {
		t.Fatalf("DecodeTranscript() rejected a valid semantic relation: %v", err)
	}

	treeWire := transcript.Initial.Tree.(map[string]any)
	relation := treeWire["root"].(map[string]any)["relations"].([]any)[0].(map[string]any)
	relation["target"] = "invalid"
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	invalid, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTranscript(invalid); err == nil {
		t.Fatal("DecodeTranscript() accepted an invalid semantic relation target")
	}
}

func TestDecodeTranscriptRejectsOverBudgetSemanticRelationWork(t *testing.T) {
	wire := overBudgetSemanticRelationWire(t)
	if _, err := decodeInitialSemanticSnapshot(wire); err == nil || !strings.Contains(err.Error(), "relation work") {
		t.Fatalf("decodeInitialSemanticSnapshot() error = %v, want relation work limit before semantic construction", err)
	}
	var tree any
	if err := json.Unmarshal(mustJSON(t, wire), &tree); err != nil {
		t.Fatal(err)
	}
	transcript := mustTranscript(t)
	transcript.Initial.Tree = tree
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxTranscriptBytes {
		t.Fatal("over-budget relation fixture exceeded byte limit")
	}
	if err := validateUniqueJSONKeys(data, 0); err != nil {
		t.Fatalf("over-budget relation fixture exceeded a JSON preflight limit: %v", err)
	}
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted over-budget semantic relation work")
	}
}

func TestValidateUniqueJSONKeysBoundsRootRecordsBeforeRawDecoding(t *testing.T) {
	if err := validateUniqueJSONKeys(compactRecordsJSON(MaxTranscriptRecords), 0); err != nil {
		t.Fatalf("validateUniqueJSONKeys() rejected exact record boundary: %v", err)
	}
	overLimit := compactRecordsJSON(MaxTranscriptRecords + 1)
	if _, err := DecodeTranscript(overLimit); err == nil || !strings.Contains(err.Error(), "record limit") {
		t.Fatalf("DecodeTranscript() error = %v, want early record limit", err)
	}
}

func TestDecodeTranscriptAcceptsCanonicalEmptyRecords(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records = nil
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTranscript(data)
	if err != nil {
		t.Fatalf("DecodeTranscript() rejected canonical empty records: %v", err)
	}
	if len(decoded.Records) != 0 {
		t.Fatalf("decoded records = %d, want zero", len(decoded.Records))
	}
}

func TestDecodeTranscriptRejectsCaseFoldedRootRecordsBeforeRawDecoding(t *testing.T) {
	data := compactRecordsJSON(MaxTranscriptRecords + 1)
	original := append([]byte(nil), data...)
	data = bytes.Replace(data, []byte(`{"records":`), []byte(`{"Records":`), 1)
	if bytes.Equal(data, original) {
		t.Fatal("test fixture did not case-fold root records")
	}
	if _, err := DecodeTranscript(data); err == nil || !strings.Contains(err.Error(), "noncanonical") {
		t.Fatalf("DecodeTranscript() error = %v, want early canonical records key rejection", err)
	}
}

func TestValidateUniqueJSONKeysDoesNotCapOpaqueArrays(t *testing.T) {
	data := []byte(`{"model":` + string(compactEmptyObjects(MaxTranscriptRecords+1)) + `}`)
	if err := validateUniqueJSONKeys(data, 0); err != nil {
		t.Fatalf("validateUniqueJSONKeys() rejected an opaque array below the value limit: %v", err)
	}
}

func TestValidateUniqueJSONKeysBoundsCumulativeValues(t *testing.T) {
	data := []byte(`{"model":[0,1]}`)
	if err := validateUniqueJSONKeysWithValueLimit(data, 0, 4); err != nil {
		t.Fatalf("validateUniqueJSONKeysWithValueLimit() rejected exact value boundary: %v", err)
	}
	if err := validateUniqueJSONKeysWithValueLimit(data, 0, 3); err == nil || !strings.Contains(err.Error(), "value count") {
		t.Fatalf("validateUniqueJSONKeysWithValueLimit() error = %v, want value limit", err)
	}
}

func TestValidateUniqueJSONKeysBoundsActionArguments(t *testing.T) {
	data := []byte(`{"records":[{"event":{"payload":{"arguments":[{},{}]}}}]}`)
	if err := validateUniqueJSONKeysWithValueLimit(data, 0, 7); err == nil || !strings.Contains(err.Error(), "value count") {
		t.Fatalf("validateUniqueJSONKeysWithValueLimit() error = %v, want value limit in action arguments", err)
	}
}

func TestDecodeTranscriptRejectsOverBudgetOpaqueModelBeforeCheckpointValidation(t *testing.T) {
	transcript := mustTranscript(t)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	model := []byte(`"model":` + string(compactEmptyObjects(maxTranscriptJSONValues)))
	updated := bytes.Replace(data, []byte(`"model":{"status":"ok"}`), model, 1)
	if bytes.Equal(updated, data) {
		t.Fatal("test fixture did not replace initial.model")
	}
	if _, err := DecodeTranscript(updated); err == nil || !strings.Contains(err.Error(), "value count") || strings.Contains(err.Error(), "CHECKPOINT") {
		t.Fatalf("DecodeTranscript() error = %v, want pre-checkpoint value limit", err)
	}
}

func TestValidateUniqueJSONKeysRejectsLargeTrailingValue(t *testing.T) {
	data := append([]byte(`{"records":[]}`), compactEmptyObjects(maxTranscriptJSONValues)...)
	if err := validateUniqueJSONKeys(data, 0); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("validateUniqueJSONKeys() error = %v, want trailing JSON", err)
	}
}

func TestDecodeTranscriptDuplicateKeyDoesNotExposeKeyName(t *testing.T) {
	const secretKey = "fuzz-secret-key"
	data := []byte(`{"` + secretKey + `":1,"` + secretKey + `":2}`)
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted duplicate key")
	} else if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("DecodeTranscript() exposed duplicate key: %v", err)
	}
}

func compactRecordsJSON(count int) []byte {
	data := make([]byte, 0, len(`{"records":}`)+count*3)
	data = append(data, `{"records":[`...)
	data = append(data, compactEmptyObjects(count)[1:]...)
	return append(data, '}')
}

func compactEmptyObjects(count int) []byte {
	data := make([]byte, 0, 2+count*3)
	data = append(data, '[')
	for index := 0; index < count; index++ {
		if index > 0 {
			data = append(data, ',')
		}
		data = append(data, '{', '}')
	}
	return append(data, ']')
}

func BenchmarkValidateUniqueJSONKeysNestedScalar(b *testing.B) {
	data := []byte(strings.Repeat("[", 60) + `"` + strings.Repeat("x", 1<<20) + `"` + strings.Repeat("]", 60))
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		if err := validateUniqueJSONKeys(data, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateUniqueJSONKeysRejectsLargeTrailingValue(b *testing.B) {
	data := append([]byte(`{"records":[]}`), compactEmptyObjects(maxTranscriptJSONValues)...)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		if err := validateUniqueJSONKeys(data, 0); err == nil {
			b.Fatal("validateUniqueJSONKeys() accepted trailing JSON")
		}
	}
}

func BenchmarkDecodeTranscriptMaximumSemanticTreeDepth(b *testing.B) {
	tree, err := deepSemanticTree(maxSemanticTreeEdges)
	if err != nil {
		b.Fatal(err)
	}
	snapshot := tree.Snapshot()
	transcript := transcriptWithSemanticSnapshot(b, snapshot)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeTranscript(data); err != nil {
			b.Fatal(err)
		}
	}
}

func deepSemanticTree(edges int) (semantic.Tree, error) {
	var child semantic.Node
	for depth := edges; depth >= 0; depth-- {
		key := semantic.NodeKey{AppNamespace: "stave", View: "replay", Kind: "node", Entity: strconv.Itoa(depth), Slot: "main"}
		spec := semantic.NodeSpec{Key: &key, Generation: 1, Role: "group", Name: "node"}
		if depth == edges {
			spec.Actions = []semantic.ActionRef{{ID: "deep.action"}}
		} else {
			spec.Children = []semantic.Node{child}
		}
		var err error
		child, err = semantic.NewNode(spec)
		if err != nil {
			return semantic.Tree{}, err
		}
	}
	return semantic.NewTree(1, child)
}

func semanticTreeWithRelation(t testing.TB) semantic.Tree {
	t.Helper()
	childID, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "stave", View: "replay", Kind: "node", Entity: "child", Slot: "main"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := semantic.NewNode(semantic.NodeSpec{ID: childID, Generation: 1, Role: "text", Name: "child"})
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "stave", View: "replay", Kind: "node", Entity: "root", Slot: "main"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := semantic.NewNode(semantic.NodeSpec{
		ID: rootID, Generation: 1, Role: "group", Name: "root", Children: []semantic.Node{child},
		Relations: []semantic.Relation{{Kind: "describedby", Target: childID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func overBudgetSemanticRelationWire(t testing.TB) semanticSnapshotWire {
	t.Helper()
	const nodes = 750
	children := make([]semanticNodeWire, nodes-1)
	for index := range children {
		id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "stave", View: "replay", Kind: "node", Entity: strconv.Itoa(index), Slot: "main"})
		if err != nil {
			t.Fatal(err)
		}
		children[index] = semanticNodeWire{ID: id, Generation: 1, Role: "text", Name: "child"}
	}
	rootID, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "stave", View: "replay", Kind: "node", Entity: "root", Slot: "main"})
	if err != nil {
		t.Fatal(err)
	}
	relations := make([]semantic.Relation, nodes)
	for index := range relations {
		relations[index] = semantic.Relation{Kind: "describedby", Target: children[index%len(children)].ID}
	}
	return semanticSnapshotWire{
		SchemaVersion: "stave-semantic-v1", Revision: 1, TreeHash: "unvalidated",
		Root: semanticNodeWire{ID: rootID, Generation: 1, Role: "group", Name: "root", Relations: relations, Children: children},
	}
}

func semanticTreeWire(t testing.TB, snapshot semantic.Snapshot) any {
	t.Helper()
	var wire any
	if err := json.Unmarshal(mustJSON(t, snapshot), &wire); err != nil {
		t.Fatal(err)
	}
	return wire
}

func transcriptWithSemanticSnapshot(t testing.TB, snapshot semantic.Snapshot) Transcript {
	t.Helper()
	transcript := mustTranscript(t)
	transcript.Initial.Tree = semanticTreeWire(t, snapshot)
	transcript.Initial.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Prior.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Result.Hashes.Tree = snapshot.TreeHash
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	return transcript
}
