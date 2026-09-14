package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/diag"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/session"
)

// BindSession attaches a Session to caller-owned agent Options. Applications
// retain ownership of the action registry, authorization, confirmation, and
// policy callbacks; this helper supplies snapshots and idempotent cancellation
// only. It is unreleased and intended for local checkouts until the next Stave
// release.
func BindSession[M any](s *session.Session[M], options Options) (Options, error) {
	if s == nil {
		return Options{}, errors.New("agent: session is required")
	}
	current, err := s.Snapshot()
	if err != nil {
		return Options{}, err
	}
	if options.SessionID != "" && options.SessionID != current.SessionID {
		return Options{}, errors.New("agent: session id does not match bound session")
	}
	if !validHash(current.ConfigHash) || !validHash(current.ThemeHash) {
		return Options{}, errors.New("agent: bound session requires valid config and theme hashes")
	}
	treeLimit := options.MaxTreeNodes
	if treeLimit <= 0 {
		treeLimit = defaultMaxTreeNodes
	}
	bridge := &sessionBridge[M]{session: s, actions: options.Actions, maxTreeNodes: treeLimit}
	options.SessionID = current.SessionID
	options.SnapshotEnvelope = bridge.snapshot
	options.CancelSession = bridge.cancel
	return options, nil
}

type sessionBridge[M any] struct {
	session          *session.Session[M]
	actions          *action.Registry
	maxTreeNodes     int
	mu               sync.Mutex
	previousTree     semantic.Tree
	previousRevision uint64
	hasPrevious      bool
	cancelOnce       sync.Once
}

const maxBridgeDiagnostics = 16

func (b *sessionBridge[M]) cancel(context.Context) error {
	b.cancelOnce.Do(b.session.Cancel)
	return nil
}

func (b *sessionBridge[M]) snapshot(ctx context.Context, mode string, since uint64) (SnapshotEnvelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current, err := b.session.Snapshot()
	if err != nil {
		return SnapshotEnvelope{}, err
	}
	requestOptions, hasRequestOptions := ctx.Value(snapshotOptionsKey{}).(snapshotOptions)
	if !hasRequestOptions {
		requestOptions = snapshotOptions{maxTreeNodes: b.maxTreeNodes, actions: b.actions}
	}
	if countSnapshotNodes(current.Tree.Root(), requestOptions.maxTreeNodes) < 0 {
		return SnapshotEnvelope{}, errors.New("agent: session snapshot exceeds tree node limit")
	}
	if current.Sequence == ^uint64(0) {
		return SnapshotEnvelope{}, errors.New("agent: session sequence overflow")
	}
	sequence := current.Sequence + 1
	envelope := SnapshotEnvelope{
		SessionID: current.SessionID, Sequence: sequence, Revision: current.Revision,
		TreeHash: current.Tree.Hash(), CapabilityHash: current.Hashes.Capability,
		SemanticVersion: current.Versions.SemanticSchema, ConfigHash: current.ConfigHash,
		ThemeHash: current.ThemeHash, WidthVersion: current.WidthPolicy,
		Diagnostics: bridgeDiagnostics(b.session.DiagnosticsTail(maxBridgeDiagnostics), sequence, current.Revision),
	}
	if requestOptions.actions != nil {
		envelope.Actions = requestOptions.actions.Manifest()
	}
	if mode == "patch" {
		if !b.hasPrevious || since != b.previousRevision || current.Revision <= since {
			return SnapshotEnvelope{}, errors.New("agent: stale session snapshot revision")
		}
		patch := semantic.Diff(b.previousTree, current.Tree)
		envelope.Mode, envelope.Patch = "patch", &patch
	} else {
		envelope.Mode = "full"
		snapshot := current.Tree.Snapshot()
		envelope.Snapshot = &snapshot
	}
	b.previousTree, b.previousRevision, b.hasPrevious = current.Tree, current.Revision, true
	return envelope, nil
}

func bridgeDiagnostics(in []session.Diagnostic, sequence, revision uint64) []diag.Diagnostic {
	if len(in) > maxBridgeDiagnostics {
		in = in[len(in)-maxBridgeDiagnostics:]
	}
	out := make([]diag.Diagnostic, 0, len(in))
	for range in {
		out = append(out, diag.Diagnostic{SchemaVersion: "stave.diag/v1", ID: "SESSION_DIAGNOSTIC", Sequence: sequence, Revision: revision, Severity: diag.Warning, Code: "SESSION_DIAGNOSTIC", Redacted: true})
	}
	return out
}
