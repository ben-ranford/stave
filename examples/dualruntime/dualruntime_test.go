package dualruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/runtime/agent"
	"github.com/ben-ranford/stave/runtime/human"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/session"
	"github.com/ben-ranford/stave/state"
)

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
	humanSnapshot, err := humanApp.Prepared.Session.Snapshot()
	if err != nil || humanSnapshot.Model.Count != 1 {
		t.Fatalf("human handler returned before increment publication: snapshot=%+v err=%v", humanSnapshot, err)
	}
	humanHash := humanSnapshot.Tree.Hash()

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
	assertAgentActionResponse(t, invoked.String())
	agentSnapshot, err := agentApp.Prepared.Session.Snapshot()
	if err != nil || agentSnapshot.Model.Count != 1 {
		t.Fatalf("agent response returned before increment publication: snapshot=%+v err=%v", agentSnapshot, err)
	}
	agentHash := agentSnapshot.Tree.Hash()
	if humanHash != agentHash {
		t.Fatalf("tree hashes differ: human=%s agent=%s", humanHash, agentHash)
	}
}

func TestIncrementAcknowledgementWaitsForReusedCallIDMutation(t *testing.T) {
	app, err := New(context.Background(), AgentManifest())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	call := action.Call{CallID: "reused", ActionID: IncrementActionID, Arguments: json.RawMessage(`{}`), SessionID: "dual-runtime"}
	for want := 1; want <= 2; want++ {
		if result := app.invoke(context.Background(), call); result.Error != nil {
			t.Fatalf("increment %d failed: %+v", want, result)
		}
		snapshot, err := app.Prepared.Session.Snapshot()
		if err != nil || snapshot.Model.Count != want {
			t.Fatalf("increment %d returned before visible mutation: snapshot=%+v err=%v", want, snapshot, err)
		}
	}
}

func TestConcurrentIncrementAcknowledgementsAreSerialized(t *testing.T) {
	app, err := New(context.Background(), AgentManifest())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	start := make(chan struct{})
	results := make(chan action.Result, 2)
	var wg sync.WaitGroup
	for _, callID := range []string{"first", "second"} {
		wg.Add(1)
		go func(callID string) {
			defer wg.Done()
			<-start
			results <- app.invoke(context.Background(), action.Call{CallID: callID, ActionID: IncrementActionID, Arguments: json.RawMessage(`{}`), SessionID: "dual-runtime"})
		}(callID)
	}
	close(start)
	wg.Wait()
	close(results)
	for result := range results {
		if result.Error != nil {
			t.Fatalf("concurrent increment failed: %+v", result)
		}
	}
	snapshot, err := app.Prepared.Session.Snapshot()
	if err != nil || snapshot.Model.Count != 2 {
		t.Fatalf("concurrent increments were not both visible: snapshot=%+v err=%v", snapshot, err)
	}
}

func TestCancelledIncrementKeepsItsPublicationTicketForNextInvocation(t *testing.T) {
	app, err := New(context.Background(), AgentManifest())
	if err != nil {
		t.Fatal(err)
	}
	app.Prepared.Session.Close()
	firstEntered, releaseFirst := make(chan struct{}), make(chan struct{})
	secondEntered, releaseSecond := make(chan struct{}), make(chan struct{})
	secondObserved := make(chan struct{})
	var observeSecond atomic.Bool
	var observedOnce sync.Once
	blocked, err := session.New(context.Background(), session.Options[Model]{
		ModelPolicy: state.ModelPolicy[Model]{Clone: func(model Model) (Model, error) {
			if observeSecond.Load() {
				observedOnce.Do(func() { close(secondObserved) })
			}
			return model, nil
		}},
		Reduce: func(_ context.Context, current Model, ev event.Event) (Model, []effect.Request, error) {
			if payload, ok := ev.Payload.(event.ActionInvokedPayload); ok && payload.ActionID == string(IncrementActionID) {
				switch payload.CallID {
				case "first":
					close(firstEntered)
					<-releaseFirst
				case "second":
					close(secondEntered)
					<-releaseSecond
				}
				current.Count++
			}
			return current, nil, nil
		},
		View: func(context.Context, Model) (session.ViewResult, error) {
			node, err := primitive.Text(primitive.Options{Namespace: "test", View: "counter", Entity: "count", Name: "Count"}, "count")
			if err != nil {
				return session.ViewResult{}, err
			}
			tree, err := semantic.NewTree(1, node)
			return session.ViewResult{Tree: tree}, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.Prepared.Session = blocked
	defer app.Close()
	var releaseFirstOnce, releaseSecondOnce sync.Once
	releaseBlockedReducers := func() {
		releaseFirstOnce.Do(func() { close(releaseFirst) })
		releaseSecondOnce.Do(func() { close(releaseSecond) })
	}
	defer releaseBlockedReducers()

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	first := make(chan action.Result, 1)
	go func() {
		first <- app.invoke(firstCtx, action.Call{CallID: "first", ActionID: IncrementActionID, Arguments: json.RawMessage(`{}`), SessionID: "dual-runtime"})
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first increment reducer did not block")
	}
	cancelFirst()
	select {
	case result := <-first:
		if result.Error == nil {
			t.Fatal("cancelled first increment succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled first increment did not return")
	}
	second := make(chan action.Result, 1)
	observeSecond.Store(true)
	go func() {
		second <- app.invoke(context.Background(), action.Call{CallID: "second", ActionID: IncrementActionID, Arguments: json.RawMessage(`{}`), SessionID: "dual-runtime"})
	}()
	select {
	case <-secondObserved:
	case <-time.After(time.Second):
		t.Fatal("second increment did not observe the state before first publication")
	}
	releaseFirstOnce.Do(func() { close(releaseFirst) })
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second increment was not queued behind the cancelled first increment")
	}
	select {
	case result := <-second:
		t.Fatalf("second increment acknowledged after the first publication: %+v", result)
	case <-time.After(25 * time.Millisecond):
	}
	releaseSecondOnce.Do(func() { close(releaseSecond) })
	select {
	case result := <-second:
		if result.Error != nil {
			t.Fatalf("second increment failed: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("second increment did not wait for its publication")
	}
	snapshot, err := blocked.Snapshot()
	if err != nil || snapshot.Model.Count != 2 {
		t.Fatalf("second increment returned before both mutations: snapshot=%+v err=%v", snapshot, err)
	}
}

func assertAgentActionResponse(t *testing.T, output string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	for decoder.More() {
		var response struct {
			ID    int             `json:"id"`
			Error json.RawMessage `json:"error"`
		}
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.ID == 4 {
			if len(response.Error) > 0 && string(response.Error) != "null" {
				t.Fatalf("increment response failed: %s", output)
			}
			return
		}
	}
	t.Fatalf("increment response missing: %s", output)
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
