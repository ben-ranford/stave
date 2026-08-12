package lipglossadapter

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	lg "charm.land/lipgloss/v2"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/surface"
	"github.com/charmbracelet/colorprofile"
)

type EffectiveColors struct {
	Foreground string
	Background string
}

type CompiledStyle struct {
	ID             surface.StyleID
	Style          lg.Style
	Effective      EffectiveColors
	Profile        Profile
	BackgroundFlip bool
}

func CompileStyles(styles []surface.ResolvedStyle, profile Profile) (map[surface.StyleID]CompiledStyle, error) {
	out := make(map[surface.StyleID]CompiledStyle, len(styles))
	for i, style := range styles {
		compiled, err := CompileStyle(surface.StyleID(i+1), style, profile)
		if err != nil {
			return nil, err
		}
		out[compiled.ID] = compiled
	}
	return out, nil
}

func CompileStyle(id surface.StyleID, resolved surface.ResolvedStyle, profile Profile) (CompiledStyle, error) {
	style := lg.NewStyle().
		Bold(resolved.Bold).
		Faint(resolved.Dim).
		Italic(resolved.Italic).
		Blink(resolved.Blink).
		Reverse(resolved.Reverse).
		Underline(resolved.Underline)

	compiled := CompiledStyle{
		ID:      id,
		Style:   style,
		Profile: profile,
	}

	fgColor, fgToken, err := resolveColor(resolved.Foreground, profile, false)
	if err != nil {
		return CompiledStyle{}, err
	}
	compiled.Effective.Foreground = fgToken
	if fgColor != nil {
		compiled.Style = compiled.Style.Foreground(fgColor)
	}

	bgColor, bgToken, err := resolveColor(resolved.Background, profile, true)
	if err != nil {
		return CompiledStyle{}, err
	}
	compiled.Effective.Background = bgToken
	if bgColor != nil {
		compiled.Style = compiled.Style.Background(bgColor)
	}

	if profile.ColorLevel == capability.ColorMonochrome && bgToken != "" {
		compiled.Style = compiled.Style.Reverse(true)
		compiled.BackgroundFlip = true
	}
	if resolved.Background != "" || compiled.BackgroundFlip {
		compiled.Style = compiled.Style.ColorWhitespace(true)
	}
	return compiled, nil
}

func resolveColor(raw string, profile Profile, background bool) (color.Color, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return lg.NoColor{}, "", nil
	}
	if profile.ColorLevel == capability.ColorNone {
		return lg.NoColor{}, "", nil
	}
	if profile.ColorLevel == capability.ColorMonochrome {
		if background {
			return nil, raw, nil
		}
		contrast := lg.LightDark(profile.DarkBackground)(lg.Black, lg.White)
		return contrast, "mono:contrast", nil
	}

	base, err := parseBaseColor(raw)
	if err != nil {
		return nil, "", err
	}
	converted := downsample(base, profile.ColorProfile())
	return converted, normalizeColor(converted), nil
}

func parseBaseColor(raw string) (color.Color, error) {
	switch {
	case strings.HasPrefix(raw, "#"):
		return lg.Color(raw), nil
	case strings.HasPrefix(raw, "ansi256:"):
		v, err := strconv.Atoi(strings.TrimPrefix(raw, "ansi256:"))
		if err != nil {
			return nil, fmt.Errorf("invalid ansi256 color %q", raw)
		}
		return lg.Color(strconv.Itoa(v)), nil
	case strings.HasPrefix(raw, "ansi16:"):
		v, err := strconv.Atoi(strings.TrimPrefix(raw, "ansi16:"))
		if err != nil {
			return nil, fmt.Errorf("invalid ansi16 color %q", raw)
		}
		// Accept both ANSI SGR literals (30..37, 90..97) and canonical
		// palette indexes (0..15), mapping literals to the palette index.
		switch {
		case v >= 30 && v <= 37:
			v -= 30
		case v >= 90 && v <= 97:
			v -= 90
			v += 8
		case v < 0 || v > 15:
			return nil, fmt.Errorf("ansi16 color out of range %q", raw)
		}
		return lg.Color(strconv.Itoa(v)), nil
	case strings.HasPrefix(raw, "mono:"):
		return lg.NoColor{}, nil
	default:
		return nil, fmt.Errorf("unsupported color token %q", raw)
	}
}

func downsample(c color.Color, profile colorprofile.Profile) color.Color {
	if c == nil {
		return nil
	}
	if _, ok := c.(lg.NoColor); ok {
		return c
	}
	return profile.Convert(c)
}

func normalizeColor(c color.Color) string {
	switch v := c.(type) {
	case nil:
		return ""
	case lg.NoColor:
		return ""
	case interface{ String() string }:
		switch s := v.String(); s {
		case "black":
			return "ansi16:0"
		case "red":
			return "ansi16:1"
		case "green":
			return "ansi16:2"
		case "yellow":
			return "ansi16:3"
		case "blue":
			return "ansi16:4"
		case "magenta":
			return "ansi16:5"
		case "cyan":
			return "ansi16:6"
		case "white":
			return "ansi16:7"
		case "bright-black":
			return "ansi16:8"
		case "bright-red":
			return "ansi16:9"
		case "bright-green":
			return "ansi16:10"
		case "bright-yellow":
			return "ansi16:11"
		case "bright-blue":
			return "ansi16:12"
		case "bright-magenta":
			return "ansi16:13"
		case "bright-cyan":
			return "ansi16:14"
		case "bright-white":
			return "ansi16:15"
		}
		return v.String()
	case lg.ANSIColor:
		return fmt.Sprintf("ansi256:%d", int(v))
	case color.RGBA:
		return fmt.Sprintf("#%02x%02x%02x", v.R, v.G, v.B)
	case interface {
		RGBA() (uint32, uint32, uint32, uint32)
	}:
		r, g, b, _ := v.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
	default:
		return fmt.Sprintf("%T", c)
	}
}
