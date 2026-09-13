package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/semantic"
)

func TestSnapshotSubscriptionRequiresNegotiatedExtensionAndIsIdempotentlyRemoved(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	options := Options{
		Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
		},
		SnapshotEnvelope:          func(context.Context, string, uint64) (SnapshotEnvelope, error) { return envelope, nil },
		SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error { <-ctx.Done(); return ctx.Err() },
	}
	requests := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"]}}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot.subscribe\"}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.snapshot.unsubscribe\"}\n"
	var output bytes.Buffer
	if err := New(options).Serve(context.Background(), input(requests), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"snapshot":{"schemaVersion":"stave.semantic/v1"`) || !strings.Contains(output.String(), `"id":4,"result":{"ok":true}`) {
		t.Fatalf("subscription transcript = %s", output.String())
	}
}

func subscriptionEnvelope(t *testing.T) SnapshotEnvelope {
	t.Helper()
	root, err := semantic.NewNode(semantic.NodeSpec{Key: &semantic.NodeKey{AppNamespace: "test", View: "subscription", Kind: "root", Entity: "root", Slot: "main"}, Generation: 1, Role: "application", Name: "Subscription"})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tree.Snapshot()
	hash := strings.Repeat("a", 64)
	return SnapshotEnvelope{Snapshot: &snapshot, Mode: "full", SessionID: "stave-session", Sequence: 1, Revision: 1, TreeHash: tree.Hash(), CapabilityHash: hash, SemanticVersion: tree.SchemaVersion(), ConfigHash: hash, ThemeHash: hash, WidthVersion: "width-v1"}
}

func TestSnapshotSubscriptionRejectsLegacyAndCompatibilityMode(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	for _, options := range []Options{
		{CompatibilityMode: true, SnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) { return envelope, nil }, SnapshotPublicationWaiter: func(context.Context, uint64) error { return nil }},
		{Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}}, nil
		}, SnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) { return envelope, nil }, SnapshotPublicationWaiter: func(context.Context, uint64) error { return nil }},
	} {
		requests := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"]}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot.subscribe\"}\n"
		var output bytes.Buffer
		if err := New(options).Serve(context.Background(), input(requests), &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), `"code":-32007`) {
			t.Fatalf("legacy subscription was accepted: %s", output.String())
		}
	}
}
