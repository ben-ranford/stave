package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/protocol"
)

func TestBindSessionUsesFinalizedActionRegistry(t *testing.T) {
	oldRegistry, newRegistry := action.NewRegistry(), action.NewRegistry()
	handler := func(context.Context, action.Call, any) (any, error) { return nil, nil }
	registerTestAction(t, oldRegistry, simpleActionDefinition("old", action.ReadOnly), handler)
	registerTestAction(t, newRegistry, simpleActionDefinition("new", action.ReadOnly), handler)
	for _, test := range []struct {
		name           string
		initial, final *action.Registry
	}{
		{"assigned", nil, newRegistry},
		{"replaced", oldRegistry, newRegistry},
		{"removed", oldRegistry, nil},
	} {
		t.Run(test.name, func(t *testing.T) { checkBoundActionRegistry(t, test.initial, test.final) })
	}
}

func checkBoundActionRegistry(t *testing.T, initial, final *action.Registry) {
	t.Helper()
	s := bridgeSession(t, "registry")
	defer s.Close()
	bound, err := BindSession(s, Options{CompatibilityMode: true, Actions: initial})
	if err != nil {
		t.Fatal(err)
	}
	bound.Actions = final
	server := New(bound)
	defer server.Close()
	request := func(method, params string) protocol.Response {
		return server.handle(context.Background(), protocol.Request{JSONRPC: protocol.JSONRPC, ID: json.RawMessage("1"), Method: method, Params: json.RawMessage(params)})
	}
	for _, method := range []string{"stave.initialize", "stave.initialized"} {
		if response := request(method, ""); response.Error != nil {
			t.Fatal(response.Error)
		}
	}
	listed := request("stave.actions.list", "")
	if listed.Error != nil {
		t.Fatal(listed.Error)
	}
	snapshot := request("stave.snapshot", `{"mode":"full","includeActions":true}`)
	if snapshot.Error != nil {
		t.Fatal(snapshot.Error)
	}
	want := listed.Result.([]action.Definition)
	if len(want) == 0 {
		want = nil
	}
	got := snapshot.Result.(protocol.SnapshotResult).Actions
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot actions differ from finalized registry: got %#v, want %#v", got, want)
	}
}
