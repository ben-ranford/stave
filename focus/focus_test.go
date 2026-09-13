package focus

import (
	"reflect"
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

func TestModalLifecycleRestoresOuterScopeAfterNestedClose(t *testing.T) {
	inner := testNode(t, "inner", "dialog", "Inner", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "inner.ok", "button", "OK", semantic.Flags{Visible: true, Focusable: true}, nil),
	})
	outer := testNode(t, "outer", "dialog", "Outer", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "outer.cancel", "button", "Cancel", semantic.Flags{Visible: true, Focusable: true}, nil), inner,
	})
	tree := testTree(t, testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "open", "button", "Open", semantic.Flags{Visible: true, Focusable: true}, nil), outer,
	}))
	g := NewGraph(tree)
	m := NewModalLifecycle(State{Active: g.Focusable()[0]})
	if !m.Open(g, outer.ID()) || !m.Open(g, inner.ID()) {
		t.Fatal("nested open failed")
	}
	if !m.Close(g, g, inner.ID()) {
		t.Fatal("nested close failed")
	}
	if m.State.Scope != outer.ID() {
		t.Fatalf("outer scope was not restored: %s", m.State.Scope)
	}
	if m.State.Active.NodeID != g.Focusable()[1].NodeID {
		t.Fatalf("outer focus was not restored: %s", m.State.Active.NodeID)
	}
	if err := g.Validate(m.State); err != nil {
		t.Fatalf("restored nested state is invalid: %v", err)
	}
	if !m.Close(g, g, outer.ID()) || m.State.Scope != "" || m.State.Active.NodeID != g.Focusable()[0].NodeID {
		t.Fatalf("outer close state=%+v", m.State)
	}
}

func TestModalLifecycleRestoresPreExistingScope(t *testing.T) {
	inner := testNode(t, "inner", "dialog", "Inner", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "inner.ok", "button", "OK", semantic.Flags{Visible: true, Focusable: true}, nil),
	})
	opener := testNode(t, "outer.open", "button", "Open", semantic.Flags{Visible: true, Focusable: true}, nil)
	outer := testNode(t, "outer", "dialog", "Outer", semantic.Flags{Visible: true}, []semantic.Node{opener, inner})
	background := testNode(t, "background", "button", "Background", semantic.Flags{Visible: true, Focusable: true}, nil)
	g := NewGraph(testTree(t, testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{background, outer})))
	active, ok := g.First(outer.ID())
	if !ok {
		t.Fatal("outer scope has no focusable target")
	}
	initial := State{Active: active, Scope: outer.ID(), Stack: []semantic.Target{g.Focusable()[0]}}
	for _, construct := range []struct {
		name string
		make func(State) ModalLifecycle
	}{
		{"constructor", NewModalLifecycle},
		{"state literal", func(s State) ModalLifecycle { return ModalLifecycle{State: s} }},
	} {
		t.Run(construct.name, func(t *testing.T) {
			m := construct.make(initial)
			if !m.Open(g, inner.ID()) || !m.Close(g, g, inner.ID()) {
				t.Fatal("inner modal lifecycle failed")
			}
			if m.State.Scope != outer.ID() || m.State.Active != active || len(m.State.Stack) != len(initial.Stack) {
				t.Fatalf("enclosing scope or initiating focus lost: got %+v, want %+v", m.State, initial)
			}
			for range len(g.Focusable()) + 1 {
				next, ok := g.Next(m.State)
				if !ok || next.Active.NodeID == background.ID() {
					t.Fatal("focus escaped enclosing scope after modal close")
				}
				m.State = next
			}
		})
	}
}

func TestModalLifecycleRepairsDeletedReturnTarget(t *testing.T) {
	dialog := testNode(t, "dialog", "dialog", "Dialog", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "dialog.ok", "button", "OK", semantic.Flags{Visible: true, Focusable: true}, nil),
	})
	before := NewGraph(testTree(t, testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "a", "button", "A", semantic.Flags{Visible: true, Focusable: true}, nil),
		testNode(t, "b", "button", "B", semantic.Flags{Visible: true, Focusable: true}, nil),
		dialog,
		testNode(t, "c", "button", "C", semantic.Flags{Visible: true, Focusable: true}, nil),
	})))
	m := NewModalLifecycle(State{Active: before.Focusable()[3]})
	if !m.Open(before, dialog.ID()) {
		t.Fatal("open failed")
	}
	current := NewGraph(testTree(t, testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "a", "button", "A", semantic.Flags{Visible: true, Focusable: true}, nil),
		testNode(t, "b", "button", "B", semantic.Flags{Visible: true, Focusable: true}, nil),
	})))
	if !m.Close(before, current, dialog.ID()) {
		t.Fatal("close failed")
	}
	if m.State.Active.NodeID != current.Focusable()[1].NodeID {
		t.Fatalf("deleted opener repaired to %s, want %s", m.State.Active.NodeID, current.Focusable()[1].NodeID)
	}
	if err := current.Validate(m.State); err != nil {
		t.Fatalf("repaired state is invalid: %v", err)
	}
}

func TestModalLifecycleRejectsInvalidScopeWithoutChangingState(t *testing.T) {
	g := NewGraph(testTree(t, testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "open", "button", "Open", semantic.Flags{Visible: true, Focusable: true}, nil),
	})))
	initial := State{Active: g.Focusable()[0]}
	m := NewModalLifecycle(initial)
	if m.Open(g, semantic.NodeID("missing")) {
		t.Fatal("invalid scope opened")
	}
	if !reflect.DeepEqual(m.State, initial) {
		t.Fatalf("invalid scope changed state: %+v", m.State)
	}
}

func TestModalLifecycleReopensWithoutLeakingScopeStack(t *testing.T) {
	dialog := testNode(t, "dialog", "dialog", "Dialog", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "dialog.ok", "button", "OK", semantic.Flags{Visible: true, Focusable: true}, nil),
	})
	g := NewGraph(testTree(t, testNode(t, "app", "application", "App", semantic.Flags{Visible: true}, []semantic.Node{
		testNode(t, "open", "button", "Open", semantic.Flags{Visible: true, Focusable: true}, nil), dialog,
	})))
	m := NewModalLifecycle(State{Active: g.Focusable()[0]})
	for attempt := 0; attempt < 2; attempt++ {
		if !m.Open(g, dialog.ID()) || !m.Close(g, g, dialog.ID()) {
			t.Fatalf("open/close attempt %d failed", attempt)
		}
		if m.State.Scope != "" || len(m.State.Stack) != 0 || m.State.Active.NodeID != g.Focusable()[0].NodeID {
			t.Fatalf("open/close attempt %d leaked state: %+v", attempt, m.State)
		}
	}
	if m.Close(g, g, dialog.ID()) {
		t.Fatal("repeated close succeeded")
	}
}
