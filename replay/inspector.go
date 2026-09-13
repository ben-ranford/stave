package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// DecodeTranscript decodes one canonical transcript artifact. It rejects
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
	if depth > 64 {
		return errors.New("replay transcript JSON nesting exceeds limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return errors.New("invalid replay transcript JSON value")
	}
	seen := map[string]struct{}{}
	for decoder.More() {
		if delim == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid replay transcript object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate replay transcript key %q", key)
			}
			seen[key] = struct{}{}
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			if err := validateUniqueJSONKeys(trimmed, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
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
	if transcript.Initial.SchemaVersion == "" || transcript.Initial.SessionID != transcript.SessionID {
		return &Divergence{Code: DivergenceSessionID, Index: -1, Field: "initial.sessionId", Expected: transcript.SessionID, Actual: transcript.Initial.SessionID}
	}
	if err := transcript.Initial.VerifyChecksum(); err != nil {
		return &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "initial.checksum", Expected: "valid", Actual: err.Error()}
	}
	if err := compareVersions(-1, transcript.Versions, transcript.Initial.Versions); err != nil {
		return err
	}
	for i, record := range transcript.Records {
		if record.SchemaVersion != SchemaVersion {
			return &Divergence{Code: DivergenceSchemaVersion, Index: i, Field: "record.schemaVersion", Expected: SchemaVersion, Actual: record.SchemaVersion}
		}
		if err := record.Event.Validate(); err != nil {
			return fmt.Errorf("replay record %d: %w", i, err)
		}
	}
	return Validate(transcript, transcript)
}

// RedactedDivergence returns a structured divergence safe to serialize in
// diagnostics. Sensitive event payloads are replaced with their existing
// event-level redaction representation.
func RedactedDivergence(divergence *Divergence) *Divergence {
	if divergence == nil {
		return nil
	}
	copy := *divergence
	copy.Expected = redactDivergenceValue(copy.Expected)
	copy.Actual = redactDivergenceValue(copy.Actual)
	return &copy
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
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return "redacted event unavailable"
	}
	return out
}
