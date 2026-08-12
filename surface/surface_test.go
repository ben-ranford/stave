package surface

import (
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
