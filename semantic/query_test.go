package semantic

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTreeQueryComposesCriteriaInPreorder(t *testing.T) {
	tree := queryFixture(t)
	result, err := tree.Query(Query{
		Role:     "button",
		Action:   "save",
		Metadata: map[string]string{"group": "primary"},
	}, QueryLimits{MaxVisited: 10, MaxResults: 10, MaxDepth: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Truncated || result.Visited != 5 {
		t.Fatalf("result = %+v", result)
	}
	if got, want := queryIDs(result.Nodes), []NodeID{queryID(t, "save-left"), queryID(t, "save-deep")}; !sameNodeIDs(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}

	byID, err := tree.Query(Query{ID: queryID(t, "save-deep")}, QueryLimits{MaxVisited: 10, MaxResults: 10, MaxDepth: 10})
	if err != nil || len(byID.Nodes) != 1 || byID.Nodes[0].ID() != queryID(t, "save-deep") {
		t.Fatalf("stable ID query = %+v, %v", byID, err)
	}
}

func TestTreeQueryRejectsMalformedQueriesAndFindsNoMatch(t *testing.T) {
	tree := queryFixture(t)
	limits := QueryLimits{MaxVisited: 10, MaxResults: 10, MaxDepth: 10}
	for _, tc := range []struct {
		name   string
		query  Query
		limits QueryLimits
	}{
		{"empty", Query{}, limits},
		{"invalid ID", Query{ID: "not-an-id"}, limits},
		{"invalid role", Query{Role: "script"}, limits},
		{"invalid action", Query{Action: ActionID("bad\x00action")}, limits},
		{"invalid metadata", Query{Metadata: map[string]string{"bad\x00key": "value"}}, limits},
		{"zero visited", Query{Role: "button"}, QueryLimits{MaxResults: 1, MaxDepth: 1}},
		{"zero results", Query{Role: "button"}, QueryLimits{MaxVisited: 1, MaxDepth: 1}},
		{"negative depth", Query{Role: "button"}, QueryLimits{MaxVisited: 1, MaxResults: 1, MaxDepth: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tree.Query(tc.query, tc.limits); !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("Query() error = %v, want ErrInvalidQuery", err)
			}
		})
	}

	result, err := tree.Query(Query{Metadata: map[string]string{"group": "missing"}}, limits)
	if err != nil || result.Truncated || result.Visited != 5 || len(result.Nodes) != 0 {
		t.Fatalf("no match = %+v, %v", result, err)
	}
}

func TestTreeQueryReportsEachBudgetPrecisely(t *testing.T) {
	tree := queryFixture(t)
	tests := []struct {
		name        string
		limits      QueryLimits
		wantVisited int
		wantIDs     []NodeID
		wantLimit   QueryLimit
	}{
		{"results", QueryLimits{MaxVisited: 10, MaxResults: 1, MaxDepth: 10}, 3, []NodeID{queryID(t, "save-left")}, QueryLimitResults},
		{"visited", QueryLimits{MaxVisited: 2, MaxResults: 10, MaxDepth: 10}, 2, []NodeID{queryID(t, "save-left")}, QueryLimitVisited},
		{"depth", QueryLimits{MaxVisited: 10, MaxResults: 10, MaxDepth: 1}, 2, []NodeID{queryID(t, "save-left")}, QueryLimitDepth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tree.Query(Query{Action: "save"}, tc.limits)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Truncated || result.Limit != tc.wantLimit || result.Visited != tc.wantVisited {
				t.Fatalf("result = %+v", result)
			}
			if got := queryIDs(result.Nodes); !sameNodeIDs(got, tc.wantIDs) {
				t.Fatalf("IDs = %v, want %v", got, tc.wantIDs)
			}
		})
	}
}

func TestTreeQueryPreservesRedactionAndResultIsolation(t *testing.T) {
	tree := queryFixture(t)
	result, err := tree.Query(Query{Role: "textbox"}, QueryLimits{MaxVisited: 10, MaxResults: 10, MaxDepth: 10})
	if err != nil || len(result.Nodes) != 1 {
		t.Fatalf("Query() = %+v, %v", result, err)
	}
	encoded, err := json.Marshal(result.Nodes[0])
	if err != nil || strings.Contains(string(encoded), `"text":`) {
		t.Fatalf("encoded result = %s, %v", encoded, err)
	}
	metadata := result.Nodes[0].Metadata()
	metadata["group"] = "changed"
	result.Nodes[0] = Node{}

	again, err := tree.Query(Query{Metadata: map[string]string{"group": "private"}}, QueryLimits{MaxVisited: 10, MaxResults: 10, MaxDepth: 10})
	if err != nil || len(again.Nodes) != 1 || again.Nodes[0].ID() != queryID(t, "secret") {
		t.Fatalf("tree mutated through result = %+v, %v", again, err)
	}
}

func queryFixture(t *testing.T) Tree {
	t.Helper()
	deep := queryNode(t, "save-deep", "button", []ActionRef{{ID: "save"}}, map[string]string{"group": "primary"}, nil)
	left := queryNode(t, "save-left", "button", []ActionRef{{ID: "save"}}, map[string]string{"group": "primary"}, []Node{deep})
	secret := queryNode(t, "secret", "textbox", nil, map[string]string{"group": "private"}, nil)
	right := queryNode(t, "other", "button", []ActionRef{{ID: "cancel"}}, map[string]string{"group": "primary"}, nil)
	root := queryNode(t, "root", "group", nil, nil, []Node{left, secret, right})
	tree, err := NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func queryNode(t *testing.T, entity string, role Role, actions []ActionRef, metadata map[string]string, children []Node) Node {
	t.Helper()
	node, err := NewNode(NodeSpec{ID: queryID(t, entity), Role: role, Name: entity, Actions: actions, Metadata: metadata, Children: children, Value: secretValueFor(entity)})
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func secretValueFor(entity string) Value {
	if entity == "secret" {
		return SecretValue()
	}
	return Value{}
}

func queryID(t *testing.T, entity string) NodeID {
	t.Helper()
	id, err := NodeIDFor(NodeKey{"query", "fixture", "node", entity, "slot"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func queryIDs(nodes []Node) []NodeID {
	ids := make([]NodeID, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID()
	}
	return ids
}

func sameNodeIDs(got, want []NodeID) bool {
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
