package dualruntime

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/runtime/agent"
	"github.com/ben-ranford/stave/runtime/human"
)

func waitCount(t *testing.T, app *Application, want int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for ctx.Err() == nil {
		s, err := app.Prepared.Session.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if s.Model.Count == want {
			return s.Tree.Hash()
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("count did not reach %d", want)
	return ""
}

func TestHumanLineAndAgentJSONLShareActionTree(t *testing.T) {
	humanApp, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer humanApp.Close()
	var humanOutput bytes.Buffer
	driver, err := human.NewLineDriver(human.LineDriverOptions{Input: strings.NewReader("inc\n"), Output: &humanOutput, TTY: false})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := human.New(humanApp.HumanOptions(driver))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	humanHash := waitCount(t, humanApp, 1)

	agentApp, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer agentApp.Close()
	opts, err := agentApp.AgentOptions(agent.Options{Actions: agentApp.Registry, CompatibilityMode: true, Authorize: func(context.Context, action.Call) *action.Error { return nil }, Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
		return capability.Manifest{ProtocolVersions: []string{protocol.Version}, OutputMode: capability.OutputMachineJSONL, SnapshotModes: []string{"full"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	in := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot\",\"params\":{\"mode\":\"full\"}}\n{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"inc\",\"actionId\":\"example.increment.v1\"}}\n")
	if err := agent.New(opts).Serve(context.Background(), ioNopCloser{Reader: in}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"mode":"full"`) || !strings.Contains(out.String(), `"sequence":1`) {
		t.Fatalf("initial full snapshot missing valid envelope: %s", out.String())
	}
	agentHash := waitCount(t, agentApp, 1)
	if humanHash != agentHash {
		t.Fatalf("tree hashes differ: human=%s agent=%s", humanHash, agentHash)
	}
}

func TestApplicationCloseAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	app.Close()
}

type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }
