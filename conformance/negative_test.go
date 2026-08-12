package conformance

import (
	"github.com/ben-ranford/stave/primitive"
	"testing"
)

func TestDisabledControlsRemainVisible(t *testing.T) {
	n, err := primitive.Button(primitive.Options{Namespace: "x", View: "v", Entity: "b", Name: "Disabled", Disabled: true}, "Disabled")
	if err != nil {
		t.Fatal(err)
	}
	if !n.Flags().Visible || !n.Flags().Disabled {
		t.Fatalf("flags=%+v", n.Flags())
	}
}

func TestPrimitiveRejectsInvalidBoundsAndAmbiguousTabs(t *testing.T) {
	if _, err := primitive.Table(primitive.TableOptions{Options: primitive.Options{Namespace: "x", View: "v", Entity: "t", Name: "T"}, Offset: -1}); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err := primitive.Viewport(primitive.ViewportOptions{Options: primitive.Options{Namespace: "x", View: "v", Entity: "vp", Name: "VP"}, Width: -1}); err == nil {
		t.Fatal("negative viewport accepted")
	}
	if _, err := primitive.Progress(primitive.Options{Namespace: "x", View: "v", Entity: "p", Name: "P"}, 4, 3, false); err == nil {
		t.Fatal("over-complete progress accepted")
	}
	if _, err := primitive.Tabs(primitive.Options{Namespace: "x", View: "v", Entity: "tabs", Name: "Tabs"}, primitive.Tab{Key: "a", Name: "A", Selected: true}, primitive.Tab{Key: "b", Name: "B", Selected: true}); err == nil {
		t.Fatal("ambiguous tabs accepted")
	}
}
func FuzzPrimitiveTextControlBytes(f *testing.F) {
	f.Add("hello")
	f.Fuzz(func(t *testing.T, value string) {
		_, _ = primitive.Text(primitive.Options{Namespace: "fuzz", View: "v", Entity: "text", Name: "Text"}, value)
	})
}
