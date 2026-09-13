package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/diag"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/session"
	"github.com/ben-ranford/stave/state"
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
	bridge := &sessionBridge[M]{session: s, actions: options.Actions}
	options.SessionID = current.SessionID
	options.SnapshotEnvelope = bridge.snapshot
	options.CancelSession = bridge.cancel
	return options, nil
}

type sessionBridge[M any] struct {
	session     *session.Session[M]
	actions     *action.Registry
	mu          sync.Mutex
	previous    state.State[M]
	hasPrevious bool
	cancelOnce  sync.Once
}

func (b *sessionBridge[M]) cancel(context.Context) error {
	b.cancelOnce.Do(b.session.Cancel)
	return nil
}

func (b *sessionBridge[M]) snapshot(_ context.Context, mode string, since uint64) (SnapshotEnvelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current, err := b.session.Snapshot()
	if err != nil {
		return SnapshotEnvelope{}, err
	}
	envelope := SnapshotEnvelope{
		SessionID: current.SessionID, Sequence: current.Sequence, Revision: current.Revision,
		TreeHash: current.Tree.Hash(), CapabilityHash: current.Hashes.Capability,
		SemanticVersion: current.Versions.SemanticSchema, ConfigHash: current.ConfigHash,
		ThemeHash: current.ThemeHash, WidthVersion: current.WidthPolicy,
		Diagnostics: bridgeDiagnostics(b.session.Diagnostics(), current.Sequence, current.Revision),
	}
	if b.actions != nil {
		envelope.Actions = b.actions.Manifest()
	}
	if mode == "patch" {
		if !b.hasPrevious || since != b.previous.Revision || current.Revision <= since {
			return SnapshotEnvelope{}, errors.New("agent: stale session snapshot revision")
		}
		patch := semantic.Diff(b.previous.Tree, current.Tree)
		envelope.Mode, envelope.Patch = "patch", &patch
	} else {
		envelope.Mode = "full"
		snapshot := current.Tree.Snapshot()
		envelope.Snapshot = &snapshot
	}
	b.previous, b.hasPrevious = current, true
	return envelope, nil
}

func bridgeDiagnostics(in []session.Diagnostic, sequence, revision uint64) []diag.Diagnostic {
	out := make([]diag.Diagnostic, 0, len(in))
	for _, item := range in {
		out = append(out, diag.Diagnostic{SchemaVersion: "stave.diag/v1", ID: item.Code, Sequence: sequence, Revision: revision, Severity: diag.Warning, Code: item.Code, Redacted: true})
	}
	return out
}
