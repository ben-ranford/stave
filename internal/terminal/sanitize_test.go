package terminal

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitizeStripsHostileControls(t *testing.T) {
	t.Parallel()
	got := Sanitize("a\x1b[31mred\x1b[0m\x1b]52;c;secret\x07\x90b")
	if got != "aredb" {
		t.Fatalf("sanitize mismatch: %q", got)
	}
}

func TestSanitizeDropsDCSAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	in := append([]byte("x\x1bPpayload\x1b\\y"), 0xff)
	got := string(SanitizeBytes(in))
	if got != "xy�" {
		t.Fatalf("sanitize bytes mismatch: %q", got)
	}
}

func TestWriterSanitizesFinalSink(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if _, err := w.WriteString("a\x1b]2;title\x07b"); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if buf.String() != "ab" {
		t.Fatalf("writer leaked control: %q", buf.String())
	}
}

func FuzzSanitize(f *testing.F) {
	f.Add("hello")
	f.Add("\x1b]52;c;secret\x07")
	f.Add(strings.Repeat("A", 64))
	f.Fuzz(func(t *testing.T, s string) {
		out := Sanitize(s)
		if strings.ContainsRune(out, 0x1b) {
			t.Fatalf("escape survived: %q", out)
		}
	})
}

func BenchmarkSanitize(b *testing.B) {
	text := strings.Repeat("safe\x1b]52;c;secret\x07unsafe", 32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Sanitize(text)
	}
}
