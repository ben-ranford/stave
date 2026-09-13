package agent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	requests := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"],\"capabilities\":{\"snapshotSubscriptionVersions\":[\"stave.snapshot.subscribe/v1\"]}}}\n" +
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

func TestSnapshotSubscriptionBaselinePrecedesNotification(t *testing.T) {
	base := subscriptionEnvelope(t)
	publication := make(chan struct{}, 1)
	delivered := make(chan struct{}, 1)
	gate := make(chan struct{})
	blocked := make(chan struct{})
	var sequence atomic.Uint64
	sequence.Store(1)
	options := Options{Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
		return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
	}, SnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
		env := base
		env.Sequence = sequence.Load()
		if env.Sequence == 2 {
			select {
			case delivered <- struct{}{}:
			default:
			}
		}
		return env, nil
	}, SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
		select {
		case <-publication:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	reader, inputWriter := io.Pipe()
	notificationWritten := make(chan struct{}, 1)
	writer := &baselineGateWriter{gate: gate, blocked: blocked, notification: notificationWritten}
	done := make(chan error, 1)
	go func() { done <- New(options).Serve(context.Background(), reader, writer) }()
	_, _ = io.WriteString(inputWriter, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"],\"capabilities\":{\"snapshotSubscriptionVersions\":[\"stave.snapshot.subscribe/v1\"]}}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot.subscribe\"}\n")
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("baseline write did not block")
	}
	sequence.Store(2)
	publication <- struct{}{}
	close(gate)
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("publication was not delivered")
	}
	select {
	case <-notificationWritten:
	case <-time.After(time.Second):
		t.Fatal("notification was not written")
	}
	_ = inputWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not finish")
	}
	out := writer.String()
	baseline := strings.Index(out, `"id":3`)
	notification := strings.Index(out, `"method":"stave.snapshot.subscription"`)
	if baseline < 0 || notification < 0 || baseline > notification {
		t.Fatalf("baseline did not precede notification: %s", out)
	}
}

type baselineGateWriter struct {
	mu sync.Mutex
	bytes.Buffer
	gate, blocked, notification chan struct{}
	once                        sync.Once
}

func (w *baselineGateWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"id":3`)) {
		w.once.Do(func() { close(w.blocked) })
		<-w.gate
	}
	if bytes.Contains(p, []byte(`"method":"stave.snapshot.subscription"`)) {
		select {
		case w.notification <- struct{}{}:
		default:
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.Write(p)
}
func (w *baselineGateWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.String()
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
		requests := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"],\"capabilities\":{\"snapshotSubscriptionVersions\":[\"stave.snapshot.subscribe/v1\"]}}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot.subscribe\"}\n"
		var output bytes.Buffer
		if err := New(options).Serve(context.Background(), input(requests), &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), `"code":-32007`) {
			t.Fatalf("legacy subscription was accepted: %s", output.String())
		}
	}
}
