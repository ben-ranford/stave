package focus

import (
	"testing"

	"github.com/ben-ranford/stave/semantic"
)

func TestGraphOrderSkipsDisabledHiddenAndOffscreen(t *testing.T) {
	tree := testTree(t,
		testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, nil),
		testNode(t, "one", "button", "One", semantic.Flags{Visible: true, Focusable: true}, nil),
		testNode(t, "two", "button", "Two", semantic.Flags{Visible: true, Focusable: true, Disabled: true}, nil),
		testNode(t, "three", "button", "Three", semantic.Flags{Visible: false, Focusable: true}, nil),
		testNode(t, "four", "button", "Four", semantic.Flags{Visible: true, Focusable: true, Offscreen: true}, nil),
		testNode(t, "five", "button", "Five", semantic.Flags{Visible: true, Focusable: true}, nil),
	)
	graph := NewGraph(tree)
	targets := graph.Focusable()
	if len(targets) != 2 {
		t.Fatalf("expected 2 focusable targets, got %d", len(targets))
	}
	if targets[0].NodeID == targets[1].NodeID {
		t.Fatal("focus order should preserve distinct targets")
	}
}

func TestGraphScopePushAndPopRestoresPreviousFocus(t *testing.T) {
	dialog := testNode(t, "dialog", "dialog", "Dialog", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "dialog.ok", "button", "OK", semantic.Flags{Visible: true, Focusable: true}, nil),
		testNode(t, "dialog.cancel", "button", "Cancel", semantic.Flags{Visible: true, Focusable: true}, nil),
	})
	tree := testTree(t,
		testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
			testNode(t, "primary", "button", "Primary", semantic.Flags{Visible: true, Focusable: true}, nil),
			dialog,
		}),
	)
	graph := NewGraph(tree)
	initial := State{Active: graph.Focusable()[0]}
	scoped, ok := graph.PushScope(initial, dialog.ID())
	if !ok {
		t.Fatal("expected scope push to succeed")
	}
	if scoped.Scope != dialog.ID() {
		t.Fatalf("unexpected scope: %s", scoped.Scope)
	}
	popped, ok := graph.PopScope(scoped)
	if !ok {
		t.Fatal("expected scope pop to succeed")
	}
	if popped.Active.NodeID != initial.Active.NodeID {
		t.Fatalf("expected focus to restore to %s, got %s", initial.Active.NodeID, popped.Active.NodeID)
	}
}

func TestGraphRepairFromChoosesNearestSurvivingNeighbor(t *testing.T) {
	before := testTree(t,
		testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
			testNode(t, "a", "button", "A", semantic.Flags{Visible: true, Focusable: true}, nil),
			testNode(t, "b", "button", "B", semantic.Flags{Visible: true, Focusable: true}, nil),
			testNode(t, "c", "button", "C", semantic.Flags{Visible: true, Focusable: true}, nil),
		}),
	)
	after := testTree(t,
		testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
			testNode(t, "a", "button", "A", semantic.Flags{Visible: true, Focusable: true}, nil),
			testNode(t, "c", "button", "C", semantic.Flags{Visible: true, Focusable: true}, nil),
		}),
	)
	previous := NewGraph(before)
	current := NewGraph(after)
	state := State{Active: previous.Focusable()[1]}
	repaired := current.RepairFrom(previous, state)
	if repaired.Active.NodeID != current.Focusable()[1].NodeID {
		t.Fatalf("expected focus to move to nearest surviving node, got %s", repaired.Active.NodeID)
	}
}

func TestGraphValidateRejectsOutOfScopeActiveTarget(t *testing.T) {
	tree := testTree(t,
		testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
			testNode(t, "a", "button", "A", semantic.Flags{Visible: true, Focusable: true}, nil),
			testNode(t, "scope", "dialog", "Scope", semantic.Flags{Visible: true}, []semantic.Node{
				testNode(t, "b", "button", "B", semantic.Flags{Visible: true, Focusable: true}, nil),
			}),
		}),
	)
	graph := NewGraph(tree)
	state := State{Active: graph.Focusable()[0], Scope: mustID(t, "scope")}
	if err := graph.Validate(state); err == nil {
		t.Fatal("expected validation error for out-of-scope target")
	}
}

func testTree(t *testing.T, nodes ...semantic.Node) semantic.Tree {
	t.Helper()
	if len(nodes) == 0 {
		t.Fatal("test tree requires a root")
	}
	root := nodes[0]
	if len(nodes) > 1 {
		var err error
		root, err = root.WithChildren(nodes[1:]...)
		if err != nil {
			t.Fatalf("children: %v", err)
		}
	}
	tree, err := semantic.NewTree(7, root)
	if err != nil {
		t.Fatalf("new tree: %v", err)
	}
	return tree
}

func testNode(t *testing.T, entity, role, name string, flags semantic.Flags, children []semantic.Node) semantic.Node {
	t.Helper()
	node, err := semantic.NewNode(semantic.NodeSpec{
		Key: &semantic.NodeKey{
			AppNamespace: "stave.test",
			View:         "runtime",
			Kind:         string(role),
			Entity:       entity,
			Slot:         "default",
		},
		Role:     semantic.Role(role),
		Name:     name,
		Children: children,
		Flags:    flags,
	})
	if err != nil {
		t.Fatalf("new node %s: %v", entity, err)
	}
	return node
}

func mustID(t *testing.T, entity string) semantic.NodeID {
	t.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{
		AppNamespace: "stave.test",
		View:         "runtime",
		Kind:         "dialog",
		Entity:       entity,
		Slot:         "default",
	})
	if err != nil {
		t.Fatalf("node id: %v", err)
	}
	return id
}

func TestModalLifecycleRestoresAndIsIdempotent(t *testing.T) {
	dialog := testNode(t, "dialog", "dialog", "Dialog", semantic.Flags{Visible: true}, []semantic.Node{testNode(t, "ok", "button", "OK", semantic.Flags{Visible: true, Focusable: true}, nil)})
	tree := testTree(t, testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{testNode(t, "open", "button", "Open", semantic.Flags{Visible: true, Focusable: true}, nil), dialog}))
	g := NewGraph(tree)
	m := NewModalLifecycle(State{Active: g.Focusable()[0]})
	if !m.Open(g, dialog.ID()) || m.State.Scope != dialog.ID() {
		t.Fatal("open failed")
	}
	if !m.Close(g, g, dialog.ID()) || m.State.Active.NodeID != g.Focusable()[0].NodeID {
		t.Fatal("restore failed")
	}
	if m.Close(g, g, dialog.ID()) {
		t.Fatal("close was not idempotent")
	}
}
