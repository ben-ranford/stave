package conformance

import (
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/testfixture"
	"testing"
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
