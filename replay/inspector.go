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

	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/state"
)

const (
	// MaxTranscriptBytes bounds an inspector input before JSON decoding.
	MaxTranscriptBytes = 16 << 20
	// MaxTranscriptRecords bounds the amount of saved evidence an inspector accepts.
	MaxTranscriptRecords = 100_000
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
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Transcript{}, fmt.Errorf("decode replay transcript: trailing JSON")
		}
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateUniqueJSONValue(decoder, depth); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}

func validateUniqueJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth > 64 {
		return errors.New("replay transcript JSON nesting exceeds limit")
	}
	switch delim {
	case '{':
		if err := validateTranscriptObject(decoder, depth); err != nil {
			return err
		}
	case '[':
		for decoder.More() {
			if err := validateUniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid replay transcript JSON value")
	}
	_, err = decoder.Token()
	return err
}

func validateTranscriptObject(decoder *json.Decoder, depth int) error {
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
			if err := validateTranscriptRecords(decoder, depth+1); err != nil {
				return err
			}
			continue
		}
		if err := validateUniqueJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateTranscriptRecords(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	if token != json.Delim('[') {
		return errors.New("replay transcript records must be an array")
	}
	if depth > 64 {
		return errors.New("replay transcript JSON nesting exceeds limit")
	}
	for count := 0; decoder.More(); count++ {
		if count >= MaxTranscriptRecords {
			return fmt.Errorf("replay transcript exceeds %d-record limit", MaxTranscriptRecords)
		}
		if err := validateUniqueJSONValue(decoder, depth+1); err != nil {
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
			Kind    event.Kind      `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(record.Event, &eventWire); err != nil {
			return fmt.Errorf("replay record %d event: %w", index, err)
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
// framework-owned wire objects. Checkpoint Model and Tree, action arguments,
// effect values, and diagnostic context remain opaque application data.
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
	if err := compareVersions(-1, transcript.Versions, transcript.Initial.Versions); err != nil {
		return err
	}
	for i, record := range transcript.Records {
		if err := validateInspectorRecord(i, record); err != nil {
			return err
		}
	}
	return Validate(transcript, transcript)
}

func validateInspectorRecord(index int, record Record) error {
	if record.SchemaVersion != SchemaVersion {
		return &Divergence{Code: DivergenceSchemaVersion, Index: index, Field: "record.schemaVersion", Expected: SchemaVersion, Actual: record.SchemaVersion}
	}
	if err := record.Event.Validate(); err != nil {
		return fmt.Errorf("replay record %d: %w", index, err)
	}
	if record.Prior.Sequence == math.MaxUint64 {
		return &Divergence{Code: DivergenceSequence, Index: index, Field: "prior.sequence", Expected: "sequence below maximum", Actual: record.Prior.Sequence}
	}
	if record.Result.Revision < record.Prior.Revision || record.Result.Revision-record.Prior.Revision > 1 {
		return &Divergence{Code: DivergenceRevision, Index: index, Field: "result.revision", Expected: "prior revision or one greater", Actual: record.Result.Revision}
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
