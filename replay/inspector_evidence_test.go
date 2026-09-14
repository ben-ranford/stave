package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/state"
)

func TestDecodeTranscriptRejectsOmittedEventSchemaVersion(t *testing.T) {
	transcript := mustTranscript(t)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	omitted := bytes.Replace(data, []byte(`"event":{"schemaVersion":"stave.event/v1",`), []byte(`"event":{`), 1)
	if bytes.Equal(omitted, data) {
		t.Fatal("canonical transcript did not contain an event schema version")
	}
	if _, err := DecodeTranscript(omitted); err == nil {
		t.Fatal("DecodeTranscript() accepted an event without schemaVersion")
	}
}

func TestValidateTranscriptRejectsTypedEventWithoutSchemaVersion(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records[0].Event.SchemaVersion = ""
	if err := ValidateTranscript(transcript); err == nil {
		t.Fatal("ValidateTranscript() accepted a typed event without schemaVersion")
	}
}

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
			if tc.kind == event.EffectResult {
				record.Result.Hashes.EffectLedger = sessionEffectLedger(t, record.Prior.Hashes.EffectLedger, record.Event)
			}
			err := ValidateTranscript(transcript)
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateTranscript() = %v, want valid=%v", err, tc.valid)
			}
		})
	}
}

func TestValidateTranscriptRequiresProducerStateTransitions(t *testing.T) {
	for _, field := range []string{"model", "tree", "surface"} {
		t.Run("changed "+field+" with revision", func(t *testing.T) {
			valid := mustTranscript(t)
			switch field {
			case "model":
				valid.Records[0].Result.Hashes.Model += "-changed"
			case "tree":
				valid.Records[0].Result.Hashes.Tree += "-changed"
			case "surface":
				valid.Records[0].Result.Hashes.Surface += "-changed"
			}
			if field != "tree" {
				valid.Records[0].Result.Hashes.Tree += "-changed"
			}
			valid.Records[0].Result.Revision++
			valid.Records[0].Event.Revision = valid.Records[0].Result.Revision
			if err := ValidateTranscript(valid); err != nil {
				t.Fatalf("ValidateTranscript() rejected a changed %s hash with one revision step: %v", field, err)
			}
		})
	}

	for _, mutate := range []struct {
		name  string
		apply func(*Transcript)
	}{
		{"changed hash without revision", func(transcript *Transcript) { transcript.Records[0].Result.Hashes.Model += "-changed" }},
		{"revision without changed hash", func(transcript *Transcript) {
			transcript.Records[0].Result.Revision++
			transcript.Records[0].Event.Revision = transcript.Records[0].Result.Revision
		}},
		{"revision with model change but retained tree hash", func(transcript *Transcript) {
			transcript.Records[0].Result.Hashes.Model += "-changed"
			transcript.Records[0].Result.Revision++
			transcript.Records[0].Event.Revision = transcript.Records[0].Result.Revision
		}},
		{"config changed", func(transcript *Transcript) { transcript.Records[0].Result.Hashes.Config += "-changed" }},
		{"theme changed", func(transcript *Transcript) { transcript.Records[0].Result.Hashes.Theme += "-changed" }},
		{"capability changed", func(transcript *Transcript) { transcript.Records[0].Result.Hashes.Capability += "-changed" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			transcript := mustTranscript(t)
			mutate.apply(&transcript)
			if err := ValidateTranscript(transcript); err == nil {
				t.Fatal("ValidateTranscript() accepted an impossible producer transition")
			}
		})
	}
}

func TestValidateTranscriptRequiresStableEffectDelivery(t *testing.T) {
	transcript := mustTranscript(t)
	first := &transcript.Records[0]
	first.Event.Kind = event.EffectResult
	first.Event.Payload = event.EffectResultPayload{CallID: "first", Status: "ok"}
	first.Delivery = "declaration_order"
	first.Result.Hashes.EffectLedger = sessionEffectLedger(t, first.Prior.Hashes.EffectLedger, first.Event)
	second := *first
	second.Prior = first.Result
	second.Event.Sequence = first.Result.Sequence + 1
	second.Event.Timestamp.Tick = second.Event.Sequence
	second.Result.Sequence = second.Event.Sequence
	second.Event.Payload = event.EffectResultPayload{CallID: "second", Status: "ok"}
	second.Result.Hashes.EffectLedger = sessionEffectLedger(t, second.Prior.Hashes.EffectLedger, second.Event)
	transcript.Records = append(transcript.Records, second)
	if err := ValidateTranscript(transcript); err != nil {
		t.Fatalf("ValidateTranscript() rejected consistent effect delivery: %v", err)
	}
	transcript.Records[1].Delivery = "completion_order"
	if err := ValidateTranscript(transcript); err == nil {
		t.Fatal("ValidateTranscript() accepted mixed effect delivery")
	}
}

func TestValidateTranscriptKeepsNonEffectLedgerStable(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records[0].Result.Hashes.EffectLedger = "changed"
	var divergence *Divergence
	if err := ValidateTranscript(transcript); !errors.As(err, &divergence) || divergence.Field != "result.hashes.effectLedger" {
		t.Fatalf("non-effect ledger change: got %v, want ledger divergence", err)
	}
	transcript.Records[0].Event.Kind = event.EffectResult
	transcript.Records[0].Event.Payload = event.EffectResultPayload{CallID: "effect", Status: "ok"}
	transcript.Records[0].Delivery = "declaration_order"
	transcript.Records[0].Result.Hashes.EffectLedger = sessionEffectLedger(t, transcript.Records[0].Prior.Hashes.EffectLedger, transcript.Records[0].Event)
	if err := ValidateTranscript(transcript); err != nil {
		t.Fatalf("effect-result ledger change rejected: %v", err)
	}
}

func TestValidateTranscriptRequiresSessionEffectLedger(t *testing.T) {
	transcript := mustTranscript(t)
	record := &transcript.Records[0]
	record.Event.Kind = event.EffectResult
	record.Event.Payload = event.EffectResultPayload{CallID: "effect", Status: "ok"}
	record.Delivery = "declaration_order"
	record.Result.Hashes.EffectLedger = record.Prior.Hashes.EffectLedger
	record.Result.DiagnosticCount = record.Prior.DiagnosticCount + 1
	if err := ValidateTranscript(transcript); err != nil {
		t.Fatalf("ValidateTranscript() rejected an effect result with the rejected-event ledger: %v", err)
	}

	for _, mutate := range []struct {
		name  string
		apply func(*Record)
	}{
		{"without diagnostic", func(record *Record) { record.Result.DiagnosticCount = record.Prior.DiagnosticCount }},
		{"with changed model", func(record *Record) {
			record.Result.Hashes.Model += "-changed"
			record.Result.Revision++
			record.Event.Revision = record.Result.Revision
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			candidate := mustTranscript(t)
			record := &candidate.Records[0]
			record.Event.Kind = event.EffectResult
			record.Event.Payload = event.EffectResultPayload{CallID: "effect", Status: "ok"}
			record.Delivery = "declaration_order"
			record.Result.Hashes.EffectLedger = record.Prior.Hashes.EffectLedger
			record.Result.DiagnosticCount = record.Prior.DiagnosticCount + 1
			mutate.apply(record)
			if err := ValidateTranscript(candidate); err == nil {
				t.Fatal("ValidateTranscript() accepted a retained ledger outside the rejected-event shape")
			}
		})
	}

	record.Result.Hashes.EffectLedger = "changed"
	if err := ValidateTranscript(transcript); err == nil {
		t.Fatal("ValidateTranscript() accepted an arbitrary effect ledger")
	}

	record.Result.Hashes.EffectLedger = sessionEffectLedger(t, record.Prior.Hashes.EffectLedger, record.Event)
	if err := ValidateTranscript(transcript); err != nil {
		t.Fatalf("ValidateTranscript() rejected the session effect ledger: %v", err)
	}
}

func TestValidateTranscriptAcceptsSensitiveEffectLedgerAfterCanonicalRoundTrip(t *testing.T) {
	transcript := mustTranscript(t)
	record := &transcript.Records[0]
	record.Event.Kind = event.EffectResult
	record.Event.Payload = event.EffectResultPayload{CallID: "effect", Status: "ok", Sensitive: true, Value: "private-canary"}
	record.Delivery = "declaration_order"
	record.Result.Hashes.EffectLedger = sessionEffectLedger(t, record.Prior.Hashes.EffectLedger, record.Event)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("private-canary")) {
		t.Fatal("canonical transcript exposed sensitive effect value")
	}
	decoded, err := DecodeTranscript(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTranscript(decoded); err != nil {
		t.Fatalf("ValidateTranscript() rejected canonical sensitive effect ledger: %v", err)
	}
}

func sessionEffectLedger(t *testing.T, prior string, accepted event.Event) string {
	t.Helper()
	ledger, err := state.HashString(struct {
		Prior string      `json:"prior,omitempty"`
		Event event.Event `json:"event"`
	}{prior, accepted})
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func TestValidateTranscriptBindsInitialCapabilityHash(t *testing.T) {
	for _, withRecords := range []bool{false, true} {
		transcript := mustTranscript(t)
		if !withRecords {
			transcript.Records = nil
		}
		if err := ValidateTranscript(transcript); err != nil {
			t.Fatalf("valid checkpoint rejected: %v", err)
		}
		transcript.Initial.Hashes.Capability = "unrelated"
		refreshInspectorCheckpointChecksum(t, &transcript.Initial)
		if withRecords {
			transcript.Records[0].Prior.Hashes.Capability = "unrelated"
			transcript.Records[0].Result.Hashes.Capability = "unrelated"
		}
		var divergence *Divergence
		if err := ValidateTranscript(transcript); !errors.As(err, &divergence) || divergence.Field != "initial.hashes.capability" {
			t.Fatalf("records=%v: got %v, want capability divergence", withRecords, err)
		}
	}
}
