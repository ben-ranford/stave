package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/session"
	"github.com/ben-ranford/stave/state"
)

func TestBindSessionEnforcesTreeLimitBeforeAdvancingPatchBaseline(t *testing.T) {
	for _, test := range []struct {
		name                                string
		bindLimit, serverLimit, clientLimit int
	}{
		{"host limit", 2, 2, 0},
		{"client limit", 5, 5, 2},
		{"default host with client limit", 0, 0, 2},
		{"caller changes host limit after binding", 1, 5, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := bridgeSessionWithTreeSize(t, 1)
			defer s.Close()
			bound, err := BindSession(s, Options{CompatibilityMode: true, MaxTreeNodes: test.bindLimit})
			if err != nil {
				t.Fatal(err)
			}
			bound.MaxTreeNodes = test.serverLimit
			server := New(bound)
			defer server.Close()
			request := func(method string, params string) protocol.Response {
				return server.handle(context.Background(), protocol.Request{JSONRPC: protocol.JSONRPC, ID: json.RawMessage("1"), Method: method, Params: json.RawMessage(params)})
			}
			if response := request("stave.initialize", fmt.Sprintf(`{"limits":{"maxTreeNodes":%d}}`, test.clientLimit)); response.Error != nil {
				t.Fatal(response.Error)
			}
			if response := request("stave.initialized", ""); response.Error != nil {
				t.Fatal(response.Error)
			}
			full := request("stave.snapshot", `{"mode":"full"}`)
			if full.Error != nil {
				t.Fatal(full.Error)
			}
			baseline := full.Result.(protocol.SnapshotResult).Revision
			advance := func(key string) {
				current, err := s.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				ev, err := event.New(event.Key, event.KeyPayload{Key: key})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Send(ev); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := s.Wait(ctx, func(next state.State[int]) bool { return next.Revision > current.Revision }); err != nil {
					t.Fatal(err)
				}
			}
			advance("enter")
			atLimit := request("stave.snapshot", fmt.Sprintf(`{"mode":"patch","sinceRevision":%d}`, baseline))
			if atLimit.Error != nil {
				t.Fatalf("patch at limit rejected: %v", atLimit.Error)
			}
			baseline = atLimit.Result.(protocol.SnapshotResult).Revision
			advance("enter")
			overLimit := request("stave.snapshot", fmt.Sprintf(`{"mode":"patch","sinceRevision":%d}`, baseline))
			if overLimit.Error == nil {
				t.Fatal("patch exceeding negotiated tree-node limit was accepted")
			}
			if response := request("stave.snapshot", `{"mode":"full"}`); response.Error == nil {
				t.Fatal("full snapshot exceeding tree-node limit was accepted")
			}
			advance("escape")
			recovered := request("stave.snapshot", fmt.Sprintf(`{"mode":"patch","sinceRevision":%d}`, baseline))
			if recovered.Error != nil {
				t.Fatalf("rejected snapshot advanced the patch baseline: %v", recovered.Error)
			}
			if patch := recovered.Result.(protocol.SnapshotResult).Patch; patch == nil || patch.FromRevision != baseline {
				t.Fatalf("recovery patch has wrong baseline: %#v", patch)
			}
		})
	}
}

func bridgeTreeSizeView(ctx context.Context, size int) (session.ViewResult, error) {
	view, err := bridgeView(ctx, 0)
	if err != nil {
		return session.ViewResult{}, err
	}
	children := make([]semantic.Node, 0, size-1)
	for index := 1; index < size; index++ {
		child, err := semantic.NewNode(semantic.NodeSpec{Key: &semantic.NodeKey{AppNamespace: "test", View: "bridge", Kind: "child", Entity: fmt.Sprint(index), Slot: "main"}, Generation: 1, Role: "text", Name: "child"})
		if err != nil {
			return session.ViewResult{}, err
		}
		children = append(children, child)
	}
	root, err := view.Tree.Root().WithChildren(children...)
	if err != nil {
		return session.ViewResult{}, err
	}
	view.Tree, err = semantic.NewTree(1, root)
	return view, err
}

func TestBindSessionDirectSnapshotHonorsHostTreeLimit(t *testing.T) {
	s := bridgeSessionWithTreeSize(t, 3)
	defer s.Close()
	bound, err := BindSession(s, Options{MaxTreeNodes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.SnapshotEnvelope(context.Background(), "full", 0); err == nil {
		t.Fatal("direct bound provider accepted a tree exceeding its host limit")
	}
}

func bridgeSessionWithTreeSize(t *testing.T, initial int) *session.Session[int] {
	t.Helper()
	s, err := session.New(context.Background(), session.Options[int]{
		SessionID: "tree-limit", Initial: initial,
		ConfigHash: strings.Repeat("a", 64), ThemeHash: strings.Repeat("b", 64),
		Reduce: func(_ context.Context, size int, ev event.Event) (int, []effect.Request, error) {
			if ev.Payload.(event.KeyPayload).Key == "escape" {
				return size - 1, nil, nil
			}
			return size + 1, nil, nil
		},
		View: bridgeTreeSizeView,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
