package width

import (
	"strings"
	"testing"
)

func TestWidthGoldens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		text  string
		width int
	}{
		{name: "ascii", text: "abc", width: 3},
		{name: "combining", text: "a\u0301", width: 1},
		{name: "wide", text: "界", width: 2},
		{name: "emoji", text: "🙂", width: 2},
		{name: "zwj", text: "👩‍💻", width: 2},
		{name: "regional", text: "🇦🇺", width: 2},
		{name: "control", text: "a\x07b", width: 2},
		{name: "tab", text: "a\tb", width: 5},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := String(tc.text); got != tc.width {
				t.Fatalf("String(%q) = %d, want %d", tc.text, got, tc.width)
			}
		})
	}
}

func TestNormalizeTabsAndASCII(t *testing.T) {
	t.Parallel()
	if got := NormalizeTabs("ab\tc", 4); got != "ab  c" {
		t.Fatalf("NormalizeTabs mismatch: %q", got)
	}
	if got := ASCIIOnly("π🙂"); got != "\\u03C0\\U0001F642" {
		t.Fatalf("ASCIIOnly mismatch: %q", got)
	}
}

func TestTruncatePreservesGraphemes(t *testing.T) {
	t.Parallel()
	got := Truncate("A👩‍💻BC", 4, "…")
	if got != "A👩‍💻…" {
		t.Fatalf("truncate split grapheme: %q", got)
	}
}

func TestClustersMarkInvalidBytes(t *testing.T) {
	t.Parallel()
	clusters := DefaultPolicy.Clusters(string([]byte{'a', 0xff, 'b'}))
	if len(clusters) != 3 || !clusters[1].Invalid || clusters[1].Width != 1 {
		t.Fatalf("invalid cluster mismatch: %#v", clusters)
	}
}

func FuzzStringWidth(f *testing.F) {
	f.Add("abc")
	f.Add("👩‍💻\t界")
	f.Add(strings.Repeat("a", 32))
	f.Fuzz(func(t *testing.T, s string) {
		if got := String(s); got < 0 {
			t.Fatalf("negative width: %d", got)
		}
		trunc := Truncate(s, 8, "…")
		if String(trunc) > 8 {
			t.Fatalf("truncate exceeded width: %d", String(trunc))
		}
	})
}

func BenchmarkStringWidth(b *testing.B) {
	text := strings.Repeat("界👩‍💻abc\t", 64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = String(text)
	}
}
