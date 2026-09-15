package surface

import (
	"slices"
	"testing"

	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/semantic"
)

func TestPatchRoundTripAndDirtyRegions(t *testing.T) {
	t.Parallel()
	a := New(4, 1)
	a, err := a.WithText(0, 0, "ab", ResolvedStyle{Foreground: "#fff"}, "", "n1_aaaaaaaaaaaaaaaaaaaaaaaaaa", 1, layout.Rect{Width: 4, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	b := New(4, 1)
	b, err = b.WithText(0, 0, "a界", ResolvedStyle{Foreground: "#fff", Bold: true}, "", "n1_bbbbbbbbbbbbbbbbbbbbbbbbbb", 2, layout.Rect{Width: 4, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	patch := Diff(a, b)
	if len(patch.Runs) == 0 || len(patch.Dirty) == 0 {
		t.Fatal("expected dirty diff")
	}
	if patch.Resize {
		t.Fatalf("unexpected resize patch: %#v", patch)
	}
	next, err := a.ApplyPatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	if next.Hash() != b.Hash() {
		t.Fatalf("hash mismatch after patch")
	}
}

func TestSurfaceClipsAndPreservesWideGraphemes(t *testing.T) {
	t.Parallel()
	s := New(4, 1)
	var err error
	s, err = s.WithText(0, 0, "界a", ResolvedStyle{}, "", semantic.NodeID("n1_aaaaaaaaaaaaaaaaaaaaaaaaaa"), 1, layout.Rect{X: 0, Y: 0, Width: 2, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.At(0, 0); got.Grapheme != "界" || !s.At(1, 0).Continuation {
		t.Fatalf("wide grapheme mismatch: %#v %#v", got, s.At(1, 0))
	}
	before := s.Hash()
	s, err = s.WithStyledGrapheme(1, 0, "b", ResolvedStyle{}, "", semantic.NodeID("n1_bbbbbbbbbbbbbbbbbbbbbbbbbb"), 1, layout.Rect{X: 1, Y: 0, Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if before == s.Hash() {
		t.Fatal("expected overwrite to update hash")
	}
	if s.At(0, 0).Grapheme != "" || s.At(1, 0).Grapheme != "b" {
		t.Fatalf("half overwrite survived: %q / %q", s.At(0, 0).Grapheme, s.At(1, 0).Grapheme)
	}
}

func TestWideRawCellsRespectContinuationOwnershipAtRightEdge(t *testing.T) {
	t.Parallel()
	base := New(2, 1).WithCell(0, 0, Cell{Grapheme: "a", Width: 1})
	for _, grapheme := range []string{"界", "🙂"} {
		got := base.WithCell(1, 0, Cell{Grapheme: grapheme, Width: 2})
		if got.Hash() != base.Hash() || got.At(1, 0) != base.At(1, 0) {
			t.Fatalf("right-edge %q cell changed the surface: %#v", grapheme, got.At(1, 0))
		}
		roundTrip, err := base.ApplyPatch(Diff(base, got))
		if err != nil {
			t.Fatal(err)
		}
		if roundTrip.Hash() != base.Hash() {
			t.Fatalf("right-edge %q patch changed the surface", grapheme)
		}
	}

	valid := base.WithCell(0, 0, Cell{Grapheme: "界", Width: 2})
	if lead, continuation := valid.At(0, 0), valid.At(1, 0); lead.Width != 2 || !continuation.Continuation {
		t.Fatalf("valid wide cell ownership mismatch: %#v %#v", lead, continuation)
	}
	if base.At(0, 0).Grapheme != "a" {
		t.Fatalf("WithCell mutated the original surface: %#v", base.At(0, 0))
	}

	builder, err := NewBuilder(2, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	builder.putCell(1, 0, Cell{Grapheme: "界", Width: 2})
	if got := builder.Surface(); got.At(1, 0).Width != 0 || got.At(0, 0).Continuation {
		t.Fatalf("builder accepted a truncated wide cell: %#v %#v", got.At(0, 0), got.At(1, 0))
	}
}

func TestSurfaceDeterminismAndMergeDirty(t *testing.T) {
	t.Parallel()
	s := New(3, 2)
	first, err := s.WithText(0, 0, "abc", ResolvedStyle{Foreground: "#123456"}, "https://example.com", semantic.NodeID("n1_cccccccccccccccccccccccccc"), 1, layout.Rect{Width: 3, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.WithText(0, 0, "abc", ResolvedStyle{Foreground: "#123456"}, "https://example.com", semantic.NodeID("n1_cccccccccccccccccccccccccc"), 1, layout.Rect{Width: 3, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash() != second.Hash() {
		t.Fatalf("nondeterministic surface hash")
	}
	merged := MergeDirty([]Region{{X: 0, Y: 0, Width: 1, Height: 1}, {X: 1, Y: 0, Width: 1, Height: 1}})
	if len(merged) != 1 || merged[0].Width != 2 {
		t.Fatalf("merge mismatch: %#v", merged)
	}
}

func TestWithTextMatchesSeededBuilderAndPreservesInput(t *testing.T) {
	t.Parallel()
	base := New(12, 2)
	var err error
	base, err = base.WithText(0, 0, "seed", ResolvedStyle{Foreground: "#112233"}, "https://seed.example", semantic.NodeID("n1_seeddddddddddddddddddddddd"), 1, layout.Rect{Width: 12, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	before := base.clone()

	builder := seededBuilder(base)
	style := ResolvedStyle{Foreground: "#abcdef", Background: "#010203", Bold: true}
	clip := layout.Rect{X: 0, Y: 0, Width: 8, Height: 1}
	if err := builder.WithText(1, 0, "a\t界z", style, "https://text.example", semantic.NodeID("n1_texttttttttttttttttttttttt"), 2, clip); err != nil {
		t.Fatal(err)
	}
	if err := builder.WithText(6, 1, "ok", ResolvedStyle{Italic: true}, "", semantic.NodeID("n1_seconddddddddddddddddddddd"), 3, layout.Rect{Width: 12, Height: 2}); err != nil {
		t.Fatal(err)
	}
	want := builder.Surface()

	got, err := base.WithText(1, 0, "a\t界z", style, "https://text.example", semantic.NodeID("n1_texttttttttttttttttttttttt"), 2, clip)
	if err != nil {
		t.Fatal(err)
	}
	got, err = got.WithText(6, 1, "ok", ResolvedStyle{Italic: true}, "", semantic.NodeID("n1_seconddddddddddddddddddddd"), 3, layout.Rect{Width: 12, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != want.Width || got.Height != want.Height || got.Hash() != want.Hash() || !slices.Equal(got.Cells(), want.Cells()) || !slices.Equal(got.Styles(), want.Styles()) || !slices.Equal(got.Links(), want.Links()) {
		t.Fatalf("WithText differs from seeded builder\n got=%#v\nwant=%#v", got, want)
	}
	if base.Width != before.Width || base.Height != before.Height || base.Hash() != before.Hash() || !slices.Equal(base.Cells(), before.Cells()) || !slices.Equal(base.Styles(), before.Styles()) || !slices.Equal(base.Links(), before.Links()) {
		t.Fatalf("WithText mutated input\n got=%#v\nwant=%#v", base, before)
	}
	patched, err := base.ApplyPatch(Diff(base, got))
	if err != nil {
		t.Fatal(err)
	}
	if patched.Hash() != got.Hash() {
		t.Fatalf("diff/patch hash mismatch: got %x want %x", patched.Hash(), got.Hash())
	}
}

func TestWithTextAllocationEnvelopeMatchesBuilder(t *testing.T) {
	line := stringsRepeat("abc界", 50)
	style := ResolvedStyle{Foreground: "#abcdef"}
	clip := layout.Rect{Width: 240, Height: 1}
	immutableAllocs := testing.AllocsPerRun(10, func() {
		_, err := New(240, 1).WithText(0, 0, line, style, "https://text.example", semantic.NodeID("n1_alloccccccccccccccccccccccc"), 1, clip)
		if err != nil {
			t.Fatal(err)
		}
	})
	builderAllocs := testing.AllocsPerRun(10, func() {
		builder, err := NewBuilder(240, 1, 240)
		if err != nil {
			t.Fatal(err)
		}
		if err := builder.WithText(0, 0, line, style, "https://text.example", semantic.NodeID("n1_alloccccccccccccccccccccccc"), 1, clip); err != nil {
			t.Fatal(err)
		}
		_ = builder.Surface()
	})
	t.Logf("immutable WithText allocations %.0f; builder allocations %.0f", immutableAllocs, builderAllocs)
	if immutableAllocs > builderAllocs+8 {
		t.Fatalf("immutable WithText allocations %.0f exceed builder envelope %.0f", immutableAllocs, builderAllocs)
	}
}

func TestWithTextUsesFirstMatchingInternedValues(t *testing.T) {
	style := ResolvedStyle{Foreground: "#abcdef"}
	link := "https://text.example"
	base := Surface{
		Width:  2,
		Height: 1,
		cells:  make([]Cell, 2),
		styles: []ResolvedStyle{style, style},
		links:  []string{link, link},
	}.finalize()
	got, err := base.WithText(0, 0, "x", style, link, semantic.NodeID("n1_duplicateeeeeeeeeeeeeeeeee"), 1, layout.Rect{Width: 2, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cell := got.At(0, 0); cell.Style != 1 || cell.Link != 1 {
		t.Fatalf("WithText changed first-match interning: %#v", cell)
	}
	builder := seededBuilder(base)
	if err := builder.WithText(0, 0, "x", style, link, semantic.NodeID("n1_duplicateeeeeeeeeeeeeeeeee"), 1, layout.Rect{Width: 2, Height: 1}); err != nil {
		t.Fatal(err)
	}
	if built := builder.Surface(); built.Hash() != got.Hash() {
		t.Fatalf("seeded builder changed first-match interning: %#v", built.At(0, 0))
	}
}

func seededBuilder(s Surface) *Builder {
	styles := make(map[ResolvedStyle]StyleID, len(s.styles))
	for i, style := range s.styles {
		if _, exists := styles[style]; !exists {
			styles[style] = StyleID(i + 1)
		}
	}
	links := make(map[string]LinkID, len(s.links))
	for i, link := range s.links {
		if _, exists := links[link]; !exists {
			links[link] = LinkID(i + 1)
		}
	}
	return &Builder{surface: s.clone(), styleIndex: styles, linkIndex: links}
}

func TestWithTextNoOpsPreserveZeroSizedAndClippedSurfaces(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    Surface
		text string
		clip layout.Rect
	}{
		{name: "zero width", s: New(0, 1), text: "x", clip: layout.Rect{Width: 1, Height: 1}},
		{name: "zero height", s: New(1, 0), text: "x", clip: layout.Rect{Width: 1, Height: 1}},
		{name: "empty", s: New(80, 50), text: "", clip: layout.Rect{Width: 80, Height: 50}},
		{name: "newline", s: New(80, 50), text: "\ntext", clip: layout.Rect{Width: 80, Height: 50}},
		{name: "clipped", s: New(80, 50), text: "text", clip: layout.Rect{X: 80, Y: 0, Width: 1, Height: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.s.clone()
			got, err := tc.s.WithText(0, 0, tc.text, ResolvedStyle{Foreground: "#abcdef"}, "https://text.example", semantic.NodeID("n1_nooppppppppppppppppppppppp"), 1, tc.clip)
			if err != nil {
				t.Fatal(err)
			}
			if got.Hash() != before.Hash() || !slices.Equal(got.Cells(), before.Cells()) || !slices.Equal(got.Styles(), before.Styles()) || !slices.Equal(got.Links(), before.Links()) {
				t.Fatalf("no-op write changed surface: got=%#v want=%#v", got, before)
			}
		})
	}
}

func TestWithTextClippedWriteAvoidsSurfaceClone(t *testing.T) {
	base := New(240, 40)
	clip := layout.Rect{X: 240, Y: 0, Width: 1, Height: 1}
	allocs := testing.AllocsPerRun(10, func() {
		got, err := base.WithText(0, 0, "text", ResolvedStyle{}, "", "", 0, clip)
		if err != nil {
			t.Fatal(err)
		}
		if got.Hash() != base.Hash() {
			t.Fatal("clipped write changed hash")
		}
	})
	t.Logf("clipped WithText allocations %.0f", allocs)
	if allocs > 2 {
		t.Fatalf("clipped WithText allocations %.0f indicate a surface clone", allocs)
	}
}

func TestDiffRepresentsResize(t *testing.T) {
	t.Parallel()
	a := New(2, 1)
	b := New(4, 2)
	var err error
	b, err = b.WithText(0, 0, "wide", ResolvedStyle{Foreground: "#fff"}, "", semantic.NodeID("n1_resizeeeeeeeeeeeeeeeeeeeee"), 1, layout.Rect{Width: 4, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	patch := Diff(a, b)
	if !patch.Resize || patch.Width != 4 || patch.Height != 2 {
		t.Fatalf("resize metadata missing: %#v", patch)
	}
	next, err := a.ApplyPatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	if next.Width != 4 || next.Height != 2 || next.Hash() != b.Hash() {
		t.Fatalf("resize roundtrip mismatch: %#v", next)
	}
}

func FuzzDiffApply(f *testing.F) {
	f.Add("abc", "abd")
	f.Add("界", "👩‍💻")
	f.Fuzz(func(t *testing.T, left, right string) {
		a := New(16, 1)
		b := New(16, 1)
		var err error
		a, err = a.WithText(0, 0, left, ResolvedStyle{}, "", semantic.NodeID("n1_dddddddddddddddddddddddddd"), 1, layout.Rect{Width: 16, Height: 1})
		if err != nil {
			t.Fatal(err)
		}
		b, err = b.WithText(0, 0, right, ResolvedStyle{}, "", semantic.NodeID("n1_eeeeeeeeeeeeeeeeeeeeeeeeee"), 1, layout.Rect{Width: 16, Height: 1})
		if err != nil {
			t.Fatal(err)
		}
		next, err := a.ApplyPatch(Diff(a, b))
		if err != nil {
			t.Fatal(err)
		}
		if next.Hash() != b.Hash() {
			t.Fatalf("roundtrip mismatch")
		}
	})
}

func BenchmarkDiff(b *testing.B) {
	a := New(120, 40)
	c, _ := a.WithText(0, 0, stringsRepeat("abc界", 200), ResolvedStyle{Foreground: "#fff"}, "", semantic.NodeID("n1_ffffffffffffffffffffffffff"), 1, layout.Rect{Width: 120, Height: 40})
	d, _ := c.WithText(0, 1, stringsRepeat("xyz", 100), ResolvedStyle{Foreground: "#000"}, "", semantic.NodeID("n1_gggggggggggggggggggggggggg"), 2, layout.Rect{Width: 120, Height: 40})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Diff(c, d)
	}
}

func stringsRepeat(s string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out += s
	}
	return out
}
