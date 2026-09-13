package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
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
	out := &subscriptionTestWriter{done: ctx.Done(), err: ctx.Err, lines: make(chan []byte, 16)}
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
	c := &subscriptionTestClient{t: t, done: ctx.Done(), in: writer, out: out}
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
	out := &subscriptionTestWriter{done: ctx.Done(), err: ctx.Err, lines: make(chan []byte, 16)}
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
	c := &subscriptionTestClient{t: t, done: ctx.Done(), in: writer, out: out}
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
	c, finish := startSubscriptionLifecycleClient(t, options)
	c.request(`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`)
	baselineLine := c.line()
	var baseline struct {
		ID     int `json:"id"`
		Result struct {
			Snapshot protocol.SnapshotResult `json:"snapshot"`
		} `json:"result"`
	}
	if err := json.Unmarshal(baselineLine, &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.ID != 3 || len(baseline.Result.Snapshot.Actions) != 0 {
		t.Fatalf("subscription baseline exposed actions: %s", baselineLine)
	}
	if len(baseline.Result.Snapshot.Diagnostics) != 1 || !baseline.Result.Snapshot.Diagnostics[0].Redacted || bytes.Contains(baselineLine, []byte("secret diagnostic")) {
		t.Fatalf("subscription baseline did not redact diagnostics: %s", baselineLine)
	}
	if authorized.Load() != 0 || confirmed.Load() != 0 {
		t.Fatalf("subscription baseline invoked authority callbacks: authorize=%d confirm=%d", authorized.Load(), confirmed.Load())
	}
	finish()
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
	envelope.WidthVersion = strings.Repeat("x", 8192)
	var waiterCalls atomic.Int32
	options := Options{
		MaxOutputBytes: 2048,
		Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}, SnapshotSubscriptionVersions: []string{protocol.SnapshotSubscriptionVersion}}, nil
		},
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) { return envelope, nil },
		SnapshotPublicationWaiter: func(context.Context, uint64) error {
			waiterCalls.Add(1)
			return nil
		},
	}
	c, finish := startSubscriptionLifecycleClient(t, options)
	c.request(`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`)
	baseline := c.line()
	var response struct {
		ID    int             `json:"id"`
		Error *protocol.Error `json:"error"`
	}
	if err := json.Unmarshal(baseline, &response); err != nil || response.ID != 3 || response.Error == nil || response.Error.Code != protocol.OutputLimit {
		t.Fatalf("oversize baseline response = %s (%v)", baseline, err)
	}
	finish()
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

func TestSnapshotSubscriptionResponseAndNotifyWriteFailuresStopPump(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure string
		trigger func(*Server, *io.PipeWriter)
	}{
		{name: "duplicate subscribe", failure: `"id":4`, trigger: func(_ *Server, input *io.PipeWriter) {
			_, _ = io.WriteString(input, `{"jsonrpc":"2.0","id":4,"method":"stave.snapshot.subscribe"}`+"\n")
		}},
		{name: "invalid unsubscribe", failure: `"id":4`, trigger: func(_ *Server, input *io.PipeWriter) {
			_, _ = io.WriteString(input, `{"jsonrpc":"2.0","id":4,"method":"stave.snapshot.unsubscribe","params":{"unexpected":true}}`+"\n")
		}},
		{name: "ordinary notify", failure: `"method":"stave.progress"`, trigger: func(server *Server, _ *io.PipeWriter) {
			_ = server.Notify(protocol.Notification{JSONRPC: protocol.JSONRPC, Method: "stave.progress"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stopped := make(chan struct{})
			entered := make(chan struct{})
			want := errors.New("write failed")
			output := &subscriptionSelectiveFailWriter{err: want, failure: []byte(test.failure)}
			options := Options{Negotiate: subscriptionNegotiator, SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
				return subscriptionEnvelope(t), nil
			}, SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
				select {
				case <-entered:
				default:
					close(entered)
				}
				<-ctx.Done()
				close(stopped)
				return ctx.Err()
			}}
			server := New(options)
			reader, input := io.Pipe()
			done := make(chan error, 1)
			go func() { done <- server.Serve(context.Background(), reader, output) }()
			writeSubscriptionHandshake(t, input)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("subscription waiter did not start")
			}
			output.armed.Store(true)
			test.trigger(server, input)
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("Serve error = %v, want %v", err, want)
				}
			case <-time.After(time.Second):
				t.Fatal("Serve did not return after write failure")
			}
			if _, err := io.WriteString(input, "{}\n"); err == nil {
				t.Fatal("writer failure did not close the owned input")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("write failure left subscription waiter running")
			}
		})
	}
}

func TestSnapshotSubscriptionWriterPanicStopsPumpAndClosesInput(t *testing.T) {
	stopped := make(chan struct{})
	entered := make(chan struct{})
	output := &subscriptionPanicWriter{failure: []byte(`"method":"stave.progress"`)}
	options := Options{Negotiate: subscriptionNegotiator, SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
		return subscriptionEnvelope(t), nil
	}, SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
		close(entered)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}
	server := New(options)
	reader, input := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, output) }()
	writeSubscriptionHandshake(t, input)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscription waiter did not start")
	}
	output.armed.Store(true)
	if err := server.Notify(protocol.Notification{JSONRPC: protocol.JSONRPC, Method: "stave.progress"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "protocol writer panic") {
			t.Fatalf("Serve error = %v, want writer panic", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after writer panic")
	}
	if _, err := io.WriteString(input, "{}\n"); err == nil {
		t.Fatal("writer panic did not close the owned input")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("writer panic left subscription waiter running")
	}
}

func TestSnapshotSubscriptionSessionCancelStopsPumpWithoutClosingSession(t *testing.T) {
	stopped := make(chan struct{})
	entered := make(chan struct{})
	options := Options{Negotiate: subscriptionNegotiator, CancelSession: func(context.Context) error { return nil }, SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
		return subscriptionEnvelope(t), nil
	}, SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
		close(entered)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}
	reader, input := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- New(options).Serve(context.Background(), reader, io.Discard) }()
	writeSubscriptionHandshake(t, input)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscription waiter did not start")
	}
	_, _ = io.WriteString(input, `{"jsonrpc":"2.0","id":4,"method":"stave.session.cancel","params":{}}`+"\n")
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("session cancellation left subscription waiter running")
	}
	_ = input.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotSubscriptionServerCloseDuringBaselineWriteStopsPump(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	gate, blocked := make(chan struct{}), make(chan struct{})
	writer := &baselineGateWriter{gate: gate, blocked: blocked, notification: make(chan struct{}, 1)}
	options := Options{Negotiate: subscriptionNegotiator, SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
		return subscriptionEnvelope(t), nil
	}, SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
		close(entered)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}
	server := New(options)
	reader, input := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, writer) }()
	_, _ = io.WriteString(input, `{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`+"\n"+`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`+"\n"+`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`+"\n")
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("baseline write did not block")
	}
	server.Close()
	close(gate)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscription waiter was not installed")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Server.Close left subscription waiter running")
	}
	_ = input.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotSubscriptionCancellationCancelsBlockedBaselineProvider(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel func(*Server, *io.PipeWriter)
	}{
		{name: "server close", cancel: func(server *Server, _ *io.PipeWriter) { server.Close() }},
		{name: "client EOF", cancel: func(_ *Server, input *io.PipeWriter) { _ = input.Close() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			providerStarted := make(chan struct{})
			providerStopped := make(chan struct{})
			options := Options{
				Negotiate: subscriptionNegotiator,
				SubscriptionSnapshotEnvelope: func(ctx context.Context, _ string, _ uint64) (SnapshotEnvelope, error) {
					close(providerStarted)
					<-ctx.Done()
					close(providerStopped)
					return lifecycleEnvelope(t, 1, 1), nil
				},
				SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error { <-ctx.Done(); return ctx.Err() },
			}
			server := New(options)
			reader, input := io.Pipe()
			var output bytes.Buffer
			done := make(chan error, 1)
			go func() { done <- server.Serve(context.Background(), reader, &output) }()
			joined := false
			t.Cleanup(func() {
				server.Close()
				_ = input.Close()
				if joined {
					return
				}
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("Serve cleanup error = %v", err)
					}
				case <-time.After(time.Second):
					t.Error("Serve cleanup did not finish")
				}
			})
			writeSubscriptionHandshake(t, input)
			select {
			case <-providerStarted:
			case <-time.After(time.Second):
				t.Fatal("baseline provider did not start")
			}
			test.cancel(server, input)
			select {
			case <-providerStopped:
			case <-time.After(time.Second):
				t.Fatal("cancellation did not stop the baseline provider")
			}
			_ = input.Close()
			select {
			case err := <-done:
				joined = true
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("Serve did not finish")
			}
			if !bytes.Contains(output.Bytes(), []byte(`"id":3,"error":{"code":-32004`)) {
				t.Fatalf("cancelled baseline response was not written: %s", output.String())
			}
		})
	}
}

func TestSnapshotSubscriptionClientEOFDrainsQueuedShutdownAcknowledgement(t *testing.T) {
	providerStarted := make(chan struct{})
	providerStopped := make(chan struct{})
	options := Options{
		Queue:     1,
		Negotiate: subscriptionNegotiator,
		SubscriptionSnapshotEnvelope: func(ctx context.Context, _ string, _ uint64) (SnapshotEnvelope, error) {
			close(providerStarted)
			<-ctx.Done()
			close(providerStopped)
			return SnapshotEnvelope{}, ctx.Err()
		},
		SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`,
		`{"jsonrpc":"2.0","id":4,"method":"stave.session.shutdown"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	server := New(options)
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), input(requests), &output) }()
	joined := false
	defer func() {
		server.Close()
		if joined {
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve cleanup error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve cleanup did not finish")
		}
	}()
	select {
	case <-providerStarted:
	case <-time.After(time.Second):
		t.Fatal("baseline provider did not start")
	}
	select {
	case <-providerStopped:
	case <-time.After(time.Second):
		t.Fatal("client EOF did not cancel the queued baseline provider")
	}
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not drain queued requests after client EOF")
	}
	responses := map[int]*protocol.Error{}
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'}) {
		var response struct {
			ID    int             `json:"id"`
			Error *protocol.Error `json:"error"`
		}
		if err := json.Unmarshal(line, &response); err == nil {
			responses[response.ID] = response.Error
		}
	}
	if response := responses[3]; response == nil || response.Code != protocol.Cancelled {
		t.Fatalf("baseline response after EOF = %+v, want cancelled: %s", response, output.String())
	}
	if response, found := responses[4]; !found || response != nil {
		t.Fatalf("queued shutdown acknowledgement = %+v, found=%v: %s", response, found, output.String())
	}
}

func TestSnapshotSubscriptionBaselineDoesNotBlockOrdinaryRequests(t *testing.T) {
	providerStarted := make(chan struct{})
	providerStopped := make(chan struct{})
	options := Options{
		Queue:     1,
		Negotiate: subscriptionNegotiator,
		SubscriptionSnapshotEnvelope: func(ctx context.Context, _ string, _ uint64) (SnapshotEnvelope, error) {
			close(providerStarted)
			<-ctx.Done()
			close(providerStopped)
			return SnapshotEnvelope{}, ctx.Err()
		},
		SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	c, finish := startSubscriptionLifecycleClient(t, options)
	c.request(`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`)
	select {
	case <-providerStarted:
	case <-c.done:
		t.Fatal("baseline provider did not start")
	}
	c.request(`{"jsonrpc":"2.0","id":4,"method":"stave.ping"}`)
	c.response(4)
	_ = c.in.Close()
	select {
	case <-providerStopped:
	case <-c.done:
		t.Fatal("EOF did not cancel the baseline provider")
	}
	c.responseError(3)
	finish()
}

func TestSnapshotSubscriptionInvalidSessionCancelDoesNotCancelPendingBaseline(t *testing.T) {
	providerStarted := make(chan struct{})
	providerStopped := make(chan struct{})
	options := Options{
		Negotiate: subscriptionNegotiator,
		SubscriptionSnapshotEnvelope: func(ctx context.Context, _ string, _ uint64) (SnapshotEnvelope, error) {
			close(providerStarted)
			<-ctx.Done()
			close(providerStopped)
			return SnapshotEnvelope{}, ctx.Err()
		},
		SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	c, finish := startSubscriptionLifecycleClient(t, options)
	c.request(`{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe"}`)
	select {
	case <-providerStarted:
	case <-c.done:
		t.Fatal("baseline provider did not start")
	}
	c.request(`{"jsonrpc":"2.0","id":4,"method":"stave.session.cancel","params":{"unexpected":true}}`)
	line := c.line()
	var invalidCancel struct {
		ID    int             `json:"id"`
		Error *protocol.Error `json:"error"`
	}
	if err := json.Unmarshal(line, &invalidCancel); err != nil || invalidCancel.ID != 4 || invalidCancel.Error == nil || invalidCancel.Error.Code != protocol.InvalidParams {
		t.Fatalf("invalid session cancel response = %s (%v)", line, err)
	}
	select {
	case <-providerStopped:
		t.Fatal("invalid session cancel stopped the baseline provider")
	default:
	}
	_ = c.in.Close()
	select {
	case <-providerStopped:
	case <-c.done:
		t.Fatal("EOF did not cancel the baseline provider")
	}
	c.responseError(3)
	finish()
}

func TestSnapshotSubscriptionShutdownAcknowledgementIsTerminal(t *testing.T) {
	base := lifecycleEnvelope(t, 1, 1)
	update := lifecycleEnvelope(t, 2, 1)
	publication := make(chan struct{})
	started, pending, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var sequence atomic.Uint64
	sequence.Store(1)
	options := Options{
		Negotiate: subscriptionNegotiator,
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
			if sequence.Load() == 2 {
				return update, nil
			}
			return base, nil
		},
		SnapshotPublicationWaiter: func(ctx context.Context, after uint64) error {
			switch after {
			case 1:
				close(started)
				select {
				case <-publication:
					return nil
				case <-ctx.Done():
					close(stopped)
					return ctx.Err()
				}
			case 2:
				close(pending)
				<-ctx.Done()
				close(stopped)
				return ctx.Err()
			default:
				return errors.New("unexpected subscription sequence")
			}
		},
	}
	server := New(options)
	reader, input := io.Pipe()
	writer := &shutdownAckWriter{progressBlocked: make(chan struct{}), releaseProgress: make(chan struct{}), lines: make(chan []byte, 16)}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, writer) }()
	serveJoined := false
	defer func() {
		writer.release()
		server.Close()
		_ = input.Close()
		if serveJoined {
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve cleanup error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve cleanup did not finish")
		}
	}()
	writeSubscriptionHandshake(t, input)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("subscription waiter did not start")
	}
	if err := server.Notify(protocol.Notification{JSONRPC: protocol.JSONRPC, Method: "stave.progress"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.progressBlocked:
	case <-time.After(time.Second):
		t.Fatal("progress notification did not block the writer")
	}
	sequence.Store(2)
	close(publication)
	select {
	case <-pending:
	case <-time.After(time.Second):
		t.Fatal("subscription update was not pending")
	}
	if _, err := io.WriteString(input, `{"jsonrpc":"2.0","id":4,"method":"stave.session.shutdown"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not stop the subscription before its acknowledgement")
	}
	writer.release()
	writer.response(t, 4)
	if output := writer.String(); strings.Contains(output, `"method":"stave.snapshot.subscription"`) {
		t.Fatalf("subscription notification followed shutdown acknowledgement: %s", output)
	}
	_ = input.Close()
	select {
	case err := <-done:
		serveJoined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not finish")
	}
}

func TestSnapshotSubscriptionServerCloseStopsBlockedWriterSubscription(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	options := Options{
		Negotiate: subscriptionNegotiator,
		SubscriptionSnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
			return lifecycleEnvelope(t, 1, 1), nil
		},
		SnapshotPublicationWaiter: func(ctx context.Context, _ uint64) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			return ctx.Err()
		},
	}
	server := New(options)
	reader, input := io.Pipe()
	writer := &shutdownAckWriter{progressBlocked: make(chan struct{}), releaseProgress: make(chan struct{}), lines: make(chan []byte, 16)}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, writer) }()
	serveJoined := false
	defer func() {
		writer.release()
		server.Close()
		_ = input.Close()
		if serveJoined {
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve cleanup error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve cleanup did not finish")
		}
	}()
	writeSubscriptionHandshake(t, input)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("subscription waiter did not start")
	}
	if err := server.Notify(protocol.Notification{JSONRPC: protocol.JSONRPC, Method: "stave.progress"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.progressBlocked:
	case <-time.After(time.Second):
		t.Fatal("progress notification did not block the writer")
	}
	server.Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Server.Close did not stop subscription while writer was blocked")
	}
	writer.release()
	_ = input.Close()
	select {
	case err := <-done:
		serveJoined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not finish")
	}
}

type shutdownAckWriter struct {
	mu sync.Mutex
	bytes.Buffer
	progressBlocked, releaseProgress chan struct{}
	lines                            chan []byte
	once                             sync.Once
	releaseOnce                      sync.Once
}

func (w *shutdownAckWriter) release() {
	w.releaseOnce.Do(func() { close(w.releaseProgress) })
}

func (w *shutdownAckWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"method":"stave.progress"`)) {
		w.once.Do(func() { close(w.progressBlocked) })
		<-w.releaseProgress
	}
	line := append([]byte(nil), p...)
	w.mu.Lock()
	n, err := w.Buffer.Write(line)
	w.mu.Unlock()
	if err != nil {
		return n, err
	}
	w.lines <- line
	return n, nil
}

func (w *shutdownAckWriter) response(t *testing.T, id int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case line := <-w.lines:
			var response struct {
				ID    int
				Error *protocol.Error
			}
			if err := json.Unmarshal(line, &response); err == nil && response.ID == id {
				if response.Error != nil {
					t.Fatalf("shutdown response = %s", line)
				}
				return
			}
		case <-deadline:
			t.Fatal("shutdown acknowledgement was not written")
		}
	}
}

func (w *shutdownAckWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.String()
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

func startSubscriptionLifecycleClient(t *testing.T, options Options) (*subscriptionTestClient, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	reader, writer := io.Pipe()
	output := &subscriptionTestWriter{done: ctx.Done(), err: ctx.Err, lines: make(chan []byte, 8)}
	done := make(chan error, 1)
	go func() { done <- New(options).Serve(ctx, reader, output) }()
	joined := false
	finish := func() {
		if joined {
			return
		}
		_ = writer.Close()
		select {
		case err := <-done:
			joined = true
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Serve did not finish")
		}
	}
	t.Cleanup(func() {
		_ = writer.Close()
		if joined {
			return
		}
		select {
		case err := <-done:
			joined = true
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("Serve cleanup error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve cleanup did not finish")
		}
	})
	t.Cleanup(cancel)
	c := &subscriptionTestClient{t: t, done: ctx.Done(), in: writer, out: output}
	c.request(`{"jsonrpc":"2.0","id":1,"method":"stave.initialize","params":{"protocolVersions":["1.0"],"capabilities":{"snapshotSubscriptionVersions":["stave.snapshot.subscribe/v1"]}}}`)
	c.response(1)
	c.request(`{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}`)
	c.response(2)
	return c, finish
}

type subscriptionNotificationFailWriter struct {
	err                error
	notificationWrites atomic.Int32
}

type subscriptionSelectiveFailWriter struct {
	err     error
	failure []byte
	armed   atomic.Bool
}

type subscriptionPanicWriter struct {
	failure []byte
	armed   atomic.Bool
}

func (w *subscriptionPanicWriter) Write(p []byte) (int, error) {
	if w.armed.Load() && bytes.Contains(p, w.failure) {
		panic("writer panic")
	}
	return len(p), nil
}

func (w *subscriptionSelectiveFailWriter) Write(p []byte) (int, error) {
	if w.armed.Load() && bytes.Contains(p, w.failure) {
		return 0, w.err
	}
	return len(p), nil
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
