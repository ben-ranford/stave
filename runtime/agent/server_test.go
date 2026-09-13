package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/config"
	"github.com/ben-ranford/stave/diag"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/semantic"
)

func TestOptionsFromConfigProjectsOnlyAgentLimits(t *testing.T) {
	defaults, err := OptionsFromConfig(config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if defaults.MaxMessageBytes != 4<<20 || defaults.MaxTreeNodes != 100_000 {
		t.Fatalf("default projection = %+v", defaults)
	}

	cfg := config.Defaults()
	cfg.Protocol.MaxMessageBytes = 8192
	cfg.Security.MaxTreeNodes = 42
	cfg.Runtime.InputQueue = 7
	projected, err := OptionsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if projected.MaxMessageBytes != 8192 || projected.MaxTreeNodes != 42 {
		t.Fatalf("projected limits = %+v", projected)
	}
	if projected.MaxOutputBytes != 0 || projected.Queue != 0 || projected.MaxInFlight != 0 {
		t.Fatalf("adapter-only options were implicitly projected: %+v", projected)
	}

	projected.Queue = 3
	projected.MaxTreeNodes = 17
	server := New(projected)
	if server.opt.Queue != 3 || server.limits.MaxTreeNodes != 17 || server.limits.MaxMessageBytes != 8192 {
		t.Fatalf("explicit adapter overrides were not retained: %+v", server.opt)
	}

	cfg.Protocol.MaxMessageBytes = 0
	if _, err := OptionsFromConfig(cfg); err == nil {
		t.Fatal("invalid config was projected")
	}
}

func input(value string) io.ReadCloser { return io.NopCloser(strings.NewReader(value)) }

func registerTestAction(t *testing.T, registry *action.Registry, def action.Definition, handler func(context.Context, action.Call, any) (any, error)) {
	t.Helper()
	if err := registry.Register(def, handler); err != nil {
		t.Fatal(err)
	}
}

func simpleActionDefinition(id action.ID, safety action.Safety) action.Definition {
	return action.Definition{
		ID:           id,
		Version:      "1",
		Title:        string(id),
		InputSchema:  action.Schema{ID: string(id) + "-input", JSON: json.RawMessage(`{}`)},
		OutputSchema: action.Schema{ID: string(id) + "-output", JSON: json.RawMessage(`{}`)},
		Safety:       safety,
	}
}

func testTargetNodeID(t *testing.T) semantic.NodeID {
	t.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "test", View: "view", Kind: "button", Entity: "one", Slot: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestServerHandshakeAndStdoutPurity(t *testing.T) {
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"]}}\n" + "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n" + "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.ping\"}\n")
	var out bytes.Buffer
	if err := New(Options{ServerName: "test", CompatibilityMode: true}).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d output=%s", len(lines), out.String())
	}
	if !strings.Contains(out.String(), `"protocolVersion":"1.0"`) {
		t.Fatal(out.String())
	}
	if !strings.Contains(out.String(), `"application":{"id":"test","version":"development"}`) {
		t.Fatalf("application identity missing from initialize result: %s", out.String())
	}
}

func TestServerInitializeUsesConfiguredApplicationIdentity(t *testing.T) {
	var out bytes.Buffer
	opts := Options{CompatibilityMode: true, ServerName: "host", Application: protocol.Application{ID: "atlas", Version: "2.3.0"}}
	if err := New(opts).Serve(context.Background(), input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"application":{"id":"atlas","version":"2.3.0"}`) {
		t.Fatalf("configured application identity missing: %s", out.String())
	}
}

func TestServerCancellationReservedLaneWhileInvokeBlocks(t *testing.T) {
	started := make(chan struct{})
	var cancelled atomic.Bool
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("x"), action.ReadOnly)
	registerTestAction(t, registry, def, func(ctx context.Context, c action.Call, _ any) (any, error) {
		close(started)
		<-ctx.Done()
		cancelled.Store(true)
		return nil, &action.Error{Code: action.Cancelled, Message: "cancelled"}
	})
	opts := Options{CompatibilityMode: true, Actions: registry, Authorize: func(context.Context, action.Call) *action.Error { return nil }}
	in := input("{" + `"jsonrpc":"2.0","id":1,"method":"stave.initialize"` + "}\n" + "{" + `"jsonrpc":"2.0","id":2,"method":"stave.initialized"` + "}\n" + "{" + `"jsonrpc":"2.0","id":3,"method":"stave.action.invoke","params":{"callId":"c","actionId":"x"}` + "}\n" + "{" + `"jsonrpc":"2.0","id":4,"method":"stave.action.cancel","params":{"callId":"c"}` + "}\n")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := New(opts).Serve(ctx, in, &out); err != nil && !strings.Contains(err.Error(), "context deadline") {
		t.Fatal(err)
	}
	if !cancelled.Load() || !strings.Contains(out.String(), `"id":4`) {
		t.Fatalf("cancel did not get reserved response: cancelled=%v output=%s", cancelled.Load(), out.String())
	}
}

func TestServerNegotiationDoesNotEchoClientOffer(t *testing.T) {
	var out bytes.Buffer
	client := map[string]any{"color": "truecolor", "interactive": true}
	opts := Options{Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
		return capability.Manifest{ProtocolVersions: []string{"1.0"}, Color: capability.ColorANSI16, TTY: true}, nil
	}}
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "stave.initialize", "params": map[string]any{"capabilities": client}})
	if err := New(opts).Serve(context.Background(), input(string(line)+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "truecolor") {
		t.Fatalf("client capability echoed: %s", out.String())
	}
}

func TestServerRejectsMalformedTargetAndRequiresConfirmation(t *testing.T) {
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("x"), action.Consequential)
	def.Confirmation.Required = true
	registerTestAction(t, registry, def, func(context.Context, action.Call, any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	var confirmed action.Confirmation
	opts := Options{
		CompatibilityMode: true,
		Actions:           registry,
		Authorize:         func(context.Context, action.Call) *action.Error { return nil },
		Confirm: func(_ context.Context, c action.Call) (action.Confirmation, error) {
			conf, err := action.NewConfirmation("stave-session", def, c.Target, c.Arguments, time.Now().Add(time.Minute))
			if err != nil {
				return action.Confirmation{}, err
			}
			confirmed = conf
			return conf, nil
		},
	}
	s := New(opts)
	var out bytes.Buffer
	nodeID := testTargetNodeID(t)
	malformedTarget := fmt.Sprintf(`{"nodeId":%q,"generation":1,"observedRevision":2,"unexpected":true}`, nodeID)
	first := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c1\",\"actionId\":\"x\",\"target\":{\"nodeId\":%q,\"generation\":1,\"observedRevision\":2},\"arguments\":{\"message\":\"hello\"}}}\n{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.action.confirm\",\"params\":{\"callId\":\"c2\",\"actionId\":\"x\",\"target\":%s,\"arguments\":{\"message\":\"hello\"}}}\n", nodeID, malformedTarget)
	if err := s.Serve(context.Background(), input(first), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"code":-32008`) {
		t.Fatalf("confirmation requirement was bypassed: %s", out.String())
	}
	if !strings.Contains(out.String(), `"code":-32602`) {
		t.Fatalf("malformed target was accepted: %s", out.String())
	}

	out.Reset()
	validConfirm := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"stave.action.confirm\",\"params\":{\"callId\":\"c3\",\"actionId\":\"x\",\"target\":{\"nodeId\":%q,\"generation\":1,\"observedRevision\":2},\"arguments\":{\"message\":\"hello\"}}}\n", nodeID)
	if err := s.Serve(context.Background(), input(validConfirm), &out); err != nil {
		t.Fatal(err)
	}
	if confirmed.Token == "" || confirmed.SessionID != "stave-session" {
		t.Fatalf("confirmation was not issued: %+v", confirmed)
	}

	out.Reset()
	invokeWithConfirmation := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":6,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c4\",\"actionId\":\"x\",\"target\":{\"nodeId\":%q,\"generation\":1,\"observedRevision\":2},\"arguments\":{\"message\":\"hello\"},\"confirmation\":{\"token\":%q,\"sessionId\":%q}}}\n", nodeID, confirmed.Token, confirmed.SessionID)
	if err := s.Serve(context.Background(), input(invokeWithConfirmation), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"callId":"c4"`) || strings.Contains(out.String(), `"error"`) {
		t.Fatalf("confirmed invoke did not complete cleanly: %s", out.String())
	}
}

func TestDecodeStrictTargetRejectsTrailingJSON(t *testing.T) {
	var target semantic.Target
	if err := decodeStrictTarget([]byte(`{"nodeId":"n1_bad"} {"unexpected":true}`), &target); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestDecodeStrictTargetRequiresStableReference(t *testing.T) {
	id := testTargetNodeID(t)
	for name, raw := range map[string]string{
		"invalid node id":        `{"nodeId":"n1_bad","generation":1,"observedRevision":1}`,
		"zero generation":        fmt.Sprintf(`{"nodeId":%q,"generation":0,"observedRevision":1}`, id),
		"zero observed revision": fmt.Sprintf(`{"nodeId":%q,"generation":1,"observedRevision":0}`, id),
		"empty object":           `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			var target semantic.Target
			if err := decodeStrictTarget([]byte(raw), &target); err == nil {
				t.Fatal("invalid target was accepted")
			}
		})
	}
}

func TestDecodeStrictTargetAllowsTargetless(t *testing.T) {
	for _, raw := range []string{"", "null", " null "} {
		var target semantic.Target
		if err := decodeStrictTarget([]byte(raw), &target); err != nil {
			t.Fatalf("targetless %q rejected: %v", raw, err)
		}
		if target != (semantic.Target{}) {
			t.Fatalf("targetless %q decoded as %+v", raw, target)
		}
	}
}

func TestServerPreparedAuthorizationBindsTrustedPolicy(t *testing.T) {
	var got action.Call
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("x"), action.ReadOnly)
	registerTestAction(t, registry, def, func(_ context.Context, c action.Call, _ any) (any, error) {
		got = c
		return map[string]any{}, nil
	})
	opts := Options{CompatibilityMode: true, Actions: registry, AuthorizePrepared: func(_ context.Context, c action.Call) (action.Call, *action.Error) {
		c.PolicyID, c.PolicyEpoch, c.Target.ObservedRevision = "trusted", 7, 42
		return c, nil
	}}
	nodeID := testTargetNodeID(t)
	params := fmt.Sprintf(`{"callId":"c","actionId":"x","target":{"nodeId":%q,"generation":1,"observedRevision":1},"arguments":{}}`, nodeID)
	in := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":%s}\n", params)
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), input(in), &out); err != nil {
		t.Fatal(err)
	}
	if got.PolicyID != "trusted" || got.PolicyEpoch != 7 || got.Target.ObservedRevision != 42 {
		t.Fatalf("untrusted call reached handler: %+v", got)
	}
}

func TestServerConfirmationBindsPreparedPolicy(t *testing.T) {
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("confirm-policy"), action.Consequential)
	def.Confirmation.Required = true
	registerTestAction(t, registry, def, func(_ context.Context, _ action.Call, _ any) (any, error) { return map[string]any{}, nil })
	epoch := uint64(7)
	s := New(Options{
		Actions: registry,
		AuthorizePrepared: func(_ context.Context, c action.Call) (action.Call, *action.Error) {
			c.PolicyID, c.PolicyEpoch = "trusted", epoch
			return c, nil
		},
		Confirm: func(_ context.Context, c action.Call) (action.Confirmation, error) {
			return action.NewConfirmation(c.SessionID, def, c.Target, c.Arguments, time.Now().Add(time.Minute))
		},
	})
	fail := func(code int, message string) protocol.Response {
		return protocol.Response{Error: &protocol.Error{Code: code, Message: message}}
	}
	confirm := func() protocol.ConfirmationPresentation {
		r := protocol.Request{Params: json.RawMessage(`{"callId":"confirm","actionId":"confirm-policy","arguments":{}}`)}
		response := s.confirm(context.Background(), r, protocol.Response{}, fail)
		if response.Error != nil {
			t.Fatalf("confirmation failed: %+v", response.Error)
		}
		presentation, ok := response.Result.(protocol.ConfirmationPresentation)
		if !ok {
			t.Fatalf("unexpected confirmation response: %#v", response.Result)
		}
		return presentation
	}
	invoke := func(p protocol.ConfirmationPresentation) protocol.Response {
		params, err := json.Marshal(protocol.InvokeParams{CallID: "invoke", ActionID: "confirm-policy", Arguments: json.RawMessage(`{}`), Confirmation: &p})
		if err != nil {
			t.Fatal(err)
		}
		return s.invoke(context.Background(), protocol.Request{Params: params}, protocol.Response{}, fail)
	}
	if response := invoke(confirm()); response.Error != nil {
		t.Fatalf("matching policy was rejected: %+v", response.Error)
	}
	presentation := confirm()
	epoch++
	if response := invoke(presentation); response.Error == nil || response.Error.Code != protocol.ConfirmationInvalid {
		t.Fatalf("changed policy epoch accepted: %+v", response)
	}
}

func TestServerConfirmationCapacityIsResourceLimit(t *testing.T) {
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("confirm-capacity"), action.ReadOnly)
	s := New(Options{
		Actions:   registry,
		Authorize: func(context.Context, action.Call) *action.Error { return nil },
		Confirm: func(_ context.Context, c action.Call) (action.Confirmation, error) {
			return action.NewConfirmation(c.SessionID, def, c.Target, c.Arguments, time.Now().Add(time.Minute))
		},
	})
	fail := func(code int, message string) protocol.Response {
		return protocol.Response{Error: &protocol.Error{Code: code, Message: message}}
	}
	request := protocol.Request{Params: json.RawMessage(`{"callId":"confirm","actionId":"confirm-capacity","arguments":{}}`)}
	for i := 0; i < 1024; i++ {
		if response := s.confirm(context.Background(), request, protocol.Response{}, fail); response.Error != nil {
			t.Fatalf("confirmation %d failed: %+v", i, response.Error)
		}
	}
	if response := s.confirm(context.Background(), request, protocol.Response{}, fail); response.Error == nil || response.Error.Code != protocol.ResourceLimit {
		t.Fatalf("capacity error = %+v, want protocol.ResourceLimit", response)
	}
}

func TestServerOversizedResponseIsTyped(t *testing.T) {
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("x"), action.ReadOnly)
	registerTestAction(t, registry, def, func(_ context.Context, c action.Call, _ any) (any, error) {
		return json.RawMessage(`"` + strings.Repeat("x", 512) + `"`), nil
	})
	opts := Options{CompatibilityMode: true, MaxOutputBytes: 128, Actions: registry, Authorize: func(context.Context, action.Call) *action.Error { return nil }}
	in := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c\",\"actionId\":\"x\"}}\n"
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), input(in), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"code":-32013`) || !strings.Contains(out.String(), `"id":3`) {
		t.Fatalf("missing typed output limit: %s", out.String())
	}
}
func TestServerRequiresInitialization(t *testing.T) {
	var out bytes.Buffer
	err := New(Options{}).Serve(context.Background(), input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.snapshot\"}\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-32006") {
		t.Fatalf("%s", out.String())
	}
}

func TestServerInvokeUsesCamelCaseDTOAndSafeTypedError(t *testing.T) {
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("x"), action.ReadOnly)
	registerTestAction(t, registry, def, func(_ context.Context, c action.Call, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	opts := Options{
		CompatibilityMode: true,
		Actions:           registry,
		Authorize:         func(context.Context, action.Call) *action.Error { return nil },
	}
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c\",\"actionId\":\"x\"}}\n")
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"callId":"c"`) || strings.Contains(out.String(), `"CallID"`) {
		t.Fatalf("transport leaked core result shape: %s", out.String())
	}
}

func TestServerNegotiatesLimitsAndRejectsReinitialize(t *testing.T) {
	opts := Options{MaxTreeNodes: 100, Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
		return capability.Manifest{ProtocolVersions: []string{protocol.Version}, Color: capability.ColorANSI256}, nil
	}}
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"],\"limits\":{\"maxMessageBytes\":2048,\"maxOutputBytes\":4096,\"maxTreeNodes\":17}}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"]}}\n")
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"maxTreeNodes":17`) || !strings.Contains(out.String(), `"code":-32600`) {
		t.Fatalf("limits/reinitialize contract failed: %s", out.String())
	}
}

func TestSnapshotEnvelopeTypedAndDiagnosticsIndependentOfActions(t *testing.T) {
	root, err := semantic.NewNode(semantic.NodeSpec{Key: &semantic.NodeKey{AppNamespace: "app", View: "view", Kind: "root", Entity: "root", Slot: "main"}, Generation: 1, Role: "application", Name: "App"})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(2, root)
	if err != nil {
		t.Fatal(err)
	}
	snap := tree.Snapshot()
	hash := strings.Repeat("a", 64)
	opts := Options{
		CompatibilityMode: true,
		SnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) {
			return SnapshotEnvelope{Snapshot: &snap, Mode: "full", SessionID: "stave-session", Sequence: 3, Revision: 2, TreeHash: tree.Hash(), CapabilityHash: hash, SemanticVersion: tree.SchemaVersion(), ConfigHash: hash, ThemeHash: hash, WidthVersion: "width-v1", Diagnostics: []diag.Diagnostic{{ID: "d", Severity: diag.Warning, Message: "secret", Redacted: true}}}, nil
		},
	}
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot\",\"params\":{\"mode\":\"full\",\"includeActions\":false}}\n")
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"diagnostics":[`) || strings.Contains(out.String(), "secret") {
		t.Fatalf("diagnostic contract failed: %s", out.String())
	}
}

func TestNotifyRejectsOversizeSynchronously(t *testing.T) {
	s := New(Options{CompatibilityMode: true, MaxOutputBytes: 128})
	params := json.RawMessage(`{"text":"` + strings.Repeat("x", 256) + `"}`)
	if !errors.Is(s.Notify(protocol.Notification{JSONRPC: protocol.JSONRPC, Method: "stave.progress", Params: params}), ErrOutputLimit) {
		t.Fatal("oversize notification reported success")
	}
}

func TestRejectedInvokeReleasesReservedCallID(t *testing.T) {
	opts := Options{CompatibilityMode: true, Authorize: func(context.Context, action.Call) *action.Error {
		return &action.Error{Code: action.Forbidden, Message: "sensitive policy reason"}
	}}
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c\",\"actionId\":\"x\"}}\n")
	var out bytes.Buffer
	s := New(opts)
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if err := s.Serve(context.Background(), input("{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c\",\"actionId\":\"x\"}}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "duplicate call id") || strings.Contains(out.String(), "sensitive policy reason") {
		t.Fatalf("reservation/error leaked: %s", out.String())
	}
}

func TestCallbackPanicBecomesInternalError(t *testing.T) {
	registry := action.NewRegistry()
	def := simpleActionDefinition(action.ID("x"), action.ReadOnly)
	registerTestAction(t, registry, def, func(context.Context, action.Call, any) (any, error) { panic("secret panic") })
	opts := Options{CompatibilityMode: true, Actions: registry, Authorize: func(context.Context, action.Call) *action.Error { return nil }}
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c\",\"actionId\":\"x\"}}\n")
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"code":-32603`) || strings.Contains(out.String(), "secret panic") {
		t.Fatalf("panic boundary failed: %s", out.String())
	}
}

func TestProductionInitializeRequiresVersionOffer(t *testing.T) {
	opts := Options{Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) { return capability.Manifest{}, nil }}
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"code":-32002`) {
		t.Fatalf("missing version offer accepted: %s", out.String())
	}
}

func TestServeCancellationInterruptsBlockingReader(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := New(Options{CompatibilityMode: true}).Serve(ctx, reader, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Serve error=%v, want deadline", err)
	}
}

func TestActionCancelInterruptsAuthorizationDeadlineContext(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	opts := Options{CompatibilityMode: true, Authorize: func(ctx context.Context, _ action.Call) *action.Error {
		close(started)
		<-ctx.Done()
		close(finished)
		return &action.Error{Code: action.Cancelled}
	}}
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c\",\"actionId\":\"x\",\"deadlineMs\":5000}}\n{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.action.cancel\",\"params\":{\"callId\":\"c\"}}\n")
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("authorization never started")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("authorization was not cancelled")
	}
}

func TestSessionCancelCancelsAllCallsAndStopsAdmission(t *testing.T) {
	var sessionCancelled atomic.Bool
	opts := Options{CompatibilityMode: true, CancelSession: func(context.Context) error { sessionCancelled.Store(true); return nil }, Authorize: func(ctx context.Context, _ action.Call) *action.Error {
		<-ctx.Done()
		return &action.Error{Code: action.Cancelled}
	}}
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"c\",\"actionId\":\"x\"}}\n{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.session.cancel\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"stave.snapshot\"}\n")
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !sessionCancelled.Load() || !strings.Contains(out.String(), `"id":4`) || !strings.Contains(out.String(), `"id":5,"error":{"code":-32004`) {
		t.Fatalf("session cancel contract failed: %s", out.String())
	}
}

func TestNegotiatedInputAndSnapshotModeLimitsAreEnforced(t *testing.T) {
	opts := Options{Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
		return capability.Manifest{ProtocolVersions: []string{protocol.Version}, SnapshotModes: []string{"full"}}, nil
	}, SnapshotEnvelope: func(context.Context, string, uint64) (SnapshotEnvelope, error) { return SnapshotEnvelope{}, nil }}
	oversize := strings.Repeat("x", 300)
	in := input("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\",\"params\":{\"protocolVersions\":[\"1.0\"],\"limits\":{\"maxMessageBytes\":256}}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot\",\"params\":{\"mode\":\"patch\",\"sinceRevision\":1}}\n{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.ping\",\"params\":{\"padding\":\"" + oversize + "\"}}\n")
	var out bytes.Buffer
	if err := New(opts).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":3,"error":{"code":-32007`) || !strings.Contains(out.String(), `"code":-32700`) {
		t.Fatalf("negotiated limits not enforced: %s", out.String())
	}
}
