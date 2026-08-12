package primitive_test

import (
	"testing"

	"github.com/ben-ranford/stave/conformance"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func TestHiddenAndDisabledPrimitivesCannotRemainFocusable(t *testing.T) {
	hidden, err := primitive.Button(primitive.Options{Namespace: "test", View: "v", Entity: "hidden", Name: "Hidden", Hidden: true}, "Hidden")
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Flags().Focusable {
		t.Fatalf("hidden flags=%+v", hidden.Flags())
	}
	disabled, err := primitive.Button(primitive.Options{Namespace: "test", View: "v", Entity: "disabled", Name: "Disabled", Disabled: true}, "Disabled")
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Flags().Focusable {
		t.Fatalf("disabled flags=%+v", disabled.Flags())
	}
}

func TestFieldPrimitivesExposeErrorRelations(t *testing.T) {
	inputField, err := primitive.Input(primitive.InputOptions{
		Options:      primitive.Options{Namespace: "test", View: "v", Entity: "input", Name: "Input"},
		Invalid:      true,
		ErrorMessage: "Required",
	})
	if err != nil {
		t.Fatal(err)
	}
	numberField, err := primitive.NumberInput(primitive.NumberOptions{
		InputOptions: primitive.InputOptions{
			Options:      primitive.Options{Namespace: "test", View: "v", Entity: "number", Name: "Number"},
			Invalid:      true,
			ErrorMessage: "Out of range",
		},
		Min: "1", Max: "9", Step: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkboxField, err := primitive.Checkbox(primitive.CheckboxOptions{
		Options:      primitive.Options{Namespace: "test", View: "v", Entity: "checkbox", Name: "Checkbox"},
		Invalid:      true,
		ErrorMessage: "Required",
	}, "Checkbox")
	if err != nil {
		t.Fatal(err)
	}
	selectField, err := primitive.Select(primitive.SelectOptions{
		Options:      primitive.Options{Namespace: "test", View: "v", Entity: "select", Name: "Select"},
		Invalid:      true,
		ErrorMessage: "Choose one",
		Choices: []primitive.Choice{
			{Key: "a", Name: "A"},
			{Key: "b", Name: "B", Selected: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	radioField, err := primitive.RadioGroup(primitive.RadioGroupOptions{
		Options:      primitive.Options{Namespace: "test", View: "v", Entity: "radio", Name: "Radio"},
		Invalid:      true,
		ErrorMessage: "Pick one",
		Choices: []primitive.Choice{
			{Key: "a", Name: "A"},
			{Key: "b", Name: "B", Selected: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := []semantic.Node{inputField, numberField, checkboxField, selectField, radioField}
	root, err := primitive.Stack(primitive.Options{Namespace: "test", View: "v", Entity: "root", Name: "Root"}, fields...)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Validate(); err != nil {
		t.Fatalf("tree validate: %v", err)
	}
	if failures := conformance.ValidateTree(root); len(failures) != 0 {
		t.Fatalf("failures=%v", failures)
	}
	if fields[1].Metadata()["min"] != "1" || fields[1].Metadata()["max"] != "9" || fields[1].Metadata()["step"] != "1" {
		t.Fatalf("number metadata=%v", fields[1].Metadata())
	}
}

func TestTableModalAndMasterDetailContracts(t *testing.T) {
	table, err := primitive.Table(primitive.TableOptions{
		Options: primitive.Options{Namespace: "test", View: "v", Entity: "table", Name: "Table"},
		Columns: []primitive.Column{{Key: "name", Name: "Name"}, {Key: "count", Name: "Count", Numeric: true}},
		Rows:    []primitive.TableRow{{Key: "a", Name: "A", Cells: []string{"A", "2"}}},
		SortKey: "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	header := table.Children()[0].Children()[1]
	if !hasAction(header.Actions(), "stave.primitive.core.sort_column.v1") {
		t.Fatalf("header actions=%v", header.Actions())
	}
	cell := table.Children()[1].Children()[0].Children()[0]
	if len(cell.Relations()) == 0 || cell.Relations()[0].Kind != "labelled-by" {
		t.Fatalf("cell relations=%v", cell.Relations())
	}

	content, err := primitive.Text(primitive.Options{Namespace: "test", View: "v", Entity: "content", Name: "Content"}, "Body")
	if err != nil {
		t.Fatal(err)
	}
	modal, err := primitive.Modal(primitive.Options{Namespace: "test", View: "v", Entity: "modal", Name: "Modal"}, content)
	if err != nil {
		t.Fatal(err)
	}
	if modal.Metadata()["focusRestore"] == "" || !hasAction(modal.Actions(), "stave.primitive.core.cancel.v1") {
		t.Fatalf("modal metadata=%v actions=%v", modal.Metadata(), modal.Actions())
	}

	master, err := primitive.List(primitive.Options{Namespace: "test", View: "v", Entity: "master", Name: "Master"})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := primitive.Section(primitive.Options{Namespace: "test", View: "v", Entity: "detail", Name: "Detail"})
	if err != nil {
		t.Fatal(err)
	}
	masterDetail, err := primitive.MasterDetail(primitive.Options{Namespace: "test", View: "v", Entity: "md", Name: "Master detail"}, master, detail)
	if err != nil {
		t.Fatal(err)
	}
	if masterDetail.Metadata()["recordFallback"] != "disclosure" || masterDetail.Metadata()["focusRestore"] == "" {
		t.Fatalf("master-detail metadata=%v", masterDetail.Metadata())
	}
	if !hasAction(masterDetail.Actions(), "stave.primitive.core.open_detail.v1") || !hasAction(masterDetail.Actions(), "stave.primitive.core.back_to_master.v1") {
		t.Fatalf("master-detail actions=%v", masterDetail.Actions())
	}
}

func TestTableRejectsInvalidWindowAndSelectionContracts(t *testing.T) {
	base := primitive.TableOptions{Options: primitive.Options{Namespace: "t", View: "v", Entity: "table", Name: "Table"}, Columns: []primitive.Column{{Key: "name", Name: "Name"}}, Rows: []primitive.TableRow{{Key: "a", Cells: []string{"A"}}}, Total: 1}
	for name, mutate := range map[string]func(*primitive.TableOptions){
		"duplicate rows": func(o *primitive.TableOptions) {
			o.Rows = append(o.Rows, primitive.TableRow{Key: "a", Cells: []string{"A"}})
		},
		"unknown selected": func(o *primitive.TableOptions) { o.SelectedKey = "missing" },
		"unknown sort":     func(o *primitive.TableOptions) { o.SortKey = "missing" },
		"multiple single": func(o *primitive.TableOptions) {
			o.Rows[0].Selected = true
			o.Rows = append(o.Rows, primitive.TableRow{Key: "b", Selected: true, Cells: []string{"B"}})
			o.Total = 2
		},
		"bad window":       func(o *primitive.TableOptions) { o.Offset = 2; o.Limit = 1; o.Total = 2 },
		"bad width policy": func(o *primitive.TableOptions) { o.Columns[0].WidthPolicy = "invalid" },
	} {
		o := base
		o.Columns = append([]primitive.Column(nil), base.Columns...)
		o.Rows = append([]primitive.TableRow(nil), base.Rows...)
		mutate(&o)
		if _, err := primitive.Table(o); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestTableExplicitStates(t *testing.T) {
	for _, state := range []string{"empty", "loading"} {
		n, err := primitive.Table(primitive.TableOptions{Options: primitive.Options{Namespace: "t", View: "v", Entity: state, Name: "Table"}, Columns: []primitive.Column{{Key: "name", Name: "Name"}}, State: state})
		if err != nil {
			t.Fatal(err)
		}
		if n.Metadata()["state"] != state {
			t.Fatalf("state metadata=%v", n.Metadata())
		}
	}
	if _, err := primitive.Table(primitive.TableOptions{Options: primitive.Options{Namespace: "t", View: "v", Entity: "error", Name: "Table"}, State: "error"}); err == nil {
		t.Fatal("error state without message accepted")
	}
}

func hasAction(actions []semantic.ActionRef, id string) bool {
	for _, actionRef := range actions {
		if string(actionRef.ID) == id {
			return true
		}
	}
	return false
}
