package state

import (
	"encoding/hex"
	"fmt"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/internal/canonical"
	"github.com/ben-ranford/stave/semantic"
)

const (
	CheckpointSchemaVersion = "stave.checkpoint/v1"
	ReplaySchemaVersion     = "stave.replay/v1"
	DefaultWidthPolicy      = "stave-width-v1"
)

type Versions struct {
	CheckpointSchema        string `json:"checkpointSchema"`
	SemanticSchema          string `json:"semanticSchema"`
	EventSchema             string `json:"eventSchema"`
	ReplaySchema            string `json:"replaySchema"`
	JSONPolicy              string `json:"jsonPolicy"`
	NodeIDAlgorithm         string `json:"nodeIdAlgorithm"`
	WidthPolicy             string `json:"widthPolicy"`
	KeymapSchema            string `json:"keymapSchema"`
	ModuleSchema            string `json:"moduleSchema"`
	ProtocolSchema          string `json:"protocolSchema"`
	CanonicalSchema         string `json:"canonicalSchema"`
	EffectSchema            string `json:"effectSchema"`
	EventAlgorithm          string `json:"eventAlgorithm"`
	CheckpointSchemaVersion string `json:"checkpointVersion"`
}

type Hashes struct {
	Config       string `json:"config,omitempty"`
	Theme        string `json:"theme,omitempty"`
	Capability   string `json:"capability,omitempty"`
	Model        string `json:"model,omitempty"`
	Tree         string `json:"tree,omitempty"`
	Surface      string `json:"surface,omitempty"`
	EffectLedger string `json:"effectLedger,omitempty"`
	Declarations string `json:"declarations,omitempty"`
}

type ModelPolicy[M any] struct {
	Clone    func(M) (M, error)
	Sanitize func(M) (M, error)
	Hash     func(M) ([32]byte, error)
}

type Meta struct {
	Sequence        uint64
	Revision        uint64
	Capabilities    capability.Manifest
	ConfigHash      string
	ThemeHash       string
	SurfaceHash     string
	WidthPolicy     string
	Versions        Versions
	DiagnosticCount uint64
	EffectLedger    string
	Declarations    string
}

type State[M any] struct {
	SessionID       string
	Sequence        uint64
	Revision        uint64
	Model           M
	Tree            semantic.Tree
	Capabilities    capability.Manifest
	ConfigHash      string
	ThemeHash       string
	WidthPolicy     string
	SurfaceHash     string
	Hashes          Hashes
	Versions        Versions
	DiagnosticCount uint64
}

type Checkpoint struct {
	SchemaVersion   string              `json:"schemaVersion"`
	SessionID       string              `json:"sessionId"`
	Sequence        uint64              `json:"sequence"`
	Revision        uint64              `json:"revision"`
	Versions        Versions            `json:"versions"`
	Hashes          Hashes              `json:"hashes"`
	Capabilities    capability.Manifest `json:"capabilities"`
	Model           any                 `json:"model,omitempty"`
	Tree            any                 `json:"tree,omitempty"`
	DiagnosticCount uint64              `json:"diagnosticCount,omitempty"`
	Checksum        string              `json:"checksum"`
}

func New[M any](sessionID string, model M, tree semantic.Tree, meta Meta, policy ModelPolicy[M]) (State[M], error) {
	if meta.Revision == 0 {
		meta.Revision = tree.Revision()
	}
	if meta.Revision != tree.Revision() {
		return State[M]{}, fmt.Errorf("state revision %d does not match semantic tree revision %d", meta.Revision, tree.Revision())
	}
	frozen, err := policy.freeze(model)
	if err != nil {
		return State[M]{}, err
	}
	versions := normalizeVersions(meta.Versions, tree)
	modelHash, err := policy.hashString(frozen)
	if err != nil {
		return State[M]{}, err
	}
	capabilityHash, err := HashString(meta.Capabilities.Clone())
	if err != nil {
		return State[M]{}, err
	}
	hashes := Hashes{
		Config:       meta.ConfigHash,
		Theme:        meta.ThemeHash,
		Capability:   capabilityHash,
		Model:        modelHash,
		Tree:         tree.Hash(),
		Surface:      meta.SurfaceHash,
		EffectLedger: meta.EffectLedger,
		Declarations: meta.Declarations,
	}
	if meta.WidthPolicy == "" {
		meta.WidthPolicy = versions.WidthPolicy
	}
	return State[M]{
		SessionID:       sessionID,
		Sequence:        meta.Sequence,
		Revision:        meta.Revision,
		Model:           frozen,
		Tree:            tree,
		Capabilities:    meta.Capabilities.Clone(),
		ConfigHash:      meta.ConfigHash,
		ThemeHash:       meta.ThemeHash,
		WidthPolicy:     meta.WidthPolicy,
		SurfaceHash:     meta.SurfaceHash,
		Hashes:          hashes,
		Versions:        versions,
		DiagnosticCount: meta.DiagnosticCount,
	}, nil
}

func (s State[M]) Clone(policy ModelPolicy[M]) (State[M], error) {
	frozen, err := policy.freeze(s.Model)
	if err != nil {
		return State[M]{}, err
	}
	s.Model = frozen
	s.Capabilities = s.Capabilities.Clone()
	return s, nil
}

func (s State[M]) Checkpoint(policy ModelPolicy[M]) (Checkpoint, error) {
	artifactModel, err := policy.artifactAny(s.Model)
	if err != nil {
		return Checkpoint{}, err
	}
	artifactTree, err := toAny(s.Tree.Snapshot())
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint := Checkpoint{
		SchemaVersion:   CheckpointSchemaVersion,
		SessionID:       s.SessionID,
		Sequence:        s.Sequence,
		Revision:        s.Revision,
		Versions:        normalizeVersions(s.Versions, s.Tree),
		Hashes:          s.Hashes,
		Capabilities:    s.Capabilities.Clone(),
		Model:           artifactModel,
		Tree:            artifactTree,
		DiagnosticCount: s.DiagnosticCount,
	}
	checksum, err := checkpoint.hashString()
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint.Checksum = checksum
	return checkpoint, nil
}

func (c Checkpoint) CanonicalJSON() ([]byte, error) {
	return canonical.Encode(c)
}

// VerifyChecksum rejects tampered or truncated checkpoint artifacts before
// they are used as replay state.
func (c Checkpoint) VerifyChecksum() error {
	if c.Checksum == "" {
		return fmt.Errorf("checkpoint checksum is required")
	}
	want, err := c.hashString()
	if err != nil {
		return err
	}
	if want != c.Checksum {
		return fmt.Errorf("checkpoint checksum mismatch")
	}
	return nil
}

func HashString(v any) (string, error) {
	sum, err := canonical.Hash(v)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

func Clone[T any](value T) (T, error) {
	return cloneJSON(value)
}

func normalizeVersions(v Versions, tree semantic.Tree) Versions {
	if v.CheckpointSchema == "" {
		v.CheckpointSchema = CheckpointSchemaVersion
	}
	if v.SemanticSchema == "" {
		v.SemanticSchema = tree.SchemaVersion()
		if v.SemanticSchema == "" {
			v.SemanticSchema = "stave.semantic/v1"
		}
	}
	if v.EventSchema == "" {
		v.EventSchema = event.SchemaVersion
	}
	if v.ReplaySchema == "" {
		v.ReplaySchema = ReplaySchemaVersion
	}
	if v.JSONPolicy == "" {
		v.JSONPolicy = canonical.JSONPolicyVersion
	}
	if v.NodeIDAlgorithm == "" {
		v.NodeIDAlgorithm = semantic.NodeIDNormalizationPolicy
	}
	if v.WidthPolicy == "" {
		v.WidthPolicy = DefaultWidthPolicy
	}
	if v.KeymapSchema == "" {
		v.KeymapSchema = "stave.keymap/v1"
	}
	if v.ModuleSchema == "" {
		v.ModuleSchema = "stave.module/v1"
	}
	if v.ProtocolSchema == "" {
		v.ProtocolSchema = "stave.protocol/v1"
	}
	if v.CanonicalSchema == "" {
		v.CanonicalSchema = canonical.JSONPolicyVersion
	}
	if v.EffectSchema == "" {
		v.EffectSchema = "stave.effect/v1"
	}
	if v.EventAlgorithm == "" {
		v.EventAlgorithm = "stave-event-v1"
	}
	if v.CheckpointSchemaVersion == "" {
		v.CheckpointSchemaVersion = CheckpointSchemaVersion
	}
	return v
}

// ValidateSupportedVersions rejects artifacts produced by a coordinated but
// incompatible runtime before any model or reducer code is invoked.
func ValidateSupportedVersions(v Versions) error {
	want := Versions{
		CheckpointSchema:        CheckpointSchemaVersion,
		SemanticSchema:          "stave-semantic-v1",
		EventSchema:             event.SchemaVersion,
		ReplaySchema:            ReplaySchemaVersion,
		JSONPolicy:              canonical.JSONPolicyVersion,
		NodeIDAlgorithm:         semantic.NodeIDNormalizationPolicy,
		WidthPolicy:             DefaultWidthPolicy,
		KeymapSchema:            "stave.keymap/v1",
		ModuleSchema:            "stave.module/v1",
		ProtocolSchema:          "stave.protocol/v1",
		CanonicalSchema:         canonical.JSONPolicyVersion,
		EffectSchema:            "stave.effect/v1",
		EventAlgorithm:          "stave-event-v1",
		CheckpointSchemaVersion: CheckpointSchemaVersion,
	}
	if v != want {
		return fmt.Errorf("unsupported runtime versions")
	}
	return nil
}

func (p ModelPolicy[M]) freeze(model M) (M, error) {
	if p.Clone != nil {
		return p.Clone(model)
	}
	return cloneJSON(model)
}

func (p ModelPolicy[M]) artifactAny(model M) (any, error) {
	sanitized := model
	var err error
	if p.Sanitize != nil {
		sanitized, err = p.Sanitize(model)
		if err != nil {
			return nil, err
		}
	}
	frozen, err := p.freeze(sanitized)
	if err != nil {
		return nil, err
	}
	return toAny(frozen)
}

func (p ModelPolicy[M]) hashString(model M) (string, error) {
	if p.Hash != nil {
		sum, err := p.Hash(model)
		if err != nil {
			return "", err
		}
		return hex.EncodeToString(sum[:]), nil
	}
	artifact, err := p.artifactAny(model)
	if err != nil {
		return "", err
	}
	return HashString(artifact)
}

func (c Checkpoint) hashString() (string, error) {
	type wire struct {
		SchemaVersion   string              `json:"schemaVersion"`
		SessionID       string              `json:"sessionId"`
		Sequence        uint64              `json:"sequence"`
		Revision        uint64              `json:"revision"`
		Versions        Versions            `json:"versions"`
		Hashes          Hashes              `json:"hashes"`
		Capabilities    capability.Manifest `json:"capabilities"`
		Model           any                 `json:"model,omitempty"`
		Tree            any                 `json:"tree,omitempty"`
		DiagnosticCount uint64              `json:"diagnosticCount,omitempty"`
	}
	return HashString(wire{
		SchemaVersion:   c.SchemaVersion,
		SessionID:       c.SessionID,
		Sequence:        c.Sequence,
		Revision:        c.Revision,
		Versions:        c.Versions,
		Hashes:          c.Hashes,
		Capabilities:    c.Capabilities.Clone(),
		Model:           c.Model,
		Tree:            c.Tree,
		DiagnosticCount: c.DiagnosticCount,
	})
}

func cloneJSON[T any](value T) (T, error) {
	data, err := canonical.Encode(value)
	if err != nil {
		var zero T
		return zero, err
	}
	var out T
	if err := canonical.Decode(data, &out); err != nil {
		var zero T
		return zero, fmt.Errorf("clone model: %w", err)
	}
	return out, nil
}

func toAny(value any) (any, error) {
	data, err := canonical.Encode(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := canonical.Decode(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
