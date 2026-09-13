package dualruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/runtime/agent"
	"github.com/ben-ranford/stave/runtime/human"
	"github.com/ben-ranford/stave/state"
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
	var humanOutput bytes.Buffer
	driver, err := human.NewLineDriver(human.LineDriverOptions{Input: strings.NewReader("inc\n"), Output: &humanOutput, TTY: false})
	if err != nil {
		t.Fatal(err)
	}
	humanManifest, err := driver.Open(context.Background(), capability.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	humanApp, err := New(context.Background(), humanManifest)
	if err != nil {
		t.Fatal(err)
	}
	defer humanApp.Close()
	runtime, err := human.New(humanApp.HumanOptions(driver))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if humanOutput.String() != "Count: count: 1\n" {
		t.Fatalf("human host rendered before applying increment: %q", humanOutput.String())
	}
	humanHash := waitCount(t, humanApp, 1)

	agentApp, err := New(context.Background(), AgentManifest())
	if err != nil {
		t.Fatal(err)
	}
	defer agentApp.Close()
	opts, err := agentApp.AgentOptions(agent.Options{CompatibilityMode: true})
	if err != nil {
		t.Fatal(err)
	}
	server := agent.New(opts)
	var initial bytes.Buffer
	if err := server.Serve(context.Background(), ioNopCloser{Reader: strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"stave.initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"stave.initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"stave.snapshot\",\"params\":{\"mode\":\"full\"}}\n")}, &initial); err != nil {
		t.Fatal(err)
	}
	assertAgentManifestAndSnapshot(t, initial.String(), agentApp)
	var invoked bytes.Buffer
	if err := server.Serve(context.Background(), ioNopCloser{Reader: strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"stave.action.invoke\",\"params\":{\"callId\":\"inc\",\"actionId\":\"example.increment.v1\"}}\n")}, &invoked); err != nil {
		t.Fatal(err)
	}
	agentHash := waitCount(t, agentApp, 1)
	if humanHash != agentHash {
		t.Fatalf("tree hashes differ: human=%s agent=%s", humanHash, agentHash)
	}
}

func TestIncrementReducerIgnoresOtherActionsAndMetadataIsAccurate(t *testing.T) {
	app, err := New(context.Background(), AgentManifest())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	definition, ok := app.Registry.Definition(IncrementActionID)
	if !ok || definition.Idempotency != action.NonIdempotent {
		t.Fatalf("increment metadata = %#v", definition)
	}
	ev, err := event.New(event.ActionInvoked, event.ActionInvokedPayload{CallID: "other", ActionID: "example.other.v1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Prepared.Session.Send(ev); err != nil {
		t.Fatal(err)
	}
	if err := app.Prepared.Session.Wait(context.Background(), func(current state.State[Model]) bool { return current.Sequence == 1 }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := app.Prepared.Session.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Model.Count != 0 {
		t.Fatalf("unrelated action changed count: %d", snapshot.Model.Count)
	}
}

func assertAgentManifestAndSnapshot(t *testing.T, output string, app *Application) {
	t.Helper()
	results := map[int]json.RawMessage{}
	decoder := json.NewDecoder(strings.NewReader(output))
	for decoder.More() {
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
		}
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		results[response.ID] = response.Result
	}
	var initialized struct {
		ResolvedManifest capability.Manifest `json:"resolvedManifest"`
	}
	if err := json.Unmarshal(results[1], &initialized); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(initialized.ResolvedManifest, app.Prepared.Capabilities) {
		t.Fatalf("agent manifest differs from bound session: got=%#v want=%#v", initialized.ResolvedManifest, app.Prepared.Capabilities)
	}
	var snapshot struct {
		CapabilityHash string `json:"capabilityHash"`
		Mode           string `json:"mode"`
	}
	if err := json.Unmarshal(results[3], &snapshot); err != nil {
		t.Fatal(err)
	}
	current, err := app.Prepared.Session.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != "full" || snapshot.CapabilityHash != current.Hashes.Capability {
		t.Fatalf("agent snapshot does not use bound capabilities: %#v", snapshot)
	}
}

func TestApplicationCloseAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app, err := New(ctx, AgentManifest())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	app.Close()
}

type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }
