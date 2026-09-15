package semantic

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ben-ranford/stave/internal/canonical"
)

// PatchDetailVersion identifies an opt-in semantic patch-detail representation.
type PatchDetailVersion string

const PatchDetailV1 PatchDetailVersion = "stave.semantic.patch-detail/v1"

const patchDetailValuePath = "/value"
const patchDetailFlagsPath = "/flags"
const patchDetailSensitiveField = "sensitive"
const patchDetailChildrenPath = "/children"
const patchDetailRelationsPath = "/relations"
const patchDetailNamePath = "/name"
const patchDetailRolePath = "/role"

type patchDetailFieldSpec struct {
	validate  func(json.RawMessage) bool
	normalize func(json.RawMessage) json.RawMessage
}

var patchDetailFields = map[string]patchDetailFieldSpec{
	"/actions":               {validPatchDetailActions, normalizedPatchDetailList[ActionRef]},
	patchDetailChildrenPath:  {validPatchDetailChildren, nil},
	"/description":           {validPatchDetailText, nil},
	patchDetailFlagsPath:     {validPatchDetailFlags, normalizedPatchDetailObject[Flags]},
	"/layout":                {validPatchDetailLayout, normalizedPatchDetailLayout},
	"/metadata":              {validPatchDetailMetadata, nil},
	patchDetailNamePath:      {validPatchDetailText, nil},
	patchDetailRelationsPath: {validPatchDetailRelations, normalizedPatchDetailList[Relation]},
	patchDetailRolePath:      {validPatchDetailRole, nil},
	"/states":                {validPatchDetailStates, normalizedPatchDetailList[State]},
	"/style":                 {validPatchDetailStyle, normalizedPatchDetailObject[StyleIntent]},
	patchDetailValuePath:     {validPatchDetailValue, normalizedPatchDetailObject[Value]},
}

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

// Validate checks ordering, canonical field types and local consistency.
// It does not establish child ownership across parents, absence of cycles,
// reachability from the root, or complete relation-target membership.
// Consumers that reconstruct a tree from external detail must validate the
// complete result with NewTree or Snapshot.Validate. Detail alone omits added
// node payloads and cannot prove full-tree validity or application action authority.
func (p PatchDetail) Validate() error {
	if p.SchemaVersion != PatchDetailV1 {
		return errors.New("unsupported semantic patch detail version")
	}
	if err := (Patch{FromRevision: p.FromRevision, ToRevision: p.ToRevision, Added: p.Added, Removed: p.Removed, GenerationChanged: p.GenerationChanged}).Validate(); err != nil {
		return err
	}
	if !sortedNodeIDs(p.Added) || !sortedNodeIDs(p.Removed) || !sortedNodeIDs(p.GenerationChanged) {
		return errors.New("semantic patch detail node IDs are not sorted")
	}
	previous := NodeID("")
	for _, change := range p.Changed {
		if !change.NodeID.Valid() || (previous != "" && change.NodeID <= previous) || len(change.Fields) == 0 {
			return errors.New("invalid semantic patch detail change")
		}
		if hasSortedNodeID(p.Added, change.NodeID) || hasSortedNodeID(p.Removed, change.NodeID) {
			return errors.New("semantic patch detail change overlaps added or removed node")
		}
		previous = change.NodeID
		if err := validatePatchDetailChange(change, p.Added, p.Removed); err != nil {
			return err
		}
	}
	return nil
}

func validatePatchDetailChange(change NodeChange, added, removed []NodeID) error {
	path := ""
	for _, field := range change.Fields {
		if !supportedPatchDetailField(field.Path) || field.Path <= path || !validPatchDetailEndpoint(field.Path, field.Before) || !validPatchDetailEndpoint(field.Path, field.After) {
			return errors.New("invalid semantic patch detail field")
		}
		if !validPatchDetailTransition(field) || !validPatchDetailChildChange(change.NodeID, field) || !validPatchDetailReferences(field, added, removed) {
			return errors.New("invalid semantic patch detail transition")
		}
		path = field.Path
	}
	if !validPatchDetailRedaction(change.Fields) || !validPatchDetailRoleNames(change.Fields) {
		return errors.New("invalid semantic patch detail field consistency")
	}
	return nil
}

func validPatchDetailTransition(field FieldChange) bool {
	before, after := field.Before, field.After
	if normalize := patchDetailFields[field.Path].normalize; normalize != nil {
		before, after = normalize(before), normalize(after)
	}
	if !bytes.Equal(before, after) {
		return true
	}
	if field.Path != patchDetailValuePath {
		return false
	}
	// A real value transition may be hidden by redaction of both endpoints.
	var value Value
	return json.Unmarshal(field.Before, &value) == nil && value.Redacted
}

func normalizedPatchDetailObject[T any](raw json.RawMessage) json.RawMessage {
	var value T
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return canonicalFieldValue(value)
}

func normalizedPatchDetailList[T any](raw json.RawMessage) json.RawMessage {
	var values []T
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	// NewNode stores both nil and empty input slices as nil.
	if len(values) == 0 {
		values = nil
	}
	return canonicalFieldValue(values)
}

func normalizedPatchDetailLayout(raw json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	// Nonzero integers already have unique, range-checked canonical bytes.
	for name, value := range fields {
		if bytes.Equal(value, []byte("0")) {
			delete(fields, name)
		}
	}
	return canonicalFieldValue(fields)
}

func validPatchDetailRoleNames(fields []FieldChange) bool {
	var role, name *FieldChange
	for i := range fields {
		switch fields[i].Path {
		case patchDetailRolePath:
			role = &fields[i]
		case patchDetailNamePath:
			name = &fields[i]
		}
	}
	if role == nil || name == nil {
		return true
	}
	return validPatchDetailRoleName(role.Before, name.Before) && validPatchDetailRoleName(role.After, name.After)
}

func validPatchDetailRoleName(roleRaw, nameRaw json.RawMessage) bool {
	var role Role
	var name string
	if json.Unmarshal(roleRaw, &role) != nil || json.Unmarshal(nameRaw, &name) != nil {
		return false
	}
	return !interactive[role] || strings.TrimSpace(name) != ""
}

func validPatchDetailReferences(field FieldChange, added, removed []NodeID) bool {
	return validPatchDetailReferenceEndpoint(field.Path, field.Before, added) && validPatchDetailReferenceEndpoint(field.Path, field.After, removed)
}

func validPatchDetailReferenceEndpoint(path string, raw json.RawMessage, absent []NodeID) bool {
	if len(absent) == 0 {
		return true
	}
	var ids []NodeID
	switch path {
	case patchDetailChildrenPath:
		if json.Unmarshal(raw, &ids) != nil {
			return false
		}
	case patchDetailRelationsPath:
		var relations []Relation
		if json.Unmarshal(raw, &relations) != nil {
			return false
		}
		for _, relation := range relations {
			ids = append(ids, relation.Target)
		}
	}
	for _, id := range ids {
		if hasSortedNodeID(absent, id) {
			return false
		}
	}
	return true
}

func validPatchDetailChildChange(id NodeID, field FieldChange) bool {
	if field.Path != patchDetailChildrenPath {
		return true
	}
	return distinctPatchDetailChildren(id, field.Before) && distinctPatchDetailChildren(id, field.After)
}

func distinctPatchDetailChildren(parent NodeID, raw json.RawMessage) bool {
	var children []NodeID
	if json.Unmarshal(raw, &children) != nil {
		return false
	}
	seen := map[NodeID]bool{parent: true}
	for _, child := range children {
		if seen[child] {
			return false
		}
		seen[child] = true
	}
	return true
}

func validPatchDetailRedaction(fields []FieldChange) bool {
	var flags, value *FieldChange
	for i := range fields {
		switch fields[i].Path {
		case patchDetailFlagsPath:
			flags = &fields[i]
		case patchDetailValuePath:
			value = &fields[i]
		}
	}
	before, after, valid := patchDetailValuePair(value)
	if !valid {
		return false
	}
	if flags == nil {
		return true
	}
	beforeSensitive, beforeValid := patchDetailSensitivity(flags.Before)
	afterSensitive, afterValid := patchDetailSensitivity(flags.After)
	if !beforeValid || !afterValid {
		return false
	}
	if !beforeSensitive && !afterSensitive {
		return true
	}
	if value == nil {
		// Other flag changes need not repeat an unchanged sensitive value.
		return beforeSensitive == afterSensitive
	}
	return before.Redacted && after.Redacted
}

func patchDetailValuePair(field *FieldChange) (Value, Value, bool) {
	var before, after Value
	if field == nil {
		return before, after, true
	}
	if json.Unmarshal(field.Before, &before) != nil || json.Unmarshal(field.After, &after) != nil {
		return before, after, false
	}
	return before, after, before.Redacted == after.Redacted
}

func patchDetailSensitivity(raw json.RawMessage) (bool, bool) {
	// Endpoint validation has already checked exact keys and member types.
	var flags Flags
	if json.Unmarshal(raw, &flags) != nil {
		return false, false
	}
	return flags.Sensitive, true
}

func hasSortedNodeID(ids []NodeID, id NodeID) bool {
	i := sort.Search(len(ids), func(i int) bool { return ids[i] >= id })
	return i < len(ids) && ids[i] == id
}

func sortedNodeIDs(ids []NodeID) bool {
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			return false
		}
	}
	return true
}

func supportedPatchDetailField(path string) bool {
	_, ok := patchDetailFields[path]
	return ok
}

func canonicalRawJSON(raw json.RawMessage) bool {
	canonicalized, err := canonical.JSON(raw)
	return err == nil && bytes.Equal(canonicalized, raw)
}

func validPatchDetailEndpoint(path string, raw json.RawMessage) bool {
	if !canonicalRawJSON(raw) {
		return false
	}
	validate, ok := patchDetailFields[path]
	return ok && validate.validate(raw)
}

func validPatchDetailValue(raw json.RawMessage) bool {
	if !validPatchDetailObject(raw, map[string]func(json.RawMessage) bool{
		"text": validPatchDetailText, "redacted": validPatchDetailBool, "hasValue": validPatchDetailBool,
	}) {
		return false
	}
	var value Value
	return json.Unmarshal(raw, &value) == nil && (!value.Redacted || value.Text == "")
}

func validPatchDetailString(raw json.RawMessage) bool {
	var value string
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &value) == nil && utf8.ValidString(value)
}

func validPatchDetailText(raw json.RawMessage) bool {
	var value string
	return validPatchDetailString(raw) && json.Unmarshal(raw, &value) == nil && !containsUnsafeControl(value)
}

func validPatchDetailRole(raw json.RawMessage) bool {
	var role Role
	return json.Unmarshal(raw, &role) == nil && roles[role]
}

func validPatchDetailBool(raw json.RawMessage) bool {
	return bytes.Equal(raw, []byte("true")) || bytes.Equal(raw, []byte("false"))
}

func validPatchDetailInt(raw json.RawMessage) bool {
	// Canonical JSON writes nonzero integers in scientific notation. Avoid a
	// float conversion, which would round integers near the platform limit.
	coefficient, exponent, scientific := strings.Cut(string(raw), "e")
	if scientific {
		power, err := strconv.Atoi(exponent)
		if err != nil || power < 0 || power > 19 || len(coefficient)+power > 20 {
			return false
		}
		coefficient += strings.Repeat("0", power)
	}
	_, err := strconv.Atoi(coefficient)
	return err == nil
}

func validPatchDetailObject(raw json.RawMessage, fields map[string]func(json.RawMessage) bool) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return false
	}
	for name, value := range object {
		validate, ok := fields[name]
		if !ok || !validate(value) {
			return false
		}
	}
	return true
}

func validPatchDetailList(raw json.RawMessage, element func(json.RawMessage) bool) bool {
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	for _, value := range values {
		if !element(value) {
			return false
		}
	}
	return true
}

func validPatchDetailNodeID(raw json.RawMessage) bool {
	var id NodeID
	return json.Unmarshal(raw, &id) == nil && id.Valid()
}

func validPatchDetailChildren(raw json.RawMessage) bool {
	return len(raw) > 0 && raw[0] == '[' && validPatchDetailList(raw, validPatchDetailNodeID)
}

func validPatchDetailMetadata(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for _, value := range fields {
		if !validPatchDetailString(value) {
			return false
		}
	}
	return true
}

func validPatchDetailActions(raw json.RawMessage) bool {
	return validPatchDetailList(raw, func(value json.RawMessage) bool {
		return validPatchDetailObject(value, map[string]func(json.RawMessage) bool{
			"id": validPatchDetailString, "label": validPatchDetailString, "default": validPatchDetailBool,
		})
	})
}

func validPatchDetailRelations(raw json.RawMessage) bool {
	return validPatchDetailList(raw, func(value json.RawMessage) bool {
		var relation Relation
		return validPatchDetailObject(value, map[string]func(json.RawMessage) bool{
			"kind": validPatchDetailString, "target": validPatchDetailNodeID,
		}) && json.Unmarshal(value, &relation) == nil && relation.Target.Valid()
	})
}

func validPatchDetailFlags(raw json.RawMessage) bool {
	return validPatchDetailObject(raw, map[string]func(json.RawMessage) bool{
		"visible": validPatchDetailBool, "focusable": validPatchDetailBool, "disabled": validPatchDetailBool,
		patchDetailSensitiveField: validPatchDetailBool, "offscreen": validPatchDetailBool,
		"live": validPatchDetailString, "stability": validPatchDetailString,
	})
}

func validPatchDetailLayout(raw json.RawMessage) bool {
	return validPatchDetailObject(raw, map[string]func(json.RawMessage) bool{"width": validPatchDetailInt, "height": validPatchDetailInt})
}

func validPatchDetailStyle(raw json.RawMessage) bool {
	return validPatchDetailObject(raw, map[string]func(json.RawMessage) bool{"role": validPatchDetailString})
}

func validPatchDetailStates(raw json.RawMessage) bool {
	return validPatchDetailList(raw, validPatchDetailString)
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
	beforeValue, afterValue := detailValues(a, b)
	fields := []struct {
		path          string
		before, after any
	}{
		{"/actions", a.actions, b.actions}, {patchDetailChildrenPath, childIDs(a.children), childIDs(b.children)},
		{"/description", a.description, b.description}, {patchDetailFlagsPath, a.flags, b.flags},
		{"/layout", a.layout, b.layout}, {"/metadata", a.metadata, b.metadata}, {patchDetailNamePath, a.name, b.name},
		{patchDetailRelationsPath, a.relations, b.relations}, {patchDetailRolePath, a.role, b.role}, {"/states", a.states, b.states},
		{"/style", a.style, b.style},
	}
	out := make([]FieldChange, 0, len(fields))
	for _, field := range fields {
		if canonical.Equal(field.before, field.after) {
			continue
		}
		before := canonicalFieldValue(field.before)
		after := canonicalFieldValue(field.after)
		out = append(out, FieldChange{Path: field.path, Before: before, After: after})
	}
	if !canonical.Equal(a.value, b.value) || detailValueRedacted(a) != detailValueRedacted(b) || a.flags.Sensitive != b.flags.Sensitive {
		out = append(out, FieldChange{Path: patchDetailValuePath, Before: canonicalFieldValue(beforeValue), After: canonicalFieldValue(afterValue)})
	}
	return out
}

func canonicalFieldValue(value any) json.RawMessage {
	raw, err := canonical.Encode(value)
	if err != nil {
		return nil
	}
	canonicalized, err := canonical.JSON(raw)
	if err != nil {
		return nil
	}
	return canonicalized
}

func childIDs(children []Node) []NodeID {
	out := make([]NodeID, len(children))
	for i, child := range children {
		out[i] = child.id
	}
	return out
}

func detailValues(before, after Node) (Value, Value) {
	beforeValue, afterValue := before.value, after.value
	if detailValueRedacted(before) || detailValueRedacted(after) {
		return redactedDetailValue(beforeValue), redactedDetailValue(afterValue)
	}
	return beforeValue, afterValue
}

func detailValueRedacted(node Node) bool {
	return node.flags.Sensitive || node.value.Redacted
}

func redactedDetailValue(value Value) Value {
	value.Text = ""
	value.Redacted = true
	return value
}
