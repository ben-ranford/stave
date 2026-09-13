package conformance

import (
	"testing"

	"github.com/ben-ranford/stave/focus"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/testfixture"
)

func TestTwoBrandsShareSemanticContract(t *testing.T) {
	for _, name := range []string{"brand-a", "brand-b"} {
		tree, err := testfixture.BrandTree(name, "ready")
		if err != nil {
			t.Fatal(err)
		}
		if got := ValidateTree(tree); len(got) != 0 {
			t.Fatalf("%s: %v", name, got)
		}
	}
}
func TestTableContractAndStableRowIDs(t *testing.T) {
	a := testfixture.Must(testfixture.TableTree("a"))
	b := testfixture.Must(primitive.Table(primitive.TableOptions{Options: primitive.Options{Namespace: "a", View: "summary", Entity: "dependencies", Name: "Dependencies"}, Columns: []primitive.Column{{Key: "name", Name: "Name"}, {Key: "count", Name: "Count", Numeric: true, Sticky: true}}, Rows: []primitive.TableRow{{Key: "b", Name: "B", Cells: []string{"B", "10"}}, {Key: "a", Name: "A", Cells: []string{"A", "2"}}}, Total: 2}))
	if f := ValidateTree(a); len(f) != 0 {
		t.Fatal(f)
	}
	if f := ValidateTree(b); len(f) != 0 {
		t.Fatal(f)
	}
	if a.Children()[1].Children()[0].ID() != b.Children()[1].Children()[1].ID() {
		t.Fatal("row IDs changed with reorder")
	}
}

func TestInteractivePrimitiveActionsHaveKeyboardBindings(t *testing.T) {
	button := testfixture.Must(primitive.Button(primitive.Options{Namespace: "a", View: "v", Entity: "button", Name: "Run"}, "Run"))
	input := testfixture.Must(primitive.Input(primitive.InputOptions{Options: primitive.Options{Namespace: "a", View: "v", Entity: "input", Name: "Query"}}))
	root := testfixture.Must(primitive.Stack(primitive.Options{Namespace: "a", View: "v", Entity: "root", Name: "Root"}, button, input))
	manifest := ActionManifest{}
	for _, n := range []semantic.Node{button, input} {
		for _, a := range n.Actions() {
			manifest[a.ID] = "key"
		}
	}
	if failures := ValidateTreeWithManifest(root, manifest); len(failures) != 0 {
		t.Fatal(failures)
	}
}

func TestModalConformanceFixtureTrapsBackgroundAndRestoresOnCancel(t *testing.T) {
	background := testfixture.Must(primitive.Button(primitive.Options{Namespace: "a", View: "v", Entity: "background", Name: "Background"}, "Background"))
	content := testfixture.Must(primitive.Button(primitive.Options{Namespace: "a", View: "v", Entity: "modal-content", Name: "Modal content"}, "Modal content"))
	modal := testfixture.Must(primitive.Modal(primitive.Options{Namespace: "a", View: "v", Entity: "modal", Name: "Modal"}, content))
	root := testfixture.Must(primitive.Stack(primitive.Options{Namespace: "a", View: "v", Entity: "root", Name: "Root"}, background, modal))
	if failures := ValidateTree(root); len(failures) != 0 {
		t.Fatal(failures)
	}
	if !hasActionID(modal, semantic.ActionID("stave.primitive.core.cancel.v1")) {
		t.Fatal("modal lacks cancel action")
	}

	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	g := focus.NewGraph(tree)
	initial, ok := g.First("")
	if !ok {
		t.Fatal("background is not focusable")
	}
	m := focus.NewModalLifecycle(focus.State{Active: initial})
	if !m.Open(g, modal.ID()) {
		t.Fatal("modal scope did not open")
	}
	for range len(g.Focusable()) + 1 {
		next, ok := g.Next(m.State)
		if !ok || next.Active.NodeID == background.ID() {
			t.Fatal("modal scope did not keep background inert")
		}
		m.State = next
	}
	if !m.Close(g, g, modal.ID()) {
		t.Fatal("cancel did not close modal scope")
	}
	if m.State.Scope != "" || m.State.Active.NodeID != background.ID() {
		t.Fatalf("cancel did not restore opener: %+v", m.State)
	}
}
