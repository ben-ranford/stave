package replay

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
)

func TestExecuteRejectsUnsupportedVersionsBeforeApply(t *testing.T) {
	tr := mustTranscript(t)
	tr.Versions.WidthPolicy = "stave-width-v99"
	called := false
	_, err := Execute(context.Background(), tr, func(context.Context, state.Checkpoint, event.Event) (state.Checkpoint, error) {
		called = true
		return state.Checkpoint{}, nil
	})
	if err == nil || called {
		t.Fatalf("Execute() error=%v called=%v, want fail before apply", err, called)
	}
}

func TestExecuteValidEmptyTranscript(t *testing.T) {
	tr := mustTranscript(t)
	tr.Records = nil
	got, err := Execute(context.Background(), tr, func(context.Context, state.Checkpoint, event.Event) (state.Checkpoint, error) {
		t.Fatal("apply called for empty transcript")
		return state.Checkpoint{}, nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := Validate(tr, got); err != nil {
		t.Fatalf("valid empty replay failed validation: %v", err)
	}
}

func TestValidateChecksFullPriorDigestLinkage(t *testing.T) {
	tr := mustTranscript(t)
	tr.Records = append(tr.Records, Record{
		SchemaVersion: SchemaVersion,
		Event:         event.Event{SchemaVersion: event.SchemaVersion, Kind: event.Key, Sequence: 3, Revision: 1, Payload: event.KeyPayload{Key: "tab"}},
		Prior:         tr.Records[0].Result,
		Result:        Digest{Sequence: 3, Revision: 1, Hashes: tr.Records[0].Result.Hashes},
	})
	actual := tr
	actual.Records = append([]Record(nil), tr.Records...)
	actual.Records[1].Prior.Hashes.Model = "tampered"
	if err := Validate(tr, actual); err == nil {
		t.Fatal("Validate() accepted a transcript with a broken prior hash chain")
	}
}

func TestValidateDetectsVersionAndHashDivergences(t *testing.T) {
	base := mustTranscript(t)
	cases := []struct {
		name   string
		mutate func(Transcript) Transcript
		code   DivergenceCode
	}{
		{
			name: "node id algorithm",
			mutate: func(tr Transcript) Transcript {
				tr.Versions.NodeIDAlgorithm = "stave-node-id-v2"
				return tr
			},
			code: DivergenceNodeID,
		},
		{
			name: "width policy",
			mutate: func(tr Transcript) Transcript {
				tr.Versions.WidthPolicy = "other-width"
				return tr
			},
			code: DivergenceWidthPolicy,
		},
		{
			name: "config hash",
			mutate: func(tr Transcript) Transcript {
				tr.Records[0].Result.Hashes.Config = "other"
				return tr
			},
			code: DivergenceConfigHash,
		},
		{
			name: "theme hash",
			mutate: func(tr Transcript) Transcript {
				tr.Records[0].Result.Hashes.Theme = "other"
				return tr
			},
			code: DivergenceThemeHash,
		},
		{
			name: "capability hash",
			mutate: func(tr Transcript) Transcript {
				tr.Records[0].Result.Hashes.Capability = "other"
				return tr
			},
			code: DivergenceCapability,
		},
		{
			name: "model hash",
			mutate: func(tr Transcript) Transcript {
				tr.Records[0].Result.Hashes.Model = "other"
				return tr
			},
			code: DivergenceModelHash,
		},
		{
			name: "tree hash",
			mutate: func(tr Transcript) Transcript {
				tr.Records[0].Result.Hashes.Tree = "other"
				return tr
			},
			code: DivergenceTreeHash,
		},
		{
			name: "surface hash",
			mutate: func(tr Transcript) Transcript {
				tr.Records[0].Result.Hashes.Surface = "other"
				return tr
			},
			code: DivergenceSurfaceHash,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloned, err := base.Clone()
			if err != nil {
				t.Fatal(err)
			}
			actual := tc.mutate(cloned)
			err = Validate(base, actual)
			div, ok := err.(*Divergence)
			if !ok {
				t.Fatalf("Validate error = %T %v, want *Divergence", err, err)
			}
			if div.Code != tc.code {
				t.Fatalf("divergence code = %s, want %s", div.Code, tc.code)
			}
		})
	}
}

func TestValidateFailsClosedAtFirstEventMismatchWithRemap(t *testing.T) {
	base := mustTranscript(t)
	actual := mustTranscript(t)
	actual.Records[0].Event = event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.ActionInvoked,
		Payload: event.ActionInvokedPayload{
			CallID:   "call-1",
			ActionID: "app.select.v1",
			Target: &event.Target{
				NodeID:           "n1_target",
				Generation:       3,
				ObservedRevision: 2,
			},
		},
	}

	err := Validate(base, actual)
	div, ok := err.(*Divergence)
	if !ok {
		t.Fatalf("Validate error = %T %v, want *Divergence", err, err)
	}
	if div.Code != DivergenceEvent {
		t.Fatalf("divergence code = %s, want %s", div.Code, DivergenceEvent)
	}
	if div.Remap == nil || div.Remap.NodeID != "n1_target" {
		t.Fatalf("expected remap metadata, got %#v", div.Remap)
	}
}

func TestTranscriptRoundTrip(t *testing.T) {
	tr := mustTranscript(t)
	data, err := tr.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Transcript
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := Validate(tr, decoded); err != nil {
		t.Fatalf("roundtrip changed transcript: %v", err)
	}
}

func mustTranscript(t *testing.T) Transcript {
	t.Helper()
	tree := mustTree(t)
	st, err := state.New("session-1", map[string]any{"status": "ok"}, tree, state.Meta{
		Sequence:     1,
		Revision:     1,
		Capabilities: capability.Manifest{Width: 80, Height: 24},
		ConfigHash:   "cfg",
		ThemeHash:    "theme",
		SurfaceHash:  "surface",
	}, state.ModelPolicy[map[string]any]{})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := st.Checkpoint(state.ModelPolicy[map[string]any]{})
	if err != nil {
		t.Fatal(err)
	}
	tr := NewTranscript(st.SessionID, st.Versions, checkpoint)
	ev, err := event.New(event.Key, event.KeyPayload{Key: "enter"})
	if err != nil {
		t.Fatal(err)
	}
	tr.Append(Record{
		Event:  ev.WithAccepted(2, 1),
		Prior:  Digest{Sequence: checkpoint.Sequence, Revision: checkpoint.Revision, Hashes: checkpoint.Hashes, DiagnosticCount: checkpoint.DiagnosticCount},
		Result: DigestFromState(st),
	})
	tr.Records[0].Result.Sequence = 2
	return tr
}

func mustTree(t *testing.T) semantic.Tree {
	t.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{
		AppNamespace: "stave",
		View:         "test",
		Kind:         "root",
		Entity:       "main",
		Slot:         "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := semantic.NewNode(semantic.NodeSpec{
		ID:         id,
		Generation: 1,
		Role:       "application",
		Name:       "root",
		Flags:      semantic.Flags{Visible: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(1, node)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
