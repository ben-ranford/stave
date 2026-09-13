package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/session"
)

type subscriptionTestWriter struct {
	ctx     context.Context
	lines   chan []byte
	gate    chan struct{}
	blocked chan struct{}
	once    sync.Once
}

func (w *subscriptionTestWriter) Write(p []byte) (int, error) {
	if w.gate != nil && bytes.Contains(p, []byte(`"method":"stave.snapshot.subscription"`)) {
		w.once.Do(func() { close(w.blocked) })
		select {
		case <-w.gate:
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		}
	}
	select {
	case w.lines <- append([]byte(nil), p...):
		return len(p), nil
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

type subscriptionTestClient struct {
	t            *testing.T
	ctx          context.Context
	in           *io.PipeWriter
	out          *subscriptionTestWriter
	seen         chan uint64
	lastSnapshot json.RawMessage
}

func startSubscriptionClient(t *testing.T, s *session.Session[int], block bool) *subscriptionTestClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	in, input := io.Pipe()
	w := &subscriptionTestWriter{ctx: ctx, lines: make(chan []byte, 32)}
	if block {
		w.gate = make(chan struct{})
		w.blocked = make(chan struct{})
	}
	c := &subscriptionTestClient{t: t, ctx: ctx, in: input, out: w, seen: make(chan uint64, 32)}
	opts, err := BindSession(s, Options{Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
		return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	waiter := opts.SnapshotPublicationWaiter
	opts.SnapshotPublicationWaiter = func(ctx context.Context, after uint64) error {
		// Reentering the wait proves the preceding snapshot reached the
		// capacity-one slot, rather than merely reaching its provider.
		select {
		case c.seen <- after:
		case <-ctx.Done():
		}
		return waiter(ctx, after)
	}
	done := make(chan error, 1)
	go func() { done <- New(opts).Serve(ctx, in, w) }()
	t.Cleanup(func() {
		cancel()
		_ = input.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("subscription Serve leaked")
		}
	})
	c.request(`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`)
	c.response(1)
	c.request(`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`)
	c.response(2)
	c.request(`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`)
	c.response(3)
	return c
}

func (c *subscriptionTestClient) request(s string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, s+"\n"); err != nil {
		c.t.Fatal(err)
	}
}
func (c *subscriptionTestClient) line() []byte {
	c.t.Helper()
	select {
	case b := <-c.out.lines:
		return b
	case <-c.ctx.Done():
		c.t.Fatal("timed out waiting for subscription output")
		return nil
	}
}
func (c *subscriptionTestClient) response(id int) {
	c.t.Helper()
	b := c.line()
	var r struct {
		ID    int
		Error *protocol.Error
	}
	if err := json.Unmarshal(b, &r); err != nil || r.ID != id || r.Error != nil {
		c.t.Fatalf("response %d: %s (%v)", id, b, err)
	}
}
func (c *subscriptionTestClient) notification() protocol.SnapshotResult {
	c.t.Helper()
	b := c.line()
	var n struct {
		Method string
		Params protocol.SnapshotSubscriptionNotification
	}
	if err := json.Unmarshal(b, &n); err != nil || n.Method != "stave.snapshot.subscription" || n.Params.Snapshot.Mode != "full" {
		c.t.Fatalf("full notification: %s (%v)", b, err)
	}
	var wire struct {
		Params struct {
			Snapshot struct{ Snapshot json.RawMessage }
		}
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		c.t.Fatal(err)
	}
	c.lastSnapshot = wire.Params.Snapshot.Snapshot
	return n.Params.Snapshot
}
func (c *subscriptionTestClient) observed(sequence uint64) {
	c.t.Helper()
	for {
		select {
		case n := <-c.seen:
			if n >= sequence {
				return
			}
		case <-c.ctx.Done():
			c.t.Fatal("producer blocked behind transport")
			return
		}
	}
}

func TestSnapshotSubscriptionCoalescesFullSnapshots(t *testing.T) {
	s := bridgeSession(t, "stave-session")
	defer s.Close()
	c := startSubscriptionClient(t, s, true)
	if err := s.Send(bridgeEvent(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.out.blocked:
	case <-c.ctx.Done():
		t.Fatal("notification writer did not block")
	}
	for seq := uint64(3); seq <= 6; seq++ {
		if err := s.Send(bridgeEvent(t)); err != nil {
			t.Fatal(err)
		}
		c.observed(seq)
	}
	latest, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	close(c.out.gate)
	first, last := c.notification(), c.notification()
	expected, err := json.Marshal(latest.Tree.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 2 || last.Sequence != 6 || last.Revision <= first.Revision || last.TreeHash != latest.Tree.Hash() || !bytes.Equal(c.lastSnapshot, expected) {
		t.Fatalf("invalid full resync: first=%+v last=%+v latestHash=%s", first, last, latest.Tree.Hash())
	}
}

func TestSnapshotSubscriptionIsolatesPhysicalClients(t *testing.T) {
	s := bridgeSession(t, "stave-session")
	defer s.Close()
	slow, fast := startSubscriptionClient(t, s, true), startSubscriptionClient(t, s, false)
	if err := s.Send(bridgeEvent(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-slow.out.blocked:
	case <-slow.ctx.Done():
		t.Fatal("slow writer did not block")
	}
	n := fast.notification()
	if n.Sequence != 2 {
		t.Fatalf("fast client did not progress: %+v", n)
	}
	close(slow.out.gate)
	if n.TreeHash != slow.notification().TreeHash {
		t.Fatal("client snapshot hashes differ")
	}
}

func TestSnapshotSubscriptionFailureCancelsAndJoinsProducer(t *testing.T) {
	waiting := make(chan struct{})
	s := newSnapshotSubscription(context.Background(), protocol.SnapshotResult{Sequence: 1, Revision: 1}, make(chan struct{}, 1), func(ctx context.Context, _ uint64) error {
		close(waiting)
		<-ctx.Done()
		return ctx.Err()
	}, func(context.Context) (protocol.SnapshotResult, bool) { return protocol.SnapshotResult{}, true })
	defer s.cancel()
	<-waiting
	s.fail("output_limit")
	s.close()
	select {
	case <-s.done:
	default:
		t.Fatal("terminated subscription left its producer running")
	}
}

func TestSnapshotSubscriptionRejectsRegressingProvider(t *testing.T) {
	for _, result := range []protocol.SnapshotResult{{Sequence: 2, Revision: 1}, {Sequence: 1, Revision: 2}} {
		s := newSnapshotSubscription(context.Background(), protocol.SnapshotResult{Sequence: 1, Revision: 2}, make(chan struct{}, 1), func(context.Context, uint64) error { return nil }, func(context.Context) (protocol.SnapshotResult, bool) { return result, true })
		select {
		case <-s.done:
		case <-time.After(time.Second):
			s.cancel()
			t.Fatal("regressing provider caused an unbounded retry loop")
		}
		if reason, ok := s.takeTerminal(); !ok || reason != "provider_failed" {
			t.Fatalf("terminal=%q, %v", reason, ok)
		}
		s.close()
	}
}
