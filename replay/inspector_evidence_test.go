package replay

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/event"
)

func TestValidateTranscriptRejectsLogicalTimeMismatch(t *testing.T) {
	for _, tick := range []uint64{0, 1, 3} {
		transcript := mustTranscript(t)
		transcript.Records[0].Event.Timestamp.Tick = tick
		var divergence *Divergence
		if err := ValidateTranscript(transcript); !errors.As(err, &divergence) || divergence.Field != "event.timestamp.tick" {
			t.Fatalf("timestamp %d: got %v, want logical-time divergence", tick, err)
		}
	}
}

func TestValidateTranscriptRequiresMonotonicDiagnosticCount(t *testing.T) {
	for _, count := range []uint64{0, 1, 2} {
		transcript := mustTranscript(t)
		transcript.Initial.DiagnosticCount = 1
		refreshInspectorCheckpointChecksum(t, &transcript.Initial)
		transcript.Records[0].Prior.DiagnosticCount = 1
		transcript.Records[0].Result.DiagnosticCount = count
		err := ValidateTranscript(transcript)
		if count == 0 {
			var divergence *Divergence
			if !errors.As(err, &divergence) || divergence.Field != "result.diagnosticCount" {
				t.Fatalf("decreasing count: got %v, want diagnostic-count divergence", err)
			}
		} else if err != nil {
			t.Fatalf("monotonic count %d rejected: %v", count, err)
		}
	}
}

func TestValidateTranscriptRejectsTypedSensitivePlaintext(t *testing.T) {
	for _, kind := range []event.Kind{event.ActionInvoked, event.EffectResult} {
		t.Run(string(kind), func(t *testing.T) {
			transcript := mustTranscript(t)
			record := &transcript.Records[0]
			record.Event.Kind = kind
			if kind == event.ActionInvoked {
				record.Event.Payload = event.ActionInvokedPayload{CallID: "call", ActionID: "secret", Sensitive: true}
			} else {
				record.Event.Payload = event.EffectResultPayload{CallID: "call", Status: "ok", Sensitive: true}
				record.Delivery = "declaration_order"
			}
			data, err := transcript.CanonicalJSON()
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), `{"redacted":true,"reason":"sensitive"}`, `{"token":"private-canary"}`, 1))
			var plaintext Transcript
			if err := json.Unmarshal(data, &plaintext); err != nil {
				t.Fatal(err)
			}
			if err := ValidateTranscript(plaintext); err == nil {
				t.Fatal("typed validator accepted sensitive plaintext")
			} else if strings.Contains(err.Error(), "private-canary") {
				t.Fatalf("validator exposed plaintext: %v", err)
			}
		})
	}
}

func TestValidateTranscriptDeliveryEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		kind                    event.Kind
		delivery                string
		recordIndex, eventIndex uint32
		valid                   bool
	}{
		{"declaration", event.EffectResult, "declaration_order", 2, 2, true},
		{"completion", event.EffectResult, "completion_order", 2, 2, true},
		{"index mismatch", event.EffectResult, "completion_order", 1, 2, false},
		{"missing delivery", event.EffectResult, "", 0, 0, false},
		{"unknown delivery", event.EffectResult, "unknown", 0, 0, false},
		{"key delivery", event.Key, "completion_order", 0, 0, false},
		{"key record index", event.Key, "", 1, 0, false},
		{"key event index", event.Key, "", 0, 1, false},
		{"key", event.Key, "", 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transcript := mustTranscript(t)
			record := &transcript.Records[0]
			record.Event.Kind = tc.kind
			if tc.kind == event.EffectResult {
				record.Event.Payload = event.EffectResultPayload{CallID: "call", Status: "ok"}
			}
			record.Delivery = tc.delivery
			record.CompletionIndex = tc.recordIndex
			record.Event.Meta.CompletionIndex = tc.eventIndex
			err := ValidateTranscript(transcript)
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateTranscript() = %v, want valid=%v", err, tc.valid)
			}
		})
	}
}
