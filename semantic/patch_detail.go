package semantic

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/ben-ranford/stave/internal/canonical"
)

// PatchDetailVersion identifies an opt-in semantic patch-detail representation.
type PatchDetailVersion string

const PatchDetailV1 PatchDetailVersion = "stave.semantic.patch-detail/v1"

// NegotiatePatchDetailVersion returns the detail version supported by both peers.
// Callers that do not negotiate a version must continue to use Patch.
func NegotiatePatchDetailVersion(offered []PatchDetailVersion) (PatchDetailVersion, bool) {
	for _, version := range offered {
		if version == PatchDetailV1 {
			return PatchDetailV1, true
		}
	}
	return "", false
}

// PatchDetail is a separately versioned, opt-in supplement to Patch. It never
// includes full node payloads; only changed fields are represented.
type PatchDetail struct {
	SchemaVersion     PatchDetailVersion `json:"schemaVersion"`
	FromRevision      uint64             `json:"fromRevision"`
	ToRevision        uint64             `json:"toRevision"`
	Added             []NodeID           `json:"added,omitempty"`
	Removed           []NodeID           `json:"removed,omitempty"`
	GenerationChanged []NodeID           `json:"generationChanged,omitempty"`
	Changed           []NodeChange       `json:"changed,omitempty"`
}

type NodeChange struct {
	NodeID NodeID        `json:"nodeId"`
	Fields []FieldChange `json:"fields"`
}

// FieldChange uses JSON Pointer paths. Before and After are canonical JSON
// values for the named field, never a complete node representation.
type FieldChange struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func (p PatchDetail) Validate() error {
	if p.SchemaVersion != PatchDetailV1 {
		return errors.New("unsupported semantic patch detail version")
	}
	if err := (Patch{FromRevision: p.FromRevision, ToRevision: p.ToRevision, Added: p.Added, Removed: p.Removed, GenerationChanged: p.GenerationChanged}).Validate(); err != nil {
		return err
	}
	previous := NodeID("")
	for _, change := range p.Changed {
		if !change.NodeID.Valid() || (previous != "" && change.NodeID <= previous) || len(change.Fields) == 0 {
			return errors.New("invalid semantic patch detail change")
		}
		previous = change.NodeID
		path := ""
		for _, field := range change.Fields {
			if field.Path == "" || field.Path <= path || !json.Valid(field.Before) || !json.Valid(field.After) {
				return errors.New("invalid semantic patch detail field")
			}
			path = field.Path
		}
	}
	return nil
}

// DiffDetail returns changed-field detail only for a previously negotiated
// version. Legacy Patch and Diff remain unchanged for unnegotiated clients.
func DiffDetail(a, b Tree, version PatchDetailVersion) (PatchDetail, error) {
	if version != PatchDetailV1 {
		return PatchDetail{}, errors.New("unsupported semantic patch detail version")
	}
	patch := Diff(a, b)
	detail := PatchDetail{SchemaVersion: version, FromRevision: patch.FromRevision, ToRevision: patch.ToRevision, Added: patch.Added, Removed: patch.Removed, GenerationChanged: patch.GenerationChanged}
	left, right := nodesByID(a.root), nodesByID(b.root)
	ids := make([]NodeID, 0, len(left))
	for id := range left {
		if _, ok := right[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		fields := changedFields(left[id], right[id])
		if len(fields) > 0 {
			detail.Changed = append(detail.Changed, NodeChange{NodeID: id, Fields: fields})
		}
	}
	return detail, detail.Validate()
}

func nodesByID(root Node) map[NodeID]Node {
	out := map[NodeID]Node{}
	var walk func(Node)
	walk = func(node Node) {
		out[node.id] = node
		for _, child := range node.children {
			walk(child)
		}
	}
	walk(root)
	return out
}

func changedFields(a, b Node) []FieldChange {
	fields := []struct {
		path          string
		before, after any
	}{
		{"/actions", a.actions, b.actions}, {"/children", childIDs(a.children), childIDs(b.children)},
		{"/description", a.description, b.description}, {"/flags", a.flags, b.flags},
		{"/layout", a.layout, b.layout}, {"/metadata", a.metadata, b.metadata}, {"/name", a.name, b.name},
		{"/relations", a.relations, b.relations}, {"/role", a.role, b.role}, {"/states", a.states, b.states},
		{"/style", a.style, b.style}, {"/value", detailValue(a), detailValue(b)},
	}
	out := make([]FieldChange, 0, len(fields))
	for _, field := range fields {
		if canonical.Equal(field.before, field.after) {
			continue
		}
		before, _ := canonical.Encode(field.before)
		after, _ := canonical.Encode(field.after)
		out = append(out, FieldChange{Path: field.path, Before: before, After: after})
	}
	return out
}

func childIDs(children []Node) []NodeID {
	out := make([]NodeID, len(children))
	for i, child := range children {
		out[i] = child.id
	}
	return out
}

func detailValue(node Node) Value {
	value := node.value
	if node.flags.Sensitive || value.Redacted {
		value.Text = ""
	}
	return value
}
