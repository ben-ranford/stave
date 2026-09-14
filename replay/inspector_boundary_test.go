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
	transcript := mustTranscript(t)
	snapshot := tree.Snapshot()
	transcript.Initial.Tree = semanticTreeWire(t, snapshot)
	transcript.Initial.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Prior.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Result.Hashes.Tree = snapshot.TreeHash
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
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
		t.Fatalf("validateUniqueJSONKeys() capped opaque application array: %v", err)
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

func BenchmarkDecodeTranscriptMaximumSemanticTreeDepth(b *testing.B) {
	tree, err := deepSemanticTree(maxSemanticTreeEdges)
	if err != nil {
		b.Fatal(err)
	}
	transcript := mustTranscript(b)
	snapshot := tree.Snapshot()
	transcript.Initial.Tree = semanticTreeWire(b, snapshot)
	transcript.Initial.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Prior.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Result.Hashes.Tree = snapshot.TreeHash
	refreshInspectorCheckpointChecksum(b, &transcript.Initial)
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

func semanticTreeWire(t testing.TB, snapshot semantic.Snapshot) any {
	t.Helper()
	var wire any
	if err := json.Unmarshal(mustJSON(t, snapshot), &wire); err != nil {
		t.Fatal(err)
	}
	return wire
}
