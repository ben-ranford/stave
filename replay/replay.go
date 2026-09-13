package replay

import (
	"context"
	"fmt"

	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/internal/canonical"
	"github.com/ben-ranford/stave/state"
)

const SchemaVersion = "stave.replay/v1"

type Digest struct {
	Sequence        uint64       `json:"sequence"`
	Revision        uint64       `json:"revision"`
	Hashes          state.Hashes `json:"hashes"`
	DiagnosticCount uint64       `json:"diagnosticCount,omitempty"`
}

type Record struct {
	SchemaVersion   string      `json:"schemaVersion"`
	Event           event.Event `json:"event"`
	Prior           Digest      `json:"prior"`
	Result          Digest      `json:"result"`
	Delivery        string      `json:"delivery,omitempty"`
	CompletionIndex uint32      `json:"completionIndex,omitempty"`
}

type Transcript struct {
	SchemaVersion string           `json:"schemaVersion"`
	SessionID     string           `json:"sessionId"`
	Versions      state.Versions   `json:"versions"`
	Initial       state.Checkpoint `json:"initial"`
	Records       []Record         `json:"records"`
}

// ApplyFunc restores the supplied checkpoint, applies one accepted input or
// recorded effect result, and returns the resulting checkpoint. Implementations
// must treat the checkpoint and event as immutable and must fail closed when a
// version or digest cannot be understood.
type ApplyFunc func(context.Context, state.Checkpoint, event.Event) (state.Checkpoint, error)

// Execute replays every recorded event from the transcript's initial
// checkpoint. It deliberately requires an application-owned restore/apply
// function: model and semantic tree types are application-defined and cannot
// safely be reconstructed from an untyped checkpoint by the framework.
func Execute(ctx context.Context, transcript Transcript, apply ApplyFunc) (Transcript, error) {
	if apply == nil {
		return Transcript{}, fmt.Errorf("replay apply function is required")
	}
	if transcript.SchemaVersion != SchemaVersion {
		return Transcript{}, &Divergence{Code: DivergenceSchemaVersion, Index: -1, Field: "schemaVersion", Expected: SchemaVersion, Actual: transcript.SchemaVersion}
	}
	if err := state.ValidateSupportedVersions(transcript.Versions); err != nil {
		return Transcript{}, &Divergence{Code: DivergenceSchemaVersion, Index: -1, Field: "versions", Expected: "supported runtime versions", Actual: transcript.Versions}
	}
	if err := transcript.Initial.VerifyChecksum(); err != nil {
		return Transcript{}, &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "initial.checksum", Expected: "valid", Actual: err.Error()}
	}
	if transcript.Initial.SchemaVersion == "" || transcript.Initial.SessionID != transcript.SessionID {
		return Transcript{}, &Divergence{Code: DivergenceSessionID, Index: -1, Field: "initial.sessionId", Expected: transcript.SessionID, Actual: transcript.Initial.SessionID}
	}
	if err := compareVersions(-1, transcript.Versions, transcript.Initial.Versions); err != nil {
		return Transcript{}, err
	}
	actual := NewTranscript(transcript.SessionID, transcript.Versions, transcript.Initial)
	checkpoint := transcript.Initial
	for i, want := range transcript.Records {
		if want.SchemaVersion != SchemaVersion {
			return actual, &Divergence{Code: DivergenceSchemaVersion, Index: i, Field: "record.schemaVersion", Expected: SchemaVersion, Actual: want.SchemaVersion}
		}
		if err := want.Event.Validate(); err != nil {
			return Transcript{}, fmt.Errorf("replay record %d: %w", i, err)
		}
		prior := Digest{Sequence: checkpoint.Sequence, Revision: checkpoint.Revision, Hashes: checkpoint.Hashes, DiagnosticCount: checkpoint.DiagnosticCount}
		if err := compareDigestValue(i, "prior", want.Prior, prior); err != nil {
			return actual, err
		}
		if want.Event.Sequence != want.Prior.Sequence+1 || want.Result.Sequence != want.Event.Sequence {
			return actual, &Divergence{Code: DivergenceSequence, Index: i, Field: "event.sequence", Expected: want.Prior.Sequence + 1, Actual: want.Event.Sequence}
		}
		if want.Result.Revision < want.Prior.Revision || want.Result.Revision > want.Prior.Revision+1 || want.Event.Revision != want.Result.Revision {
			return actual, &Divergence{Code: DivergenceRevision, Index: i, Field: "event.revision", Expected: want.Result.Revision, Actual: want.Event.Revision}
		}
		select {
		case <-ctx.Done():
			return Transcript{}, ctx.Err()
		default:
		}
		next, err := apply(ctx, checkpoint, want.Event)
		if err != nil {
			return Transcript{}, fmt.Errorf("replay record %d: %w", i, err)
		}
		if next.SchemaVersion != checkpoint.SchemaVersion || next.SessionID != transcript.SessionID {
			return Transcript{}, &Divergence{Code: DivergenceSessionID, Index: i, Field: "result.sessionId", Expected: transcript.SessionID, Actual: next.SessionID}
		}
		if err := next.VerifyChecksum(); err != nil {
			return actual, &Divergence{Code: DivergenceCheckpoint, Index: i, Field: "result.checksum", Expected: "valid", Actual: err.Error()}
		}
		if err := compareVersions(i, transcript.Versions, next.Versions); err != nil {
			return actual, err
		}
		result := Digest{Sequence: next.Sequence, Revision: next.Revision, Hashes: next.Hashes, DiagnosticCount: next.DiagnosticCount}
		actual.Append(Record{Event: want.Event, Prior: prior, Result: result, Delivery: want.Delivery, CompletionIndex: want.CompletionIndex})
		if err := compareDigestValue(i, "result", want.Result, result); err != nil {
			return actual, err
		}
		checkpoint = next
	}
	if err := Validate(transcript, actual); err != nil {
		return actual, err
	}
	return actual, nil
}

type DivergenceCode string

const (
	DivergenceSchemaVersion DivergenceCode = "SCHEMA_VERSION_MISMATCH"
	DivergenceSessionID     DivergenceCode = "SESSION_ID_MISMATCH"
	DivergenceNodeID        DivergenceCode = "NODE_ID_ALGORITHM_MISMATCH"
	DivergenceWidthPolicy   DivergenceCode = "WIDTH_POLICY_MISMATCH"
	DivergenceEvent         DivergenceCode = "EVENT_MISMATCH"
	DivergenceSequence      DivergenceCode = "SEQUENCE_MISMATCH"
	DivergenceRevision      DivergenceCode = "REVISION_MISMATCH"
	DivergenceConfigHash    DivergenceCode = "CONFIG_HASH_MISMATCH"
	DivergenceThemeHash     DivergenceCode = "THEME_HASH_MISMATCH"
	DivergenceCapability    DivergenceCode = "CAPABILITY_HASH_MISMATCH"
	DivergenceModelHash     DivergenceCode = "MODEL_HASH_MISMATCH"
	DivergenceTreeHash      DivergenceCode = "TREE_HASH_MISMATCH"
	DivergenceSurfaceHash   DivergenceCode = "SURFACE_HASH_MISMATCH"
	DivergenceRecordCount   DivergenceCode = "RECORD_COUNT_MISMATCH"
	DivergenceDelivery      DivergenceCode = "DELIVERY_ORDER_MISMATCH"
	DivergenceCheckpoint    DivergenceCode = "CHECKPOINT_MISMATCH"
)

type RemapMetadata struct {
	NodeID           string   `json:"nodeId,omitempty"`
	Generation       uint32   `json:"generation,omitempty"`
	ObservedRevision uint64   `json:"observedRevision,omitempty"`
	Hints            []string `json:"hints,omitempty"`
}

type Divergence struct {
	Code     DivergenceCode `json:"code"`
	Index    int            `json:"index"`
	Field    string         `json:"field"`
	Expected any            `json:"expected,omitempty"`
	Actual   any            `json:"actual,omitempty"`
	Remap    *RemapMetadata `json:"remap,omitempty"`
}

func (d *Divergence) Error() string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%s index=%d field=%s expected=%v actual=%v", d.Code, d.Index, d.Field, d.Expected, d.Actual)
}

func NewTranscript(sessionID string, versions state.Versions, initial state.Checkpoint) Transcript {
	return Transcript{
		SchemaVersion: SchemaVersion,
		SessionID:     sessionID,
		Versions:      versions,
		Initial:       initial,
	}
}

func DigestFromState[M any](snapshot state.State[M]) Digest {
	return Digest{
		Sequence:        snapshot.Sequence,
		Revision:        snapshot.Revision,
		Hashes:          snapshot.Hashes,
		DiagnosticCount: snapshot.DiagnosticCount,
	}
}

func (t *Transcript) Append(record Record) {
	record.SchemaVersion = SchemaVersion
	t.Records = append(t.Records, record)
}

func (t Transcript) Clone() (Transcript, error) {
	data, err := t.CanonicalJSON()
	if err != nil {
		return Transcript{}, err
	}
	var out Transcript
	if err := canonical.Decode(data, &out); err != nil {
		return Transcript{}, err
	}
	return out, nil
}

func (t Transcript) CanonicalJSON() ([]byte, error) {
	if t.SchemaVersion == "" {
		t.SchemaVersion = SchemaVersion
	}
	return canonical.Encode(t)
}

func Encode(record Record) ([]byte, error) {
	if record.SchemaVersion == "" {
		record.SchemaVersion = SchemaVersion
	}
	return canonical.Encode(record)
}

func Validate(expected, actual Transcript) error {
	if expected.SchemaVersion != actual.SchemaVersion {
		return &Divergence{Code: DivergenceSchemaVersion, Index: -1, Field: "schemaVersion", Expected: expected.SchemaVersion, Actual: actual.SchemaVersion}
	}
	if expected.SessionID != actual.SessionID {
		return &Divergence{Code: DivergenceSessionID, Index: -1, Field: "sessionId", Expected: expected.SessionID, Actual: actual.SessionID}
	}
	if err := expected.Initial.VerifyChecksum(); err != nil {
		return &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "expected.initial.checksum", Expected: "valid", Actual: err.Error()}
	}
	if err := actual.Initial.VerifyChecksum(); err != nil {
		return &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "actual.initial.checksum", Expected: "valid", Actual: err.Error()}
	}
	if err := compareVersions(-1, expected.Versions, actual.Versions); err != nil {
		return err
	}
	if expected.Initial.Checksum != actual.Initial.Checksum {
		return &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "initial.checksum", Expected: expected.Initial.Checksum, Actual: actual.Initial.Checksum}
	}
	if expected.Initial.Sequence != actual.Initial.Sequence {
		return &Divergence{Code: DivergenceSequence, Index: -1, Field: "initial.sequence", Expected: expected.Initial.Sequence, Actual: actual.Initial.Sequence}
	}
	if expected.Initial.Revision != actual.Initial.Revision {
		return &Divergence{Code: DivergenceRevision, Index: -1, Field: "initial.revision", Expected: expected.Initial.Revision, Actual: actual.Initial.Revision}
	}
	if expected.Initial.DiagnosticCount != actual.Initial.DiagnosticCount {
		return &Divergence{Code: DivergenceCheckpoint, Index: -1, Field: "initial.diagnosticCount", Expected: expected.Initial.DiagnosticCount, Actual: actual.Initial.DiagnosticCount}
	}
	if err := compareDigest(-1, "initial.hashes", expected.Initial.Hashes, actual.Initial.Hashes); err != nil {
		return err
	}
	if len(expected.Records) != len(actual.Records) {
		return &Divergence{Code: DivergenceRecordCount, Index: min(len(expected.Records), len(actual.Records)), Field: "records", Expected: len(expected.Records), Actual: len(actual.Records)}
	}
	for i := range expected.Records {
		want := expected.Records[i]
		got := actual.Records[i]
		if want.SchemaVersion != got.SchemaVersion {
			return &Divergence{Code: DivergenceSchemaVersion, Index: i, Field: "record.schemaVersion", Expected: want.SchemaVersion, Actual: got.SchemaVersion}
		}
		if !event.Equal(want.Event, got.Event) {
			remap := extractRemap(want.Event)
			if remap == nil {
				remap = extractRemap(got.Event)
			}
			return &Divergence{
				Code:     DivergenceEvent,
				Index:    i,
				Field:    "event",
				Expected: want.Event,
				Actual:   got.Event,
				Remap:    remap,
			}
		}
		if want.Delivery != got.Delivery || want.CompletionIndex != got.CompletionIndex {
			return &Divergence{
				Code:     DivergenceDelivery,
				Index:    i,
				Field:    "delivery",
				Expected: fmt.Sprintf("%s/%d", want.Delivery, want.CompletionIndex),
				Actual:   fmt.Sprintf("%s/%d", got.Delivery, got.CompletionIndex),
			}
		}
		if err := compareDigestValue(i, "prior", want.Prior, got.Prior); err != nil {
			return err
		}
		if i == 0 {
			initial := Digest{Sequence: expected.Initial.Sequence, Revision: expected.Initial.Revision, Hashes: expected.Initial.Hashes, DiagnosticCount: expected.Initial.DiagnosticCount}
			if err := compareDigestValue(i, "prior.initial", initial, want.Prior); err != nil {
				return err
			}
		} else {
			prev := expected.Records[i-1].Result
			if err := compareDigestValue(i, "prior.chain", prev, want.Prior); err != nil {
				return err
			}
			actualPrev := actual.Records[i-1].Result
			if err := compareDigestValue(i, "actual.prior.chain", actualPrev, got.Prior); err != nil {
				return err
			}
		}
		if want.Event.Sequence != want.Prior.Sequence+1 || want.Event.Sequence != want.Result.Sequence || want.Event.Revision != want.Result.Revision {
			return &Divergence{Code: DivergenceSequence, Index: i, Field: "event.sequenceRevision", Expected: fmt.Sprintf("%d/%d", want.Result.Sequence, want.Result.Revision), Actual: fmt.Sprintf("%d/%d", want.Event.Sequence, want.Event.Revision)}
		}
		if want.Result.Sequence != got.Result.Sequence {
			return &Divergence{Code: DivergenceSequence, Index: i, Field: "result.sequence", Expected: want.Result.Sequence, Actual: got.Result.Sequence}
		}
		if want.Result.Revision != got.Result.Revision {
			return &Divergence{Code: DivergenceRevision, Index: i, Field: "result.revision", Expected: want.Result.Revision, Actual: got.Result.Revision}
		}
		if err := compareDigestValue(i, "result", want.Result, got.Result); err != nil {
			return err
		}
	}
	return nil
}

func compareDigestValue(index int, field string, expected, actual Digest) error {
	if expected.Sequence != actual.Sequence {
		return &Divergence{Code: DivergenceSequence, Index: index, Field: field + ".sequence", Expected: expected.Sequence, Actual: actual.Sequence}
	}
	if expected.Revision != actual.Revision {
		return &Divergence{Code: DivergenceRevision, Index: index, Field: field + ".revision", Expected: expected.Revision, Actual: actual.Revision}
	}
	if expected.DiagnosticCount != actual.DiagnosticCount {
		return &Divergence{Code: DivergenceCheckpoint, Index: index, Field: field + ".diagnosticCount", Expected: expected.DiagnosticCount, Actual: actual.DiagnosticCount}
	}
	return compareDigest(index, field, expected.Hashes, actual.Hashes)
}

func compareVersions(index int, a, b state.Versions) error {
	fields := []struct {
		name      string
		code      DivergenceCode
		want, got string
	}{
		{"nodeIdAlgorithm", DivergenceNodeID, a.NodeIDAlgorithm, b.NodeIDAlgorithm},
		{"widthPolicy", DivergenceWidthPolicy, a.WidthPolicy, b.WidthPolicy},
		{"checkpointSchema", DivergenceSchemaVersion, a.CheckpointSchema, b.CheckpointSchema},
		{"semanticSchema", DivergenceSchemaVersion, a.SemanticSchema, b.SemanticSchema},
		{"eventSchema", DivergenceSchemaVersion, a.EventSchema, b.EventSchema},
		{"replaySchema", DivergenceSchemaVersion, a.ReplaySchema, b.ReplaySchema},
		{"jsonPolicy", DivergenceSchemaVersion, a.JSONPolicy, b.JSONPolicy},
		{"keymapSchema", DivergenceSchemaVersion, a.KeymapSchema, b.KeymapSchema},
		{"moduleSchema", DivergenceSchemaVersion, a.ModuleSchema, b.ModuleSchema},
		{"protocolSchema", DivergenceSchemaVersion, a.ProtocolSchema, b.ProtocolSchema},
		{"canonicalSchema", DivergenceSchemaVersion, a.CanonicalSchema, b.CanonicalSchema},
		{"effectSchema", DivergenceSchemaVersion, a.EffectSchema, b.EffectSchema},
		{"eventAlgorithm", DivergenceSchemaVersion, a.EventAlgorithm, b.EventAlgorithm},
		{"checkpointVersion", DivergenceSchemaVersion, a.CheckpointSchemaVersion, b.CheckpointSchemaVersion},
	}
	for _, f := range fields {
		if f.want != f.got {
			return &Divergence{Code: f.code, Index: index, Field: "versions." + f.name, Expected: f.want, Actual: f.got}
		}
	}
	return nil
}

func compareDigest(index int, fieldPrefix string, expected, actual state.Hashes) error {
	for _, pair := range []struct {
		field string
		code  DivergenceCode
		want  string
		got   string
	}{
		{"config", DivergenceConfigHash, expected.Config, actual.Config},
		{"theme", DivergenceThemeHash, expected.Theme, actual.Theme},
		{"capability", DivergenceCapability, expected.Capability, actual.Capability},
		{"model", DivergenceModelHash, expected.Model, actual.Model},
		{"tree", DivergenceTreeHash, expected.Tree, actual.Tree},
		{"surface", DivergenceSurfaceHash, expected.Surface, actual.Surface},
		{"effectLedger", DivergenceSchemaVersion, expected.EffectLedger, actual.EffectLedger},
		{"declarations", DivergenceSchemaVersion, expected.Declarations, actual.Declarations},
	} {
		if pair.want != pair.got {
			return &Divergence{
				Code:     pair.code,
				Index:    index,
				Field:    fieldPrefix + "." + pair.field,
				Expected: pair.want,
				Actual:   pair.got,
			}
		}
	}
	return nil
}

func extractRemap(ev event.Event) *RemapMetadata {
	payload, ok := ev.Payload.(event.ActionInvokedPayload)
	if !ok || payload.Target == nil {
		return nil
	}
	return &RemapMetadata{
		NodeID:           payload.Target.NodeID,
		Generation:       payload.Target.Generation,
		ObservedRevision: payload.Target.ObservedRevision,
		Hints:            []string{"re-resolve target against current semantic tree", "do not guess replacements after divergence"},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
