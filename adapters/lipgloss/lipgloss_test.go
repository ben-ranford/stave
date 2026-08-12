package lipglossadapter

import (
	"strings"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/surface"
	"github.com/ben-ranford/stave/testfixture"
)

func TestCompileStylePreservesSemanticStyleMetadata(t *testing.T) {
	profile := Profile{
		OutputMode:     capability.OutputAuto,
		ColorLevel:     capability.ColorTrueColor,
		TTY:            true,
		DarkBackground: true,
	}

	compiled, err := CompileStyle(1, surface.ResolvedStyle{
		Foreground: "#f5f5f5",
		Background: "#202530",
		Bold:       true,
	}, profile)
	if err != nil {
		t.Fatal(err)
	}

	if got := compiled.Effective.Foreground; got != "#f5f5f5" {
		t.Fatalf("fg = %q", got)
	}
	if got := compiled.Effective.Background; got != "#202530" {
		t.Fatalf("bg = %q", got)
	}
	if !compiled.Style.GetColorWhitespace() {
		t.Fatal("style should color whitespace")
	}
}

func TestSharedAdapterSurfaceFixture(t *testing.T) {
	shared, err := testfixture.Surface()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Render(RenderRequest{Surface: shared, Profile: Profile{ColorLevel: capability.ColorANSI16, TTY: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != shared.Height || strings.TrimRight(got.Rows[0].Plain, " ") != "Stave" {
		t.Fatalf("shared surface projection changed: %+v", got.Rows)
	}
}

func TestRenderSurfaceTracksSemanticRunsNotBytes(t *testing.T) {
	profile := Profile{
		OutputMode:     capability.OutputAuto,
		ColorLevel:     capability.ColorTrueColor,
		TTY:            true,
		DarkBackground: false,
	}
	s := surface.New(4, 2)
	title := surface.ResolvedStyle{Foreground: "#f5f5f5", Background: "#202530", Bold: true}
	alert := surface.ResolvedStyle{Foreground: "#fbfbfd", Background: "#1d2533", Bold: true}
	var err error
	s, err = s.WithText(0, 0, "A", title, "", semantic.NodeID("n1_aaaaaaaaaaaaaaaaaaaaaaaaaa"), 1, layout.Rect{Width: 4, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.WithText(1, 0, "B", title, "", semantic.NodeID("n1_aaaaaaaaaaaaaaaaaaaaaaaaaa"), 1, layout.Rect{Width: 4, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	s.Put(2, 0, surface.Cell{Width: 1, Style: 1})
	s, err = s.WithText(3, 0, "!", alert, "", semantic.NodeID("n1_aaaaaaaaaaaaaaaaaaaaaaaaaa"), 1, layout.Rect{Width: 4, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.WithText(0, 1, "x", surface.ResolvedStyle{}, "", semantic.NodeID("n1_bbbbbbbbbbbbbbbbbbbbbbbbbb"), 1, layout.Rect{Width: 4, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.WithText(1, 1, "y", alert, "", semantic.NodeID("n1_bbbbbbbbbbbbbbbbbbbbbbbbbb"), 1, layout.Rect{Width: 4, Height: 2})
	if err != nil {
		t.Fatal(err)
	}

	result, err := Render(RenderRequest{Surface: s, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}

	if got := result.Rows[0].Plain; got != "AB !" {
		t.Fatalf("row0 plain = %q", got)
	}
	if got := result.Rows[1].Plain; got != "xy  " {
		t.Fatalf("row1 plain = %q", got)
	}
	if len(result.Segments) != 5 {
		t.Fatalf("segments = %d", len(result.Segments))
	}
	if result.Rows[0].Segments[0].Text != "AB " || result.Rows[0].Segments[0].StyleID != 1 {
		t.Fatalf("row0 segment0 = %#v", result.Rows[0].Segments[0])
	}
	if result.Rows[0].Segments[1].Text != "!" || result.Rows[0].Segments[1].StyleID != 2 {
		t.Fatalf("row0 segment1 = %#v", result.Rows[0].Segments[1])
	}
	if result.Rows[0].Width != 4 || result.Rows[1].Width != 4 {
		t.Fatalf("row widths = %d,%d", result.Rows[0].Width, result.Rows[1].Width)
	}
	if !strings.Contains(result.Output, "AB ") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestANSIColorFixtures(t *testing.T) {
	tests := []struct {
		name     string
		level    capability.ColorLevel
		expected string
	}{
		{
			name:     "truecolor",
			level:    capability.ColorTrueColor,
			expected: "\x1b[1;38;2;245;245;245;48;2;32;37;48mX\x1b[m",
		},
		{
			name:     "ansi256",
			level:    capability.ColorANSI256,
			expected: "\x1b[1;38;5;255;48;5;17mX\x1b[m",
		},
		{
			name:     "ansi16",
			level:    capability.ColorANSI16,
			expected: "\x1b[1;97;44mX\x1b[m",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Render(RenderRequest{
				Surface: styledSurface(t, "X", surface.ResolvedStyle{
					Foreground: "#f5f5f5",
					Background: "#202530",
					Bold:       true,
				}),
				Profile: Profile{OutputMode: capability.OutputAuto, ColorLevel: tc.level, TTY: true, DarkBackground: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Output != tc.expected {
				t.Fatalf("fixture mismatch\nwant: %q\ngot:  %q", tc.expected, result.Output)
			}
		})
	}
}

func TestMonochromeAndNoColorDegradeSafely(t *testing.T) {
	catalogStyle := surface.ResolvedStyle{Foreground: "#fbfbfd", Background: "#1d2533", Bold: true}
	mono, err := Render(RenderRequest{
		Surface: styledSurface(t, "X", catalogStyle),
		Profile: Profile{OutputMode: capability.OutputAuto, ColorLevel: capability.ColorMonochrome, TTY: true, DarkBackground: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mono.Output, "\x1b[") {
		t.Fatal("monochrome should still use a deterministic terminal emphasis path")
	}
	if !mono.Styles[1].BackgroundFlip {
		t.Fatal("monochrome background should flip emphasis")
	}

	plain, err := Render(RenderRequest{
		Surface: styledSurface(t, "X", catalogStyle),
		Profile: Profile{OutputMode: capability.OutputPlain, ColorLevel: capability.ColorTrueColor, TTY: false, DarkBackground: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Output != "X" {
		t.Fatalf("plain output = %q", plain.Output)
	}
	if strings.Contains(plain.Output, "\x1b[") {
		t.Fatalf("plain output contains ANSI: %q", plain.Output)
	}

	noColor, err := Render(RenderRequest{
		Surface: styledSurface(t, "X", catalogStyle),
		Profile: Profile{OutputMode: capability.OutputAuto, ColorLevel: capability.ColorNone, TTY: true, DarkBackground: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if noColor.Output != "X" {
		t.Fatalf("no-color output = %q", noColor.Output)
	}
	if strings.Contains(noColor.Output, "\x1b[") {
		t.Fatalf("no-color output contains ANSI: %q", noColor.Output)
	}
}

func TestProfileChangesDoNotChangeGeometry(t *testing.T) {
	req := RenderRequest{
		Surface: styledSurface(t, "Q", surface.ResolvedStyle{
			Foreground: "#f5f5f5",
			Background: "#202530",
			Bold:       true,
		}),
		Profile: Profile{OutputMode: capability.OutputAuto, ColorLevel: capability.ColorTrueColor, TTY: true, DarkBackground: true},
	}

	dark, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}

	req.Profile.ColorLevel = capability.ColorANSI256
	light, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}

	if dark.Rows[0].Width != light.Rows[0].Width {
		t.Fatalf("geometry changed: dark=%d light=%d", dark.Rows[0].Width, light.Rows[0].Width)
	}
	if dark.Rows[0].Plain != light.Rows[0].Plain {
		t.Fatalf("plain semantics changed: dark=%q light=%q", dark.Rows[0].Plain, light.Rows[0].Plain)
	}
	if dark.Rows[0].Segments[0].StyleID != light.Rows[0].Segments[0].StyleID {
		t.Fatalf("semantic style changed: dark=%d light=%d", dark.Rows[0].Segments[0].StyleID, light.Rows[0].Segments[0].StyleID)
	}
}

func styledSurface(t *testing.T, text string, style surface.ResolvedStyle) surface.Surface {
	t.Helper()
	s := surface.New(1, 1)
	var err error
	s, err = s.WithText(0, 0, text, style, "", semantic.NodeID("n1_cccccccccccccccccccccccccc"), 1, layout.Rect{Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
