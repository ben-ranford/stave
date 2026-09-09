package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/semantic"
)

// Exercise the published schema as a client, so it cannot advertise snapshot
// modes that the protocol server rejects.
func TestPublishedSnapshotModesRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../schema/protocol/protocol.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	root, err := semantic.NewNode(semantic.NodeSpec{Key: &semantic.NodeKey{AppNamespace: "test", View: "snapshot", Kind: "root", Entity: "root", Slot: "main"}, Generation: 1, Role: "application", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(2, root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tree.Snapshot()
	patch := semantic.Patch{FromRevision: 1, ToRevision: 2}
	hash := strings.Repeat("a", 64)
	for _, def := range []string{"snapshotParams", "snapshotResult"} {
		modes := schema.Defs[def].Properties["mode"].Enum
		if len(modes) == 0 {
			t.Fatalf("%s has no declared modes", def)
		}
		for _, mode := range modes {
			t.Run(def+"/"+mode, func(t *testing.T) {
				s := New(Options{CompatibilityMode: true, SnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
					envelope := SnapshotEnvelope{Mode: mode, SessionID: "stave-session", Sequence: 1, Revision: 2, TreeHash: tree.Hash(), CapabilityHash: hash, SemanticVersion: tree.SchemaVersion(), ConfigHash: hash, ThemeHash: hash, WidthVersion: "width-v1", Snapshot: &snapshot}
					if mode == "patch" {
						envelope.Snapshot, envelope.Patch = nil, &patch
					}
					return envelope, nil
				}})
				requests := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot\",\"params\":{\"mode\":%q,\"sinceRevision\":1}}\n", mode)
				var output bytes.Buffer
				if err := s.Serve(context.Background(), input(requests), &output); err != nil {
					t.Fatal(err)
				}
				decoder := json.NewDecoder(&output)
				for i := 0; i < 3; i++ {
					var response protocol.Response
					if err := decoder.Decode(&response); err != nil {
						t.Fatal(err)
					}
					if response.Error != nil {
						t.Fatalf("published mode %q rejected: %+v", mode, response.Error)
					}
				}
			})
		}
	}
}
