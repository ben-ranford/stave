package replay

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/event"
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
