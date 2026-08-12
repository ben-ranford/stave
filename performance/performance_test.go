package performance

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/surface"
)

func TestPercentilesUsesNearestRank(t *testing.T) {
	p50, p95, p99, err := Percentiles([]time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if err != nil {
		t.Fatal(err)
	}
	if p50 != 5*time.Nanosecond || p95 != 10*time.Nanosecond || p99 != 10*time.Nanosecond {
		t.Fatalf("got %s/%s/%s", p50, p95, p99)
	}
}

func TestMeasureIdleCPUReportsFiniteRatio(t *testing.T) {
	got := MeasureIdleCPU(10 * time.Millisecond)
	if got.Name == "" || got.Window <= 0 || math.IsNaN(got.Value) || math.IsInf(got.Value, 0) || got.Value < 0 {
		t.Fatalf("invalid idle CPU measurement: %+v", got)
	}
}

func TestFixtureHasRequestedNodeCountAndStableHash(t *testing.T) {
	a, err := Fixture(2000)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := Fixture(2000)
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash() != b.Hash() {
		t.Fatal("fixture hash is not deterministic")
	}
}

func TestSurfaceDiffRoundTripIsDeterministic(t *testing.T) {
	a, err := SurfaceFixture(120, 40)
	if err != nil {
		t.Fatal(err)
	}
	b := a.WithCell(0, 0, surface.Cell{Grapheme: "X", Width: 1})
	p := surface.Diff(a, b)
	got, err := a.ApplyPatch(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash() != b.Hash() {
		t.Fatal("patch hash mismatch")
	}
	if (layout.Size{Width: a.Width, Height: a.Height}) != (layout.Size{Width: 120, Height: 40}) {
		t.Fatal("fixture viewport changed")
	}
}

func BenchmarkFixtureValidate2K(b *testing.B) {
	tree, _ := Fixture(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := tree.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkLayout2K120x40(b *testing.B) {
	tree, _ := Fixture(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := layout.Arrange(context.Background(), tree.Root(), layout.Size{Width: 120, Height: 40}, 100000); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkSurfaceDiff120x40(b *testing.B) {
	a, _ := SurfaceFixture(120, 40)
	c := a.WithCell(0, 0, surface.Cell{Grapheme: "X", Width: 1})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = surface.Diff(a, c)
	}
}
