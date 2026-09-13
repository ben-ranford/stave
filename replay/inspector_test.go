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
