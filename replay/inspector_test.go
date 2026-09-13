package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
)

func TestDecodeTranscriptRoundTripAndRejectsInvalidArtifacts(t *testing.T) {
	transcript := mustTranscript(t)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTranscript(data)
	if err != nil {
		t.Fatalf("DecodeTranscript() error = %v", err)
	}
	if err := Validate(transcript, decoded); err != nil {
		t.Fatalf("decoded transcript changed: %v", err)
	}

	cases := []struct {
		name string
		data []byte
	}{
		{"unknown field", append(append([]byte(nil), data[:len(data)-1]...), []byte(`,"unexpected":true}`)...)},
		{"duplicate root key", append(append([]byte(nil), data[:len(data)-1]...), []byte(`,"sessionId":"other"}`)...)},
		{"duplicate nested key", []byte(`{"schemaVersion":"stave.replay/v1","sessionId":"s","versions":{"nodeIdAlgorithm":"x","nodeIdAlgorithm":"y"}}`)},
		{"unknown schema", []byte(`{"schemaVersion":"stave.replay/v99"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeTranscript(tc.data); err == nil {
				t.Fatal("DecodeTranscript() accepted invalid artifact")
			}
		})
	}
	if _, err := DecodeTranscript(append(data, []byte(" {}")...)); err == nil {
		t.Fatal("DecodeTranscript() accepted trailing JSON")
	}
}

func TestValidateTranscriptBoundsRecords(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records = make([]Record, MaxTranscriptRecords+1)
	if err := ValidateTranscript(transcript); err == nil || !strings.Contains(err.Error(), "record limit") {
		t.Fatalf("ValidateTranscript() error = %v, want record limit", err)
	}
}

func TestRedactedDivergenceDoesNotSerializeSensitiveEventPayload(t *testing.T) {
	divergence := &Divergence{Code: DivergenceEvent, Expected: event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.ActionInvoked,
		Payload: event.ActionInvokedPayload{
			CallID:    "call-1",
			ActionID:  "example.secret",
			Arguments: map[string]any{"token": "do-not-disclose"},
			Sensitive: true,
		},
	}}
	data, err := json.Marshal(RedactedDivergence(divergence))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "do-not-disclose") {
		t.Fatalf("redacted divergence exposed sensitive payload: %s", data)
	}
	if !strings.Contains(string(data), "redacted") {
		t.Fatalf("redacted divergence omitted redaction marker: %s", data)
	}
}

func TestValidateTranscriptRejectsUnsupportedCheckpointSchema(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Initial.SchemaVersion = "stave.checkpoint/v99"
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	if err := ValidateTranscript(transcript); err == nil {
		t.Fatal("unsupported checkpoint schema accepted")
	}
}

func TestDecodeTranscriptBindsInitialSemanticSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*Transcript)
	}{
		{"invalid snapshot", func(transcript *Transcript) {
			tree := transcript.Initial.Tree.(map[string]any)
			tree["treeHash"] = "tampered"
		}},
		{"revision mismatch", func(transcript *Transcript) {
			transcript.Initial.Tree = snapshotWire(t, transcript.Initial.Revision+1, "same-tree", semantic.Value{})
		}},
		{"tree hash mismatch", func(transcript *Transcript) {
			transcript.Initial.Tree = snapshotWire(t, transcript.Initial.Revision, "different-tree", semantic.Value{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transcript := mustTranscript(t)
			tc.apply(&transcript)
			refreshInspectorCheckpointChecksum(t, &transcript.Initial)
			data, err := transcript.CanonicalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeTranscript(data); err == nil {
				t.Fatal("DecodeTranscript() accepted an invalid initial semantic snapshot")
			}
		})
	}
}

func TestDecodeTranscriptAcceptsRedactedInitialSemanticSnapshot(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Initial.Tree = snapshotWire(t, transcript.Initial.Revision, "redacted-tree", semantic.SecretValue())
	var snapshot semantic.Snapshot
	if err := json.Unmarshal(mustJSON(t, transcript.Initial.Tree), &snapshot); err != nil {
		t.Fatal(err)
	}
	transcript.Initial.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Prior.Hashes.Tree = snapshot.TreeHash
	transcript.Records[0].Result.Hashes.Tree = snapshot.TreeHash
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTranscript(data)
	if err != nil {
		t.Fatalf("DecodeTranscript() rejected a redacted initial snapshot: %v", err)
	}
	if err := ValidateTranscript(decoded); err != nil {
		t.Fatalf("ValidateTranscript() rejected a redacted initial snapshot: %v", err)
	}
}

func TestDecodeTranscriptAcceptsSeededCheckpointHistory(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Initial.Hashes.EffectLedger = "seeded-ledger"
	transcript.Initial.Hashes.Declarations = "seeded-declarations"
	transcript.Records[0].Prior.Hashes.EffectLedger = transcript.Initial.Hashes.EffectLedger
	transcript.Records[0].Prior.Hashes.Declarations = transcript.Initial.Hashes.Declarations
	transcript.Records[0].Result.Hashes.EffectLedger = transcript.Initial.Hashes.EffectLedger
	transcript.Records[0].Result.Hashes.Declarations = transcript.Initial.Hashes.Declarations
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTranscript(data)
	if err != nil {
		t.Fatalf("DecodeTranscript() rejected a seeded checkpoint: %v", err)
	}
	if err := ValidateTranscript(decoded); err != nil {
		t.Fatalf("ValidateTranscript() rejected a seeded checkpoint: %v", err)
	}
}

func snapshotWire(t *testing.T, revision uint64, entity string, value semantic.Value) any {
	t.Helper()
	root, err := semantic.NewNode(semantic.NodeSpec{
		Key:        &semantic.NodeKey{AppNamespace: "stave", View: "replay", Kind: "root", Entity: entity, Slot: "main"},
		Generation: 1,
		Role:       "application",
		Name:       entity,
		Value:      value,
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(revision, root)
	if err != nil {
		t.Fatal(err)
	}
	var wire any
	if err := json.Unmarshal(mustJSON(t, tree.Snapshot()), &wire); err != nil {
		t.Fatal(err)
	}
	return wire
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func refreshInspectorCheckpointChecksum(t *testing.T, checkpoint *state.Checkpoint) {
	t.Helper()
	data, err := checkpoint.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	index := bytes.LastIndex(data, []byte(`,"checksum":`))
	if index < 0 {
		t.Fatal("checkpoint checksum field missing")
	}
	unsigned := append(append([]byte(nil), data[:index]...), '}')
	digest := sha256.Sum256(unsigned)
	checkpoint.Checksum = hex.EncodeToString(digest[:])
	if err := checkpoint.VerifyChecksum(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTranscriptRejectsInvalidRevisionSteps(t *testing.T) {
	for _, revision := range []uint64{0, 100} {
		transcript := mustTranscript(t)
		transcript.Records[0].Event.Revision = revision
		transcript.Records[0].Result.Revision = revision
		var divergence *Divergence
		if err := ValidateTranscript(transcript); !errors.As(err, &divergence) || divergence.Code != DivergenceRevision {
			t.Fatalf("revision %d error = %v, want revision divergence", revision, err)
		}
	}
}

func TestRedactedDivergencePreservesLargePayloadNumbers(t *testing.T) {
	const exact = "9007199254740993"
	value := event.Event{SchemaVersion: event.SchemaVersion, Kind: event.ActionInvoked,
		Payload: event.ActionInvokedPayload{CallID: "call-1", ActionID: "example.number", Arguments: map[string]any{"number": json.Number(exact)}}}
	data, err := json.Marshal(RedactedDivergence(&Divergence{Code: DivergenceEvent, Actual: value}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(exact)) {
		t.Fatalf("divergence rounded payload number: %s", data)
	}
}

func TestValidateInspectorRecordAllowsUnchangedMaximumRevision(t *testing.T) {
	record := mustTranscript(t).Records[0]
	record.Prior.Revision = ^uint64(0)
	record.Result.Revision = record.Prior.Revision
	record.Event.Revision = record.Prior.Revision
	if err := validateInspectorRecord(0, record); err != nil {
		t.Fatalf("unchanged maximum revision rejected: %v", err)
	}
}

func TestDecodeTranscriptRejectsCaseFoldedTypedAliases(t *testing.T) {
	transcript := mustTranscript(t)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		old  []byte
		new  []byte
	}{
		{"transcript", []byte(`"schemaVersion":"stave.replay/v1"`), []byte(`"SchemaVersion":"stave.replay/v1"`)},
		{"record", []byte(`"schemaVersion":"stave.replay/v1","event"`), []byte(`"SchemaVersion":"stave.replay/v1","event"`)},
		{"event", []byte(`"sequence":2,"revision":1,"timestamp"`), []byte(`"Sequence":2,"revision":1,"timestamp"`)},
		{"checkpoint", []byte(`"checksum"`), []byte(`"Checksum"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aliased := bytes.Replace(data, tc.old, tc.new, 1)
			if bytes.Equal(aliased, data) {
				t.Fatal("test fixture did not replace a typed key")
			}
			if _, err := DecodeTranscript(aliased); err == nil {
				t.Fatalf("DecodeTranscript() accepted a case-folded %s typed alias", tc.name)
			}
		})
	}
}

func TestDecodeTranscriptPreservesApplicationModelKeyCase(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Initial.Model = map[string]any{"APIKey": "value"}
	transcript.Records[0].Event = event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.ActionInvoked,
		Sequence:      transcript.Records[0].Prior.Sequence + 1,
		Revision:      transcript.Records[0].Result.Revision,
		Timestamp:     event.LogicalTime{Tick: transcript.Records[0].Prior.Sequence + 1},
		Payload: event.ActionInvokedPayload{
			CallID: "call-1", ActionID: "example.action", Arguments: map[string]any{"APIKey": "value"},
		},
	}
	refreshInspectorCheckpointChecksum(t, &transcript.Initial)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTranscript(data)
	if err != nil {
		t.Fatalf("DecodeTranscript() error = %v", err)
	}
	model, ok := decoded.Initial.Model.(map[string]any)
	if !ok || model["APIKey"] != "value" {
		t.Fatalf("application model key case changed: %#v", decoded.Initial.Model)
	}
	payload, ok := decoded.Records[0].Event.Payload.(event.ActionInvokedPayload)
	arguments, argumentsOK := payload.Arguments.(map[string]any)
	if !ok || !argumentsOK || arguments["APIKey"] != "value" {
		t.Fatalf("application action argument key case changed: %#v", decoded.Records[0].Event.Payload)
	}
}

func TestDecodeTranscriptRejectsUnsanitizedSensitivePayloadBeforeClone(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records[0].Event = event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.ActionInvoked,
		Sequence:      transcript.Records[0].Prior.Sequence + 1,
		Revision:      transcript.Records[0].Result.Revision,
		Timestamp:     event.LogicalTime{Tick: transcript.Records[0].Prior.Sequence + 1},
		Payload: event.ActionInvokedPayload{
			CallID: "call-1", ActionID: "example.secret", Sensitive: true,
		},
	}
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), data...)
	data = bytes.Replace(data, []byte(`"arguments":{"redacted":true,"reason":"sensitive"}`), []byte(`"arguments":{"token":"do-not-disclose"}`), 1)
	if bytes.Equal(data, original) {
		t.Fatal("test fixture did not replace the redaction representation")
	}
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted an unsanitized sensitive action payload")
	} else if strings.Contains(err.Error(), "do-not-disclose") {
		t.Fatalf("DecodeTranscript() exposed sensitive payload: %v", err)
	}
}

func TestDecodeTranscriptAcceptsAlreadyRedactedSensitivePayload(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records[0].Event = event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.EffectResult,
		Sequence:      transcript.Records[0].Prior.Sequence + 1,
		Revision:      transcript.Records[0].Result.Revision,
		Timestamp:     event.LogicalTime{Tick: transcript.Records[0].Prior.Sequence + 1},
		Payload: event.EffectResultPayload{
			CallID: "call-1", Status: "ok", Sensitive: true,
		},
	}
	transcript.Records[0].Delivery = "declaration_order"
	transcript.Records[0].Result.Hashes.EffectLedger = sessionEffectLedger(t, transcript.Records[0].Prior.Hashes.EffectLedger, transcript.Records[0].Event)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTranscript(data); err != nil {
		t.Fatalf("DecodeTranscript() rejected an already redacted sensitive payload: %v", err)
	}
}

func TestValidateInspectorRecordRejectsSequenceWrap(t *testing.T) {
	record := mustTranscript(t).Records[0]
	record.Prior.Sequence = ^uint64(0)
	record.Event.Sequence = 0
	record.Result.Sequence = 0
	var divergence *Divergence
	if err := validateInspectorRecord(0, record); !errors.As(err, &divergence) || divergence.Code != DivergenceSequence {
		t.Fatalf("sequence wrap error = %v, want sequence divergence", err)
	}
}

func TestDecodeTranscriptRejectsSensitiveRedactionWithExtraFields(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records[0].Event = event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.ActionInvoked,
		Sequence:      transcript.Records[0].Prior.Sequence + 1,
		Revision:      transcript.Records[0].Result.Revision,
		Timestamp:     event.LogicalTime{Tick: transcript.Records[0].Prior.Sequence + 1},
		Payload: event.ActionInvokedPayload{
			CallID: "call-1", ActionID: "example.secret", Sensitive: true,
		},
	}
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), data...)
	data = bytes.Replace(data, []byte(`"arguments":{"redacted":true,"reason":"sensitive"}`), []byte(`"arguments":{"redacted":true,"reason":"sensitive","secret":"do-not-disclose"}`), 1)
	if bytes.Equal(data, original) {
		t.Fatal("test fixture did not add the plaintext field")
	}
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted a sensitive redaction with plaintext extras")
	} else if strings.Contains(err.Error(), "do-not-disclose") {
		t.Fatalf("DecodeTranscript() exposed sensitive payload: %v", err)
	}
}

func TestDecodeTranscriptRejectsCaseFoldedEventMetadataAlias(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records[0].Event.Meta.Source = "test"
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), data...)
	data = bytes.Replace(data, []byte(`"meta":{"source":"test"}`), []byte(`"meta":{"Source":"test"}`), 1)
	if bytes.Equal(data, original) {
		t.Fatal("test fixture did not replace event metadata")
	}
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted a case-folded event metadata alias")
	}
}

func TestDecodeTranscriptRejectsUnknownSensitivePayloadFieldWithoutLeakingValue(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records[0].Event = event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.ActionInvoked,
		Sequence:      transcript.Records[0].Prior.Sequence + 1,
		Revision:      transcript.Records[0].Result.Revision,
		Timestamp:     event.LogicalTime{Tick: transcript.Records[0].Prior.Sequence + 1},
		Payload: event.ActionInvokedPayload{
			CallID: "call-1", ActionID: "example.secret", Sensitive: true,
		},
	}
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), data...)
	data = bytes.Replace(data, []byte(`"sensitive":true}`), []byte(`"sensitive":true,"unexpected":"do-not-disclose"}`), 1)
	if bytes.Equal(data, original) {
		t.Fatal("test fixture did not add the unknown sensitive field")
	}
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted an unknown sensitive payload field")
	} else if strings.Contains(err.Error(), "do-not-disclose") {
		t.Fatalf("DecodeTranscript() exposed sensitive payload: %v", err)
	}
}
