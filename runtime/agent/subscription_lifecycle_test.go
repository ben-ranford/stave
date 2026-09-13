package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/diag"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/semantic"
)

func TestSnapshotSubscriptionUnsubscribeStopsDeliveryAndAllowsResubscribe(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	var sequence atomic.Uint64
	sequence.Store(1)
	publication := make(chan uint64, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	out := &subscriptionTestWriter{ctx: ctx, lines: make(chan []byte, 16)}
	done := make(chan error, 1)
	go func() {
		done <- New(Options{
			Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
				return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
			},
			SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
				env := envelope
				env.Sequence = sequence.Load()
				return env, nil
			},
			SnapshotPublicationWaiter: func(waitCtx context.Context, after uint64) error {
				for {
					select {
					case sequence := <-publication:
						if sequence > after {
							return nil
						}
					case <-waitCtx.Done():
						return waitCtx.Err()
					}
				}
			},
		}).Serve(ctx, reader, out)
	}()
	c := &subscriptionTestClient{t: t, ctx: ctx, in: writer, out: out}
	c.request(`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`)
	c.response(1)
	c.request(`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`)
	c.response(2)
	c.request(`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`)
	c.response(3)
	c.request(`{"jsonrpc":"2.0","id":4,"method":"stave.snapshot.subscribe"}`)
	c.responseError(4)
	c.request(`{"jsonrpc":"2.0","id":5,"method":"stave.snapshot.unsubscribe"}`)
	c.response(5)
	sequence.Store(2)
	publication <- 2
	select {
	case line := <-c.out.lines:
		t.Fatalf("notification after unsubscribe: %s", line)
	case <-time.After(100 * time.Millisecond):
	}
	c.request(`{"jsonrpc":"2.0","id":6,"method":"stave.snapshot.subscribe"}`)
	c.response(6)
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotSubscriptionRejectsMalformedParamsAndUnofferedExtension(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	options := Options{
		Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			// A host offer alone must not enable an extension the client declined.
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
		},
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) { return envelope, nil },
		SnapshotPublicationWaiter:    func(ctx context.Context, _ uint64) error { <-ctx.Done(); return ctx.Err() },
	}
	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"]}}`,
		`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe","params":{"unexpected":true}}`,
		`{"jsonrpc":"2.0","id":4,"method":"stave.snapshot.unsubscribe","params":{"unexpected":true}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := New(options).Serve(context.Background(), input(requests), &output); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{3, 4} {
		var response struct {
			ID    int             `json:"id"`
			Error *protocol.Error `json:"error"`
		}
		for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'}) {
			if json.Unmarshal(line, &response) == nil && response.ID == id {
				break
			}
		}
		if response.ID != id || response.Error == nil {
			t.Fatalf("request %d was accepted or omitted: %s", id, output.String())
		}
	}
}

func TestSnapshotSubscriptionAdvertisesOnlyMutuallySupportedVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		host []string
		want []string
	}{
		{name: "deduplicates and intersects", host: []string{protocol.SnapshotSubscriptionVersion, "stave.snapshot.subscribe/v2", protocol.SnapshotSubscriptionVersion}, want: []string{protocol.SnapshotSubscriptionVersion}},
		{name: "omits unsupported host versions", host: []string{"stave.snapshot.subscribe/v2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := Options{Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
				return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: tc.host}, nil
			}}
			var output bytes.Buffer
			request := `{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}` + "\n"
			if err := New(options).Serve(context.Background(), input(request), &output); err != nil {
				t.Fatal(err)
			}
			var response struct {
				Result struct {
					Capabilities capability.Manifest `json:"capabilities"`
				} `json:"result"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
				t.Fatal(err)
			}
			if got := response.Result.Capabilities.SnapshotSubscriptionVersions; !equalStrings(got, tc.want) {
				t.Fatalf("advertised snapshot subscription versions = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSnapshotSubscriptionIgnoresIDlessUnsubscribeNotification(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	var sequence atomic.Uint64
	sequence.Store(1)
	publication := make(chan uint64, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	out := &subscriptionTestWriter{ctx: ctx, lines: make(chan []byte, 16)}
	done := make(chan error, 1)
	go func() {
		done <- New(Options{
			Negotiate: subscriptionNegotiator,
			SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
				env := envelope
				env.Sequence = sequence.Load()
				return env, nil
			},
			SnapshotPublicationWaiter: func(waitCtx context.Context, after uint64) error {
				for {
					select {
					case next := <-publication:
						if next > after {
							return nil
						}
					case <-waitCtx.Done():
						return waitCtx.Err()
					}
				}
			},
		}).Serve(ctx, reader, out)
	}()
	c := &subscriptionTestClient{t: t, ctx: ctx, in: writer, out: out}
	c.request(`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`)
	c.response(1)
	c.request(`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`)
	c.response(2)
	c.request(`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`)
	c.response(3)
	c.request(`{"jsonrpc":"2.0","method":"stave.snapshot.unsubscribe"}`)
	sequence.Store(2)
	publication <- 2
	if got := c.notification(); got.Sequence != 2 {
		t.Fatalf("notification sequence = %d, want 2", got.Sequence)
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotSubscriptionBaselineOmitsActionsAndRedactsDiagnostics(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	envelope.Actions = []action.Definition{simpleActionDefinition(action.ID("test.action"), action.ReadOnly)}
	envelope.Diagnostics = []diag.Diagnostic{{ID: "private", Code: "PRIVATE", Message: "secret diagnostic", Redacted: false}}
	var authorized, confirmed atomic.Int32
	options := Options{
		Negotiate: subscriptionNegotiator,
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
			return envelope, nil
		},
		SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error { <-ctx.Done(); return ctx.Err() },
		Authorize:                 func(context.Context, action.Call) *action.Error { authorized.Add(1); return nil },
		Confirm: func(context.Context, action.Call) (action.Confirmation, error) {
			confirmed.Add(1)
			return action.Confirmation{}, nil
		},
	}
	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := New(options).Serve(context.Background(), input(requests), &output); err != nil {
		t.Fatal(err)
	}
	var baseline struct {
		ID     int `json:"id"`
		Result struct {
			Snapshot protocol.SnapshotResult `json:"snapshot"`
		} `json:"result"`
	}
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'}) {
		if json.Unmarshal(line, &baseline) == nil && baseline.ID == 3 {
			break
		}
	}
	if baseline.ID != 3 || len(baseline.Result.Snapshot.Actions) != 0 {
		t.Fatalf("subscription baseline exposed actions: %s", output.String())
	}
	if len(baseline.Result.Snapshot.Diagnostics) != 1 || !baseline.Result.Snapshot.Diagnostics[0].Redacted || strings.Contains(output.String(), "secret diagnostic") {
		t.Fatalf("subscription baseline did not redact diagnostics: %s", output.String())
	}
	if authorized.Load() != 0 || confirmed.Load() != 0 {
		t.Fatalf("subscription baseline invoked authority callbacks: authorize=%d confirm=%d", authorized.Load(), confirmed.Load())
	}
}

func TestBoundSnapshotSubscriptionLifecycleStopsPump(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger func(*Server, context.CancelFunc, *io.PipeWriter, func())
	}{
		{name: "session close", trigger: func(_ *Server, _ context.CancelFunc, _ *io.PipeWriter, closeSession func()) { closeSession() }},
		{name: "serve context cancellation", trigger: func(_ *Server, cancel context.CancelFunc, _ *io.PipeWriter, _ func()) { cancel() }},
		{name: "transport EOF", trigger: func(_ *Server, _ context.CancelFunc, input *io.PipeWriter, _ func()) { _ = input.Close() }},
		{name: "server close", trigger: func(server *Server, _ context.CancelFunc, _ *io.PipeWriter, _ func()) { server.Close() }},
		{name: "session cancel", trigger: func(_ *Server, _ context.CancelFunc, input *io.PipeWriter, _ func()) {
			_, _ = io.WriteString(input, `{"jsonrpc":"2.0","id":4,"method":"stave.session.cancel","params":{}}`+"\n")
		}},
		{name: "session shutdown", trigger: func(_ *Server, _ context.CancelFunc, input *io.PipeWriter, _ func()) {
			_, _ = io.WriteString(input, `{"jsonrpc":"2.0","id":4,"method":"stave.session.shutdown"}`+"\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := bridgeSession(t, "lifecycle-"+strings.ReplaceAll(tc.name, " ", "-"))
			defer session.Close()
			bound, err := BindSession(session, Options{Negotiate: subscriptionNegotiator})
			if err != nil {
				t.Fatal(err)
			}
			entered, exited := make(chan struct{}), make(chan struct{})
			wait := bound.SnapshotPublicationWaiter
			bound.SnapshotPublicationWaiter = func(ctx context.Context, after uint64) error {
				close(entered)
				defer close(exited)
				return wait(ctx, after)
			}
			server := New(bound)
			serveCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader, writer := io.Pipe()
			done := make(chan error, 1)
			go func() { done <- server.Serve(serveCtx, reader, io.Discard) }()
			writeSubscriptionHandshake(t, writer)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("subscription waiter did not start")
			}
			tc.trigger(server, cancel, writer, session.Close)
			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Fatal("subscription waiter did not stop")
			}
			_ = writer.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Serve did not clean up after subscription stop")
			}
		})
	}
}

func TestSnapshotSubscriptionProviderFailureEmitsOneTerminalAndStopsPump(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	publication := make(chan struct{}, 1)
	providerCalls := 0
	options := Options{
		Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
		},
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
			providerCalls++
			if providerCalls > 1 {
				return SnapshotEnvelope{}, errors.New("provider failure")
			}
			return envelope, nil
		},
		SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
			select {
			case <-publication:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	inputReader, inputWriter := io.Pipe()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- New(options).Serve(context.Background(), inputReader, &output) }()
	_, _ = inputWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}` + "\n" + `{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}` + "\n"))
	publication <- struct{}{}
	time.Sleep(20 * time.Millisecond)
	_ = inputWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not release the subscription pump")
	}
	if got := strings.Count(output.String(), `"state":"terminated","reason":"provider_failed"`); got != 1 {
		t.Fatalf("provider failure terminal count=%d transcript=%s", got, output.String())
	}
}

func TestSnapshotSubscriptionOversizeBaselineDoesNotArmWaiter(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	envelope.WidthVersion = strings.Repeat("x", 1024)
	var waiterCalls atomic.Int32
	options := Options{
		MaxOutputBytes: 128,
		Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
		},
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) { return envelope, nil },
		SnapshotPublicationWaiter: func(context.Context, uint64) error {
			waiterCalls.Add(1)
			return nil
		},
	}
	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := New(options).Serve(context.Background(), input(requests), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"id":3,"error":{"code":-32013`) {
		t.Fatalf("oversize baseline was not typed OutputLimit: %s", output.String())
	}
	if got := waiterCalls.Load(); got != 0 {
		t.Fatalf("oversize baseline armed waiter %d times", got)
	}
}

func TestSnapshotSubscriptionSessionCloseEmitsOneTerminalAndStopsPump(t *testing.T) {
	envelope := subscriptionEnvelope(t)
	closed := make(chan struct{}, 1)
	options := Options{
		Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
		},
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) { return envelope, nil },
		SnapshotPublicationWaiter: func(context.Context, uint64) error {
			<-closed
			return errors.New("session closed")
		},
	}
	reader, writer := io.Pipe()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- New(options).Serve(context.Background(), reader, &output) }()
	_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}` + "\n" + `{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}` + "\n"))
	close(closed)
	time.Sleep(20 * time.Millisecond)
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not join session-closed subscription pump")
	}
	if got := strings.Count(output.String(), `"state":"terminated","reason":"session_closed"`); got != 1 {
		t.Fatalf("session close terminal count=%d transcript=%s", got, output.String())
	}
}

func TestSnapshotSubscriptionOversizeUpdateEmitsOneTerminalAndStopsPump(t *testing.T) {
	base, large := lifecycleEnvelope(t, 1, 1), lifecycleEnvelope(t, 2, 4096)
	publication := make(chan struct{}, 1)
	stopped := make(chan struct{})
	var calls atomic.Int32
	options := Options{MaxOutputBytes: 2048, Negotiate: subscriptionNegotiator, SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
		if calls.Add(1) == 1 {
			return base, nil
		}
		return large, nil
	}, SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
		select {
		case <-publication:
			return nil
		case <-ctx.Done():
			close(stopped)
			return ctx.Err()
		}
	}}
	output, done, writer := serveSubscription(t, options)
	writeSubscriptionHandshake(t, writer)
	publication <- struct{}{}
	time.Sleep(20 * time.Millisecond)
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), `"state":"terminated","reason":"output_limit"`); got != 1 {
		t.Fatalf("output limit terminal count=%d transcript=%s", got, output.String())
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("oversize update left waiter running")
	}
}

func TestSnapshotSubscriptionNotificationWriteFailureStopsPump(t *testing.T) {
	base, update := lifecycleEnvelope(t, 1, 1), lifecycleEnvelope(t, 2, 1)
	publication := make(chan struct{}, 1)
	stopped := make(chan struct{})
	var calls atomic.Int32
	want := errors.New("notification write failed")
	output := &subscriptionNotificationFailWriter{err: want}
	options := Options{Negotiate: subscriptionNegotiator, SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
		if calls.Add(1) == 1 {
			return base, nil
		}
		return update, nil
	}, SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
		select {
		case <-publication:
			return nil
		case <-ctx.Done():
			close(stopped)
			return ctx.Err()
		}
	}}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- New(options).Serve(context.Background(), reader, output) }()
	writeSubscriptionHandshake(t, writer)
	publication <- struct{}{}
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("Serve error = %v, want %v", err, want)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after subscription notification write failure")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("notification write failure left waiter running")
	}
	if got := output.notificationWrites.Load(); got != 1 {
		t.Fatalf("notification write attempts = %d, want 1", got)
	}
}

func lifecycleEnvelope(t *testing.T, revision uint64, nameSize int) SnapshotEnvelope {
	t.Helper()
	root, err := semantic.NewNode(semantic.NodeSpec{Key: &semantic.NodeKey{AppNamespace: "test", View: "lifecycle", Kind: "root", Entity: string(rune('0' + revision)), Slot: "main"}, Generation: uint32(revision), Role: "application", Name: strings.Repeat("x", nameSize)})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(revision, root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, hash := tree.Snapshot(), strings.Repeat("a", 64)
	return SnapshotEnvelope{Snapshot: &snapshot, Mode: "full", SessionID: "stave-session", Sequence: revision, Revision: revision, TreeHash: tree.Hash(), CapabilityHash: hash, SemanticVersion: tree.SchemaVersion(), ConfigHash: hash, ThemeHash: hash, WidthVersion: "width-v1"}
}

func subscriptionNegotiator(context.Context, map[string]any) (capability.Manifest, error) {
	return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
}

func serveSubscription(t *testing.T, options Options) (*bytes.Buffer, <-chan error, *io.PipeWriter) {
	t.Helper()
	reader, writer := io.Pipe()
	output := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() { done <- New(options).Serve(context.Background(), reader, output) }()
	return output, done, writer
}

func writeSubscriptionHandshake(t *testing.T, writer *io.PipeWriter) {
	t.Helper()
	_, err := writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}` + "\n" + `{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
}

type subscriptionNotificationFailWriter struct {
	err                error
	notificationWrites atomic.Int32
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func (c *subscriptionTestClient) responseError(id int) {
	c.t.Helper()
	b := c.line()
	var response struct {
		ID    int             `json:"id"`
		Error *protocol.Error `json:"error"`
	}
	if err := json.Unmarshal(b, &response); err != nil || response.ID != id || response.Error == nil {
		c.t.Fatalf("response %d: %s (%v)", id, b, err)
	}
}

func (w *subscriptionNotificationFailWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"method":"stave.snapshot.subscription"`)) {
		w.notificationWrites.Add(1)
		return 0, w.err
	}
	return len(p), nil
}
