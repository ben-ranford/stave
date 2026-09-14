package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
)

const (
	// MaxTranscriptBytes bounds an inspector input before JSON decoding.
	MaxTranscriptBytes = 16 << 20
	// MaxTranscriptRecords bounds the amount of saved evidence an inspector accepts.
	MaxTranscriptRecords    = 100_000
	maxGenericJSONDepth     = 64
	maxTranscriptJSONValues = 400_000
	// semantic.Tree accepts a root-to-leaf path of 1024 edges. A transcript
	// wraps its root node in the transcript, initial checkpoint, and snapshot
	// objects; every edge then adds a children array and node object. A deepest
	// action or relation object adds two more containers.
	maxSemanticTreeEdges   = 1024
	maxTranscriptJSONDepth = 2*maxSemanticTreeEdges + 5
	// A semantic snapshot is validated twice while decoding: once by NewTree
	// and once by Snapshot.Validate. Bound each nodes×relations traversal.
	maxSemanticSnapshotRelationWork uint64 = 500_000
)

// DecodeTranscript decodes one saved transcript artifact. JSON whitespace and
// field order need not match CanonicalJSON output. It rejects
// unknown JSON fields, trailing values, unsupported versions, malformed
// records, and artifacts that exceed the inspector bounds. It validates saved
// evidence only; it never re-executes an application model.
func DecodeTranscript(data []byte) (Transcript, error) {
	if len(data) > MaxTranscriptBytes {
		return Transcript{}, fmt.Errorf("replay transcript exceeds %d-byte limit", MaxTranscriptBytes)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Transcript{}, errors.New("replay transcript is empty")
	}
	if !utf8.Valid(data) {
		return Transcript{}, errors.New("replay transcript is not valid UTF-8")
	}
	if data[0] != '{' {
		return Transcript{}, errors.New("replay transcript must be a JSON object")
	}
	if err := validateUniqueJSONKeys(data, 0); err != nil {
		return Transcript{}, fmt.Errorf("decode replay transcript: %w", err)
	}
	if err := validateRawEventPayloads(data); err != nil {
		return Transcript{}, fmt.Errorf("decode replay transcript: %w", err)
	}
	if err := validateCanonicalTypedKeys(data); err != nil {
		return Transcript{}, fmt.Errorf("decode replay transcript: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var transcript Transcript
	if err := decoder.Decode(&transcript); err != nil {
		return Transcript{}, fmt.Errorf("decode replay transcript: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Transcript{}, fmt.Errorf("decode replay transcript: %w", err)
	}
	if len(transcript.Records) > MaxTranscriptRecords {
		return Transcript{}, fmt.Errorf("replay transcript exceeds %d-record limit", MaxTranscriptRecords)
	}
	for i := range transcript.Records {
		decoded, err := transcript.Records[i].Event.Clone()
		if err != nil {
			return Transcript{}, fmt.Errorf("decode replay record %d event: %w", i, err)
		}
		transcript.Records[i].Event = decoded
	}
	if err := ValidateTranscript(transcript); err != nil {
		return Transcript{}, err
	}
	return transcript, nil
}

// validateUniqueJSONKeys rejects duplicate fields throughout an input artifact
// and caps nesting before typed decoding. It mirrors the strict protocol input
// boundary, so a duplicate cannot silently replace earlier saved evidence.
func validateUniqueJSONKeys(data []byte, depth int) error {
	return validateUniqueJSONKeysWithValueLimit(data, depth, maxTranscriptJSONValues)
}

func validateUniqueJSONKeysWithValueLimit(data []byte, depth, valueLimit int) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	budget := jsonValueBudget{limit: valueLimit}
	if err := validateUniqueJSONValueInScope(decoder, depth, maxGenericJSONDepth, false, &budget); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func requireJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}

type jsonValueBudget struct {
	limit  int
	values int
}

func (budget *jsonValueBudget) consume() error {
	budget.values++
	if budget.values > budget.limit {
		return errors.New("replay transcript JSON value count exceeds limit")
	}
	return nil
}

func validateUniqueJSONValueInScope(decoder *json.Decoder, depth, maxDepth int, initialCheckpoint bool, budget *jsonValueBudget) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if err := budget.consume(); err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth > maxDepth {
		return errors.New("replay transcript JSON nesting exceeds limit")
	}
	switch delim {
	case '{':
		if err := validateTranscriptObjectInScope(decoder, depth, maxDepth, initialCheckpoint, budget); err != nil {
			return err
		}
	case '[':
		for decoder.More() {
			if err := validateUniqueJSONValueInScope(decoder, depth+1, maxDepth, false, budget); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid replay transcript JSON value")
	}
	_, err = decoder.Token()
	return err
}

func validateTranscriptObjectInScope(decoder *json.Decoder, depth, maxDepth int, initialCheckpoint bool, budget *jsonValueBudget) error {
	seen := map[string]struct{}{}
	for decoder.More() {
		key, err := validateUniqueJSONObjectKey(decoder, seen)
		if err != nil {
			return err
		}
		if depth == 0 && key != "records" && strings.EqualFold(key, "records") {
			return errors.New("noncanonical replay transcript records key")
		}
		if depth == 0 && key == "records" {
			if err := validateTranscriptRecords(decoder, depth+1, budget); err != nil {
				return err
			}
			continue
		}
		if depth == 0 && key == "initial" {
			if err := validateUniqueJSONValueInScope(decoder, depth+1, maxGenericJSONDepth, true, budget); err != nil {
				return err
			}
			continue
		}
		if initialCheckpoint && key == "tree" {
			if err := validateUniqueJSONValueInScope(decoder, depth+1, maxTranscriptJSONDepth, false, budget); err != nil {
				return err
			}
			continue
		}
		if err := validateUniqueJSONValueInScope(decoder, depth+1, maxDepth, false, budget); err != nil {
			return err
		}
	}
	return nil
}

func validateTranscriptRecords(decoder *json.Decoder, depth int, budget *jsonValueBudget) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if err := budget.consume(); err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	if token != json.Delim('[') {
		return errors.New("replay transcript records must be an array")
	}
	if depth > maxGenericJSONDepth {
		return errors.New("replay transcript JSON nesting exceeds limit")
	}
	for count := 0; decoder.More(); count++ {
		if count >= MaxTranscriptRecords {
			return fmt.Errorf("replay transcript exceeds %d-record limit", MaxTranscriptRecords)
		}
		if err := validateUniqueJSONValueInScope(decoder, depth+1, maxGenericJSONDepth, false, budget); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func validateUniqueJSONObjectKey(decoder *json.Decoder, seen map[string]struct{}) (string, error) {
	keyToken, err := decoder.Token()
	if err != nil {
		return "", err
	}
	key, ok := keyToken.(string)
	if !ok {
		return "", errors.New("invalid replay transcript object key")
	}
	if _, exists := seen[key]; exists {
		return "", errors.New("duplicate replay transcript key")
	}
	seen[key] = struct{}{}
	return key, nil
}

func validateRawEventPayloads(data []byte) error {
	var wire struct {
		Records []struct {
			Event json.RawMessage `json:"event"`
		} `json:"records"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	for index, record := range wire.Records {
		var eventWire struct {
			SchemaVersion string          `json:"schemaVersion"`
			Kind          event.Kind      `json:"kind"`
			Payload       json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(record.Event, &eventWire); err != nil {
			return fmt.Errorf("replay record %d event: %w", index, err)
		}
		if eventWire.SchemaVersion != event.SchemaVersion {
			return fmt.Errorf("replay record %d event has unsupported schemaVersion", index)
		}
		if payloadlessEvent(eventWire.Kind) && len(bytes.TrimSpace(eventWire.Payload)) > 0 && !bytes.Equal(bytes.TrimSpace(eventWire.Payload), []byte("null")) {
			return fmt.Errorf("replay record %d event %q must not have a payload", index, eventWire.Kind)
		}
		if err := validateSensitiveRawPayload(index, eventWire.Kind, eventWire.Payload); err != nil {
			return err
		}
	}
	return nil
}

// validateCanonicalTypedKeys rejects case-folded aliases only for the
// framework-owned wire objects. Checkpoint Model, action arguments, effect
// values, and diagnostic context remain opaque application data. Checkpoint
// Tree is validated separately as a semantic snapshot.
func validateCanonicalTypedKeys(data []byte) error {
	return validateTypedJSON(data, reflect.TypeOf(Transcript{}))
}

var (
	eventType           = reflect.TypeOf(event.Event{})
	rawMessageType      = reflect.TypeOf(json.RawMessage(nil))
	typedJSONFieldCache sync.Map // map[reflect.Type]map[string]reflect.Type
)

// validateTypedJSON checks canonical field spelling from the actual framework
// types. It stops at application-owned interfaces and maps, which keeps model
// data and arbitrary action/effect values opaque to the inspector.
func validateTypedJSON(raw []byte, typ reflect.Type) error {
	if typ == nil || typ == rawMessageType || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Interface, reflect.Map:
		return nil
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := validateTypedJSON(value, typ.Elem()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		object, err := rawObject(raw)
		if err != nil {
			return err
		}
		if object == nil {
			return nil
		}
		fields := typedJSONFields(typ)
		if err := rejectTypedAliases(object, fields); err != nil {
			return err
		}
		for name, fieldType := range fields {
			if err := validateTypedJSON(object[name], fieldType); err != nil {
				return err
			}
		}
		if typ == eventType {
			var decoded event.Event
			if err := json.Unmarshal(raw, &decoded); err != nil {
				return err
			}
			if payload := object["payload"]; len(payload) != 0 {
				return validateTypedJSON(payload, reflect.TypeOf(decoded.Payload))
			}
		}
	}
	return nil
}

func typedJSONFields(typ reflect.Type) map[string]reflect.Type {
	if cached, ok := typedJSONFieldCache.Load(typ); ok {
		return cached.(map[string]reflect.Type)
	}
	fields := make(map[string]reflect.Type)
	for index := range typ.NumField() {
		field := typ.Field(index)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	actual, _ := typedJSONFieldCache.LoadOrStore(typ, fields)
	return actual.(map[string]reflect.Type)
}

func rawObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	return object, nil
}

func rejectTypedAliases(object map[string]json.RawMessage, fields map[string]reflect.Type) error {
	for key := range object {
		for field := range fields {
			if key != field && strings.EqualFold(key, field) {
				return fmt.Errorf("noncanonical typed key %q, want %q", key, field)
			}
		}
	}
	return nil
}

func validateSensitiveRawPayload(index int, kind event.Kind, raw []byte) error {
	switch kind {
	case event.ActionInvoked:
		var payload struct {
			Sensitive bool            `json:"sensitive"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		if payload.Sensitive && (!hasOnlyTypedFields(raw, reflect.TypeOf(event.ActionInvokedPayload{})) || !isSensitiveRedaction(payload.Arguments)) {
			return fmt.Errorf("replay record %d sensitive action payload is not redacted", index)
		}
	case event.EffectResult:
		var payload struct {
			Sensitive bool            `json:"sensitive"`
			Value     json.RawMessage `json:"value"`
			Error     string          `json:"error"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		if payload.Sensitive && (!hasOnlyTypedFields(raw, reflect.TypeOf(event.EffectResultPayload{})) || !isSensitiveRedaction(payload.Value) || payload.Error != "effect execution failed") {
			return fmt.Errorf("replay record %d sensitive effect payload is not redacted", index)
		}
	}
	return nil
}

func hasOnlyTypedFields(raw []byte, typ reflect.Type) bool {
	object, err := rawObject(raw)
	if err != nil {
		return false
	}
	fields := typedJSONFields(typ)
	for key := range object {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	return true
}

func isSensitiveRedaction(raw []byte) bool {
	object, err := rawObject(raw)
	if err != nil || len(object) != 2 {
		return false
	}
	if _, ok := object["redacted"]; !ok {
		return false
	}
	if _, ok := object["reason"]; !ok {
		return false
	}
	if err := validateTypedJSON(raw, reflect.TypeOf(event.Redaction{})); err != nil {
		return false
	}
	var redaction event.Redaction
	return json.Unmarshal(raw, &redaction) == nil && redaction == (event.Redaction{Redacted: true, Reason: "sensitive"})
}

func payloadlessEvent(kind event.Kind) bool {
	switch kind {
	case event.Focus, event.Blur, event.Tick, event.Cancel, event.Shutdown:
		return true
	default:
		return false
	}
}

// ValidateTranscript verifies that one saved transcript is internally
// consistent. It does not invoke ApplyFunc or reconstruct application state.
func ValidateTranscript(transcript Transcript) error {
	if len(transcript.Records) > MaxTranscriptRecords {
		return fmt.Errorf("replay transcript exceeds %d-record limit", MaxTranscriptRecords)
	}
	if transcript.SchemaVersion != SchemaVersion {
		return &Divergence{Code: DivergenceSchemaVersion, Index: -1, Field: "schemaVersion", Expected: SchemaVersion, Actual: transcript.SchemaVersion}
	}
	if err := state.ValidateSupportedVersions(transcript.Versions); err != nil {
		return &Divergence{Code: DivergenceSchemaVersion, Index: -1, Field: "versions", Expected: "supported runtime versions", Actual: transcript.Versions}
	}
	if transcript.Initial.SchemaVersion != state.CheckpointSchemaVersion {
		return &Divergence{Code: DivergenceSchemaVersion, Index: -1, Field: "initial.schemaVersion", Expected: state.CheckpointSchemaVersion, Actual: transcript.Initial.SchemaVersion}
	}
	if transcript.Initial.SessionID != transcript.SessionID {
		return &Divergence{Code: DivergenceSessionID, Index: -1, Field: "initial.sessionId", Expected: transcript.SessionID, Actual: transcript.Initial.SessionID}
	}
	if err := transcript.Initial.VerifyChecksum(); err != nil {
		return &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "initial.checksum", Expected: "valid", Actual: err.Error()}
	}
	snapshot, err := decodeInitialSemanticSnapshot(transcript.Initial.Tree)
	if err != nil {
		return &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "initial.tree", Expected: "valid semantic snapshot", Actual: "invalid"}
	}
	if snapshot.Revision != transcript.Initial.Revision {
		return &Divergence{Code: DivergenceRevision, Index: -1, Field: "initial.tree.revision", Expected: transcript.Initial.Revision, Actual: snapshot.Revision}
	}
	if snapshot.TreeHash != transcript.Initial.Hashes.Tree {
		return &Divergence{Code: DivergenceTreeHash, Index: -1, Field: "initial.tree.treeHash", Expected: transcript.Initial.Hashes.Tree, Actual: snapshot.TreeHash}
	}
	capabilityHash, err := state.HashString(transcript.Initial.Capabilities.Clone())
	if err != nil {
		return fmt.Errorf("initial capability manifest hash: %w", err)
	}
	if transcript.Initial.Hashes.Capability != capabilityHash {
		return &Divergence{Code: DivergenceCapability, Index: -1, Field: "initial.hashes.capability", Expected: capabilityHash, Actual: transcript.Initial.Hashes.Capability}
	}
	if err := compareVersions(-1, transcript.Versions, transcript.Initial.Versions); err != nil {
		return err
	}
	if err := validateTranscriptProducerRecords(transcript.Records); err != nil {
		return err
	}
	return Validate(transcript, transcript)
}

func decodeInitialSemanticSnapshot(tree any) (semantic.Snapshot, error) {
	data, err := json.Marshal(tree)
	if err != nil {
		return semantic.Snapshot{}, err
	}
	if err := validateSemanticSnapshotKeys(data); err != nil {
		return semantic.Snapshot{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var wire semanticSnapshotWire
	if err := decoder.Decode(&wire); err != nil {
		return semantic.Snapshot{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return semantic.Snapshot{}, errors.New("trailing semantic snapshot")
		}
		return semantic.Snapshot{}, err
	}
	if err := wire.Root.validateRelationWork(maxSemanticSnapshotRelationWork); err != nil {
		return semantic.Snapshot{}, err
	}
	root, err := wire.Root.node()
	if err != nil {
		return semantic.Snapshot{}, err
	}
	treeValue, err := semantic.NewTree(wire.Revision, root)
	if err != nil {
		return semantic.Snapshot{}, err
	}
	snapshot := treeValue.Snapshot()
	if wire.SchemaVersion != snapshot.SchemaVersion || wire.TreeHash != snapshot.TreeHash {
		return semantic.Snapshot{}, errors.New("semantic snapshot metadata mismatch")
	}
	if err := snapshot.Validate(); err != nil {
		return semantic.Snapshot{}, err
	}
	return snapshot, nil
}

// validateSemanticSnapshotKeys checks the framework-owned snapshot spelling
// in one token pass. It avoids recursively unmarshaling RawMessages for a
// valid 1024-edge semantic tree, while interface and map values remain opaque.
func validateSemanticSnapshotKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateTypedJSONTokens(decoder, reflect.TypeOf(semanticSnapshotWire{}), 0); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing semantic snapshot")
		}
		return err
	}
	return nil
}

func validateTypedJSONTokens(decoder *json.Decoder, typ reflect.Type, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Interface || typ.Kind() == reflect.Map {
		return skipJSONToken(decoder, token, depth)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth > maxTranscriptJSONDepth {
		return errors.New("replay transcript JSON nesting exceeds limit")
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		if delim != '[' {
			return nil
		}
		return validateTypedJSONArrayTokens(decoder, typ.Elem(), depth+1)
	case reflect.Struct:
		if delim != '{' {
			return nil
		}
		return validateTypedJSONObjectTokens(decoder, typ, depth+1)
	}
	return skipJSONToken(decoder, token, depth)
}

func validateTypedJSONArrayTokens(decoder *json.Decoder, element reflect.Type, depth int) error {
	for decoder.More() {
		if err := validateTypedJSONTokens(decoder, element, depth); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func validateTypedJSONObjectTokens(decoder *json.Decoder, typ reflect.Type, depth int) error {
	fields := typedJSONFields(typ)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return errors.New("invalid semantic snapshot object key")
		}
		fieldType, exists, err := canonicalTypedJSONField(fields, name)
		if err != nil {
			return err
		}
		if !exists {
			if err := skipJSONValueTokens(decoder, depth); err != nil {
				return err
			}
			continue
		}
		if err := validateTypedJSONTokens(decoder, fieldType, depth); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func canonicalTypedJSONField(fields map[string]reflect.Type, name string) (reflect.Type, bool, error) {
	if fieldType, exists := fields[name]; exists {
		return fieldType, true, nil
	}
	for field := range fields {
		if strings.EqualFold(name, field) {
			return nil, false, fmt.Errorf("noncanonical typed key %q, want %q", name, field)
		}
	}
	return nil, false, nil
}

func skipJSONValueTokens(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	return skipJSONToken(decoder, token, depth)
}

func skipJSONToken(decoder *json.Decoder, token any, depth int) error {
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth > maxTranscriptJSONDepth {
		return errors.New("replay transcript JSON nesting exceeds limit")
	}
	for decoder.More() {
		if delim == '{' {
			if _, err := decoder.Token(); err != nil {
				return err
			}
		}
		if err := skipJSONValueTokens(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

type semanticSnapshotWire struct {
	SchemaVersion string           `json:"schemaVersion"`
	Revision      uint64           `json:"revision"`
	TreeHash      string           `json:"treeHash"`
	Root          semanticNodeWire `json:"root"`
}

type semanticNodeWire struct {
	ID          semantic.NodeID      `json:"id"`
	Generation  uint32               `json:"generation"`
	Role        semantic.Role        `json:"role"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Value       semantic.Value       `json:"value,omitempty"`
	States      []semantic.State     `json:"states,omitempty"`
	Relations   []semantic.Relation  `json:"relations,omitempty"`
	Actions     []semantic.ActionRef `json:"actions,omitempty"`
	Layout      semantic.LayoutSpec  `json:"layout,omitempty"`
	Style       semantic.StyleIntent `json:"style,omitempty"`
	Children    []semanticNodeWire   `json:"children,omitempty"`
	Flags       semantic.Flags       `json:"flags"`
	Metadata    map[string]string    `json:"metadata,omitempty"`
}

func (wire semanticNodeWire) validateRelationWork(limit uint64) error {
	var nodes, relations uint64
	var visit func(semanticNodeWire) error
	visit = func(current semanticNodeWire) error {
		nodes++
		relationCount := uint64(len(current.Relations))
		if relationCount > limit-relations {
			return errors.New("semantic snapshot relation work exceeds limit")
		}
		relations += relationCount
		if relations != 0 && nodes > limit/relations {
			return errors.New("semantic snapshot relation work exceeds limit")
		}
		for _, child := range current.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(wire)
}

func (wire semanticNodeWire) node() (semantic.Node, error) {
	children := make([]semantic.Node, len(wire.Children))
	for i, child := range wire.Children {
		decoded, err := child.node()
		if err != nil {
			return semantic.Node{}, err
		}
		children[i] = decoded
	}
	return semantic.NewNode(semantic.NodeSpec{
		ID: wire.ID, Generation: wire.Generation, Role: wire.Role, Name: wire.Name, Description: wire.Description,
		Value: wire.Value, States: wire.States, Relations: wire.Relations, Actions: wire.Actions, Layout: wire.Layout,
		Style: wire.Style, Children: children, Flags: wire.Flags, Metadata: wire.Metadata,
	})
}

func validateTranscriptProducerRecords(records []Record) error {
	delivery := ""
	for i, record := range records {
		if err := validateInspectorRecord(i, record); err != nil {
			return err
		}
		if err := validateEffectLedger(i, record.Prior, record.Result, record.Event); err != nil {
			return err
		}
		if err := validateProducerTransition(i, record.Prior, record.Result); err != nil {
			return err
		}
		if record.Event.Kind == event.EffectResult {
			if delivery != "" && record.Delivery != delivery {
				return &Divergence{Code: DivergenceDelivery, Index: i, Field: "delivery", Expected: delivery, Actual: record.Delivery}
			}
			delivery = record.Delivery
		}
	}
	return nil
}

func validateEffectLedger(index int, prior, result Digest, accepted event.Event) error {
	if accepted.Kind != event.EffectResult {
		if prior.Hashes.EffectLedger != result.Hashes.EffectLedger {
			return &Divergence{Code: DivergenceSchemaVersion, Index: index, Field: "result.hashes.effectLedger", Expected: "unchanged for non-effect event", Actual: "changed"}
		}
		return nil
	}
	if result.Hashes == prior.Hashes && result.Revision == prior.Revision && result.DiagnosticCount > prior.DiagnosticCount {
		return nil
	}
	expected, err := state.HashString(struct {
		Prior string      `json:"prior,omitempty"`
		Event event.Event `json:"event"`
	}{prior.Hashes.EffectLedger, accepted})
	if err != nil {
		return fmt.Errorf("replay record %d effect ledger: %w", index, err)
	}
	if result.Hashes.EffectLedger != expected {
		return &Divergence{Code: DivergenceEvent, Index: index, Field: "result.hashes.effectLedger", Expected: expected, Actual: result.Hashes.EffectLedger}
	}
	return nil
}

func validateProducerTransition(index int, prior, result Digest) error {
	changed := prior.Hashes.Model != result.Hashes.Model || prior.Hashes.Tree != result.Hashes.Tree || prior.Hashes.Surface != result.Hashes.Surface
	if changed != (result.Revision == prior.Revision+1) {
		return &Divergence{Code: DivergenceRevision, Index: index, Field: "result.revision", Expected: "change exactly with model, tree, or surface hash", Actual: result.Revision}
	}
	if result.Revision == prior.Revision+1 && result.Hashes.Tree == prior.Hashes.Tree {
		return &Divergence{Code: DivergenceTreeHash, Index: index, Field: "result.hashes.tree", Expected: "change with revision", Actual: "unchanged"}
	}
	for _, hash := range []struct {
		field  string
		code   DivergenceCode
		prior  string
		result string
	}{
		{"config", DivergenceConfigHash, prior.Hashes.Config, result.Hashes.Config},
		{"theme", DivergenceThemeHash, prior.Hashes.Theme, result.Hashes.Theme},
		{"capability", DivergenceCapability, prior.Hashes.Capability, result.Hashes.Capability},
	} {
		if hash.prior != hash.result {
			return &Divergence{Code: hash.code, Index: index, Field: "result.hashes." + hash.field, Expected: "unchanged", Actual: "changed"}
		}
	}
	return nil
}

func validateInspectorRecord(index int, record Record) error {
	if record.SchemaVersion != SchemaVersion {
		return &Divergence{Code: DivergenceSchemaVersion, Index: index, Field: "record.schemaVersion", Expected: SchemaVersion, Actual: record.SchemaVersion}
	}
	if record.Event.SchemaVersion != event.SchemaVersion {
		return &Divergence{Code: DivergenceSchemaVersion, Index: index, Field: "event.schemaVersion", Expected: event.SchemaVersion, Actual: record.Event.SchemaVersion}
	}
	if err := record.Event.Validate(); err != nil {
		return fmt.Errorf("replay record %d: %w", index, err)
	}
	if record.Event.Timestamp.Tick != record.Event.Sequence {
		return &Divergence{Code: DivergenceSequence, Index: index, Field: "event.timestamp.tick", Expected: record.Event.Sequence, Actual: record.Event.Timestamp.Tick}
	}
	if err := validateSensitiveTypedPayload(index, record.Event); err != nil {
		return err
	}
	if err := validateDeliveryEvidence(index, record); err != nil {
		return err
	}
	if record.Prior.Sequence == math.MaxUint64 {
		return &Divergence{Code: DivergenceSequence, Index: index, Field: "prior.sequence", Expected: "sequence below maximum", Actual: record.Prior.Sequence}
	}
	if record.Result.Revision < record.Prior.Revision || record.Result.Revision-record.Prior.Revision > 1 {
		return &Divergence{Code: DivergenceRevision, Index: index, Field: "result.revision", Expected: "prior revision or one greater", Actual: record.Result.Revision}
	}
	if record.Result.DiagnosticCount < record.Prior.DiagnosticCount {
		return &Divergence{Code: DivergenceCheckpoint, Index: index, Field: "result.diagnosticCount", Expected: "at least the prior diagnostic count", Actual: record.Result.DiagnosticCount}
	}
	return nil
}

func validateSensitiveTypedPayload(index int, ev event.Event) error {
	var sensitive bool
	switch payload := ev.Payload.(type) {
	case event.ActionInvokedPayload:
		sensitive = payload.Sensitive
	case event.EffectResultPayload:
		sensitive = payload.Sensitive
	}
	if !sensitive {
		return nil
	}
	// Marshal the payload directly: Event's canonical representation redacts it
	// and would hide plaintext from callers of this typed validator.
	raw, err := json.Marshal(ev.Payload)
	if err != nil {
		return fmt.Errorf("replay record %d sensitive payload is invalid", index)
	}
	if err := validateUniqueJSONKeys(raw, 0); err != nil {
		return fmt.Errorf("replay record %d sensitive payload is invalid", index)
	}
	return validateSensitiveRawPayload(index, ev.Kind, raw)
}

func validateDeliveryEvidence(index int, record Record) error {
	if record.Event.Kind != event.EffectResult {
		if record.Delivery != "" || record.CompletionIndex != 0 || record.Event.Meta.CompletionIndex != 0 {
			return fmt.Errorf("replay record %d delivery metadata requires an effect result", index)
		}
		return nil
	}
	if record.Delivery != string(effect.DeclarationOrder) && record.Delivery != string(effect.CompletionOrder) {
		return fmt.Errorf("replay record %d has unsupported effect delivery", index)
	}
	if record.CompletionIndex != record.Event.Meta.CompletionIndex {
		return fmt.Errorf("replay record %d completion index does not match its event", index)
	}
	payload := record.Event.Payload.(event.EffectResultPayload)
	if payload.Lane != record.Event.Meta.Lane {
		return &Divergence{Code: DivergenceEvent, Index: index, Field: "event.meta.lane", Expected: payload.Lane, Actual: record.Event.Meta.Lane}
	}
	return nil
}

// RedactedDivergence returns a structured divergence safe to serialize in
// diagnostics. Sensitive event payloads are replaced with their existing
// event-level redaction representation.
func RedactedDivergence(divergence *Divergence) *Divergence {
	if divergence == nil {
		return nil
	}
	redacted := *divergence
	redacted.Expected = redactDivergenceValue(redacted.Expected)
	redacted.Actual = redactDivergenceValue(redacted.Actual)
	return &redacted
}

func redactDivergenceValue(value any) any {
	eventValue, ok := value.(event.Event)
	if !ok {
		return value
	}
	data, err := eventValue.CanonicalJSON()
	if err != nil {
		return "redacted event unavailable"
	}
	return json.RawMessage(data)
}
