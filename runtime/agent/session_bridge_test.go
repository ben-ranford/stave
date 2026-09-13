package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/session"
	"github.com/ben-ranford/stave/state"
)

func TestBindSessionServesValidatedFullAndPatchAndPreservesAuthority(t *testing.T) {
	s := bridgeSession(t, "bound-session")
	defer s.Close()
	registry := action.NewRegistry()
	var authorized atomic.Int32
	bound, err := BindSession(s, Options{SessionID: "bound-session", Actions: registry, Authorize: func(context.Context, action.Call) *action.Error {
		authorized.Add(1)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if bound.SessionID != "bound-session" || bound.Authorize == nil || bound.Actions != registry {
		t.Fatalf("BindSession replaced caller authority: %#v", bound)
	}
	full, err := bound.SnapshotEnvelope(context.Background(), "full", 0)
	if err != nil || full.Snapshot == nil || full.Patch != nil || full.Snapshot.Validate() != nil || full.Snapshot.TreeHash != full.TreeHash {
		t.Fatalf("invalid full envelope: %#v, %v", full, err)
	}
	if full.Sequence != 1 {
		t.Fatalf("initial agent sequence = %d, want one-based 1", full.Sequence)
	}
	if err := s.Send(bridgeEvent(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.Wait(context.Background(), func(current state.State[int]) bool { return current.Revision > full.Revision }); err != nil {
		t.Fatal(err)
	}
	patch, err := bound.SnapshotEnvelope(context.Background(), "patch", full.Revision)
	if err != nil || patch.Patch == nil || patch.Snapshot != nil || patch.Patch.FromRevision != full.Revision || patch.Patch.ToRevision != patch.Revision {
		t.Fatalf("invalid patch envelope: %#v, %v", patch, err)
	}
	if _, err := bound.SnapshotEnvelope(context.Background(), "patch", full.Revision); err == nil {
		t.Fatal("stale patch revision accepted")
	}
	if _, err := BindSession(s, Options{SessionID: "other"}); err == nil {
		t.Fatal("mismatched caller session id accepted")
	}

	bound.CompatibilityMode = true
	server := New(bound)
	var output bytes.Buffer
	requests := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot\"}\n"
	if err := server.Serve(context.Background(), input(requests), &output); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("\"error\"")) || !bytes.Contains(output.Bytes(), []byte("\"sequence\":2")) {
		t.Fatalf("initial bound snapshot was rejected: %s", output.String())
	}
	if err := bound.CancelSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Lifecycle() != session.LifecycleClosed {
		t.Fatalf("cancel did not close bound session: %s", s.Lifecycle())
	}
	if authorized.Load() != 0 {
		t.Fatal("session bridge invoked application authorization")
	}
}

func bridgeSession(t *testing.T, sessionID string) *session.Session[int] {
	t.Helper()
	return bridgeSessionWithHashes(t, sessionID, strings.Repeat("a", 64), strings.Repeat("b", 64))
}

func bridgeSessionWithHashes(t *testing.T, sessionID, configHash, themeHash string) *session.Session[int] {
	t.Helper()
	s, err := session.New(context.Background(), session.Options[int]{
		SessionID:  sessionID,
		ConfigHash: configHash,
		ThemeHash:  themeHash,
		Reduce: func(_ context.Context, current int, _ event.Event) (int, []effect.Request, error) {
			return current + 1, nil, nil
		},
		View: func(_ context.Context, current int) (session.ViewResult, error) {
			id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "test", View: "bridge", Kind: "root", Entity: fmt.Sprint(current), Slot: "main"})
			if err != nil {
				return session.ViewResult{}, err
			}
			node, err := semantic.NewNode(semantic.NodeSpec{ID: id, Generation: 1, Role: "application", Name: "bridge"})
			if err != nil {
				return session.ViewResult{}, err
			}
			tree, err := semantic.NewTree(uint64(current+1), node)
			return session.ViewResult{Tree: tree, SurfaceHash: fmt.Sprintf("surface-%d", current)}, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func bridgeEvent(t *testing.T) event.Event {
	t.Helper()
	ev, err := event.New(event.Key, event.KeyPayload{Key: "enter"})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestBindSessionRejectsUnusableHashMetadata(t *testing.T) {
	for _, hashes := range [][2]string{{"", ""}, {"invalid", strings.Repeat("b", 64)}, {strings.Repeat("a", 64), "invalid"}} {
		s := bridgeSessionWithHashes(t, "metadata", hashes[0], hashes[1])
		if _, err := BindSession(s, Options{}); err == nil {
			t.Error("BindSession accepted unusable hashes")
		}
		s.Close()
	}
}

func TestBridgeDiagnosticsBoundsAndRedactsHistory(t *testing.T) {
	in := make([]session.Diagnostic, 1000)
	for i := range in {
		in[i] = session.Diagnostic{Code: "secret-token-abc", Message: "private message"}
	}
	out := bridgeDiagnostics(in, 7, 4)
	if len(out) != 16 {
		t.Fatalf("projected %d diagnostics, want bounded tail 16", len(out))
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-token-abc")) || bytes.Contains(raw, []byte("private message")) {
		t.Fatalf("diagnostic leaked: %s", raw)
	}
	for _, item := range out {
		if item.Sequence != 7 || item.Revision != 4 || !item.Redacted {
			t.Fatalf("invalid projected metadata: %#v", item)
		}
	}
}
