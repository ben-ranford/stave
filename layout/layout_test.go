package layout

import (
	"context"
	"fmt"
	"testing"

	"github.com/ben-ranford/stave/semantic"
)

func node(tb testing.TB, name string, meta map[string]string, flags semantic.Flags, cs ...semantic.Node) semantic.Node {
	tb.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "a", View: "v", Kind: "text", Entity: name, Slot: "s"})
	if err != nil {
		tb.Fatal(err)
	}
	n, err := semantic.NewNode(semantic.NodeSpec{
		ID:       id,
		Role:     "text",
		Name:     name,
		Metadata: meta,
		Children: cs,
		Flags:    flags,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return n
}

func visibleNode(tb testing.TB, name string, meta map[string]string, cs ...semantic.Node) semantic.Node {
	tb.Helper()
	return node(tb, name, meta, semantic.Flags{Visible: true}, cs...)
}

func TestArrangeSupportsStackRowSplitGridScrollOverlayInsetConditionalAndRecords(t *testing.T) {
	t.Parallel()
	overlay := visibleNode(t, "overlay", map[string]string{"layout.kind": "overlay"},
		visibleNode(t, "base", map[string]string{"layout.z": "0"}),
		visibleNode(t, "dialog", map[string]string{"layout.z": "10"}),
	)
	conditional := visibleNode(t, "conditional", map[string]string{"layout.kind": "conditional", "layout.when": "b"},
		visibleNode(t, "case-a", map[string]string{"layout.case": "a"}),
		visibleNode(t, "case-b", map[string]string{"layout.case": "b"}),
	)
	root := visibleNode(t, "root", map[string]string{"layout.kind": "stack", "layout.gap": "1"},
		visibleNode(t, "row", map[string]string{"layout.kind": "row", "layout.gap": "1"}, visibleNode(t, "x", nil), visibleNode(t, "y", nil)),
		visibleNode(t, "split", map[string]string{"layout.kind": "split", "layout.direction": "horizontal", "layout.gap": "1"},
			visibleNode(t, "left", map[string]string{"layout.flex": "1", "layout.minWidth": "4"}),
			visibleNode(t, "right", map[string]string{"layout.flex": "2", "layout.minWidth": "4"}),
		),
		visibleNode(t, "grid", map[string]string{"layout.kind": "grid", "layout.columns": "4,1fr", "layout.gap": "1"},
			visibleNode(t, "g1", nil), visibleNode(t, "g2", nil), visibleNode(t, "g3", nil), visibleNode(t, "g4", nil),
		),
		visibleNode(t, "scroll", map[string]string{"layout.kind": "scroll", "layout.scrollY": "2"},
			visibleNode(t, "content", map[string]string{"layout.kind": "records"},
				visibleNode(t, "a", nil), visibleNode(t, "b", nil), visibleNode(t, "c", nil), visibleNode(t, "d", nil),
			),
		),
		overlay,
		visibleNode(t, "inset", map[string]string{"layout.kind": "inset", "layout.inset": "1,2"}, visibleNode(t, "inside", nil)),
		conditional,
		visibleNode(t, "records", map[string]string{"layout.kind": "records"}, visibleNode(t, "r1", nil), visibleNode(t, "r2", nil)),
	)
	plan, err := Arrange(context.Background(), root, Size{Width: 28, Height: 24}, 128)
	if err != nil {
		t.Fatalf("Arrange failed: %v", err)
	}
	if len(plan.Boxes) < 20 {
		t.Fatalf("unexpected box count: %d", len(plan.Boxes))
	}
	for _, box := range plan.Boxes {
		if box.Rect.Width < 0 || box.Rect.Height < 0 {
			t.Fatalf("negative rect: %#v", box)
		}
		if box.Clip.Width < 0 || box.Clip.Height < 0 {
			t.Fatalf("negative clip: %#v", box)
		}
	}
	if !containsBox(plan.Boxes, "case-b") || containsBox(plan.Boxes, "case-a") {
		t.Fatalf("conditional layout did not select the active child: %#v", plan.Boxes)
	}
	if !containsBox(plan.Boxes, "dialog") {
		t.Fatalf("overlay child missing from plan")
	}
}

func TestArrangeOmitsHiddenOffscreenAndZeroAreaFocusableNodes(t *testing.T) {
	t.Parallel()
	root := visibleNode(t, "root", map[string]string{"layout.kind": "stack"},
		visibleNode(t, "visible", nil),
		node(t, "hidden", nil, semantic.Flags{Visible: false}),
		node(t, "offscreen", nil, semantic.Flags{Visible: true, Offscreen: true}),
		visibleNode(t, "focus-wrapper", map[string]string{"layout.kind": "inset", "layout.inset": "8"},
			node(t, "focus-trap", nil, semantic.Flags{Visible: true, Focusable: true}),
		),
	)
	plan, err := Arrange(context.Background(), root, Size{Width: 12, Height: 2}, 32)
	if err != nil {
		t.Fatal(err)
	}
	if containsBox(plan.Boxes, "hidden") || containsBox(plan.Boxes, "offscreen") || containsBox(plan.Boxes, "focus-trap") {
		t.Fatalf("hidden or offscreen nodes leaked into plan: %#v", plan.Boxes)
	}
}

func TestMeasureAndArrangeAreDeterministic(t *testing.T) {
	t.Parallel()
	root := visibleNode(t, "root", map[string]string{"layout.kind": "stack"}, visibleNode(t, "child", nil))
	engine := DefaultEngine{Options: DefaultOptions()}
	a, err := engine.Arrange(context.Background(), root, Rect{Width: 20, Height: 4})
	if err != nil {
		t.Fatal(err)
	}
	b, err := engine.Arrange(context.Background(), root, Rect{Width: 20, Height: 4})
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("nondeterministic layout hash: %x != %x", a.Hash, b.Hash)
	}
}

func TestHashPlanGolden(t *testing.T) {
	t.Parallel()
	plan := Plan{
		Viewport: Size{Width: 120, Height: 40},
		Window:   Rect{X: 2, Y: 3, Width: 80, Height: 20},
		Content:  Size{Width: 160, Height: 80},
		Boxes: []Box{{
			NodeID:     semantic.NodeID("n1_aaaaaaaaaaaaaaaaaaaaaaaaaa"),
			Generation: 7,
			Rect:       Rect{X: 4, Y: 5, Width: 60, Height: 10},
			Clip:       Rect{X: 6, Y: 7, Width: 50, Height: 8},
			Z:          -2,
		}},
	}
	const want = "75343cd6daaec47a7c782414b8764a469f817e27d521630e0d892e351cff5b1f"
	if got := fmt.Sprintf("%x", hashPlan(plan)); got != want {
		t.Fatalf("plan hash = %s, want %s", got, want)
	}
}

func TestMeasureIntrinsicTextWidthAcrossFastAndUnicodePaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value semantic.Value
		width int
	}{
		{name: "node", value: semantic.Value{HasValue: true, Text: "value"}, width: 11},
		{name: "界", value: semantic.Value{HasValue: true, Text: "a\u0301"}, width: 5},
		{name: "a\tb", width: 5},
		{width: len("text")},
		{value: semantic.SecretValue(), width: len("text")},
		{value: semantic.Value{Text: "ignored"}, width: len("text")},
	}
	for index, test := range tests {
		id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "a", View: "v", Kind: "text", Entity: fmt.Sprint(index), Slot: "intrinsic"})
		if err != nil {
			t.Fatal(err)
		}
		n, err := semantic.NewNode(semantic.NodeSpec{ID: id, Role: "text", Name: test.name, Value: test.value, Flags: semantic.Flags{Visible: true}})
		if err != nil {
			t.Fatal(err)
		}
		measurement, err := (DefaultEngine{Options: DefaultOptions()}).Measure(context.Background(), n, Constraints{})
		if err != nil {
			t.Fatal(err)
		}
		if measurement.Size.Width != test.width {
			t.Fatalf("intrinsic width for %q/%q = %d, want %d", test.name, test.value.Text, measurement.Size.Width, test.width)
		}
	}
}

func TestArrangeCanonicalizesOverlayPaintOrderAndHash(t *testing.T) {
	t.Parallel()
	root := visibleNode(t, "root", map[string]string{"layout.kind": "overlay"},
		visibleNode(t, "low", map[string]string{"layout.z": "0"}),
		visibleNode(t, "high", map[string]string{"layout.z": "5"}),
	)
	plan, err := Arrange(context.Background(), root, Size{Width: 20, Height: 4}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Boxes) != 3 {
		t.Fatalf("unexpected overlay plan: %#v", plan.Boxes)
	}
	if plan.Boxes[1].NodeID == plan.Boxes[2].NodeID {
		t.Fatalf("overlay order collapsed: %#v", plan.Boxes)
	}
	again, err := Arrange(context.Background(), root, Size{Width: 20, Height: 4}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Hash != again.Hash {
		t.Fatalf("overlay hash changed across identical trees: %x != %x", plan.Hash, again.Hash)
	}
}

func TestArrangeCancelsAndEnforcesBudgets(t *testing.T) {
	t.Parallel()
	root := visibleNode(t, "root", nil, visibleNode(t, "a", nil), visibleNode(t, "b", nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Arrange(ctx, root, Size{Width: 10, Height: 4}, 10); err == nil {
		t.Fatal("expected cancellation")
	}
	engine := DefaultEngine{Options: Options{Budgets: Budgets{MaxNodes: 2, MaxDepth: 8, MaxWork: 32}, TabWidth: 4}}
	if _, err := engine.Arrange(context.Background(), root, Rect{Width: 10, Height: 4}); err == nil {
		t.Fatal("expected node budget error")
	}
}

func TestMeasureRespectsConstraintsAndInsets(t *testing.T) {
	t.Parallel()
	root := visibleNode(t, "root", map[string]string{"layout.kind": "inset", "layout.inset": "1", "layout.minWidth": "5", "layout.maxWidth": "8"}, visibleNode(t, "child", nil))
	engine := DefaultEngine{Options: DefaultOptions()}
	measurement, err := engine.Measure(context.Background(), root, Constraints{MinWidth: 2, MaxWidth: 6, MinHeight: 1, MaxHeight: 4})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Size.Width != 6 {
		t.Fatalf("constraint clamp mismatch: %#v", measurement.Size)
	}
}

func FuzzArrange(f *testing.F) {
	f.Add(10, 4)
	f.Add(40, 20)
	f.Fuzz(func(t *testing.T, w, h int) {
		if w < 0 || h < 0 {
			t.Skip()
		}
		root := visibleNode(t, "root", map[string]string{"layout.kind": "stack"}, visibleNode(t, "child", nil))
		plan, err := Arrange(context.Background(), root, Size{Width: w, Height: h}, 16)
		if err != nil {
			return
		}
		for _, box := range plan.Boxes {
			if box.Rect.Width < 0 || box.Rect.Height < 0 {
				t.Fatalf("invalid rect: %#v", box)
			}
		}
	})
}

func BenchmarkArrange2000Nodes(b *testing.B) {
	makeTree := func(nodes int) semantic.Node {
		children := make([]semantic.Node, 0, nodes)
		for i := 0; i < nodes; i++ {
			meta := map[string]string{"layout.kind": "records"}
			if i%25 == 0 {
				meta = map[string]string{"layout.kind": "overlay"}
			}
			children = append(children, benchNode(b, fmt.Sprintf("leaf-%04d", i), meta))
		}
		return benchNode(b, "root", map[string]string{"layout.kind": "records", "layout.gap": "0"}, children...)
	}
	root := makeTree(2000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Arrange(context.Background(), root, Size{Width: 120, Height: 40}, 5000); err != nil {
			b.Fatal(err)
		}
	}
}

func benchNode(tb testing.TB, name string, meta map[string]string, cs ...semantic.Node) semantic.Node {
	tb.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "a", View: "v", Kind: "text", Entity: name, Slot: "bench"})
	if err != nil {
		tb.Fatal(err)
	}
	n, err := semantic.NewNode(semantic.NodeSpec{
		ID:       id,
		Role:     "text",
		Name:     name,
		Metadata: meta,
		Children: cs,
		Flags:    semantic.Flags{Visible: true},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return n
}

func containsBox(boxes []Box, entity string) bool {
	id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "a", View: "v", Kind: "text", Entity: entity, Slot: "s"})
	if err != nil {
		return false
	}
	for _, box := range boxes {
		if box.NodeID == id {
			return true
		}
	}
	return false
}
