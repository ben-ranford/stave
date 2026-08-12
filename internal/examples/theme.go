package examples

import (
	"strings"

	"github.com/ben-ranford/stave/theme"
)

// BrandTheme builds a complete production token set for executable fixtures.
// Real applications own their equivalent theme package and may use any visual
// identity that satisfies the same semantic role and contrast contracts.
func BrandTheme(id, primary, banner string) theme.Theme {
	tokens := theme.TokenSet{}
	for _, role := range theme.RequiredRoleIDs() {
		name := string(role)
		switch {
		case strings.HasPrefix(name, "motion.duration"):
			tokens[role] = theme.Value{Kind: theme.KindDuration, Literal: "120ms"}
		case strings.HasPrefix(name, "motion.easing"):
			tokens[role] = theme.Value{Kind: theme.KindString, Literal: "ease-out"}
		case strings.HasPrefix(name, "space."), strings.HasPrefix(name, "radius."), strings.HasPrefix(name, "elevation."):
			tokens[role] = theme.Value{Kind: theme.KindNumber, Literal: 2}
		case strings.HasPrefix(name, "type."):
			tokens[role] = theme.Value{Kind: theme.KindString, Literal: "terminal"}
		default:
			tokens[role] = theme.Value{Kind: theme.KindColor, Literal: brandColor(name, primary)}
		}
	}
	return theme.Theme{
		ID: id, Version: "v1",
		Modes: map[theme.Mode]theme.TokenSet{
			theme.ModeAuto: tokens,
			theme.ModeDark: {"surface.canvas": {Kind: theme.KindColor, Literal: "#101418"}},
		},
		Densities: map[theme.Density]theme.TokenSet{theme.DensityComfortable: {}},
		Glyphs: map[string]theme.GlyphSet{
			"render": {"truncation": {Unicode: "…", ASCII: "...", Width: 3}},
			"status": {"success": {Unicode: "✓", ASCII: "v", Width: 1}},
		},
		Assets: map[string]theme.AssetRef{
			"brand.mark": {ID: id + ".mark", Text: banner}, "brand.mark.ascii": {ID: id + ".mark.ascii", Text: strings.ToUpper(id[:1])}, "brand.banner.terminal": {ID: id + ".banner", Text: banner},
		},
	}
}

func brandColor(role, primary string) string {
	switch {
	case role == "domain.primary.fg":
		return "#000000"
	case role == "domain.primary.bg", strings.Contains(role, "action.primary.bg"):
		return primary
	case strings.HasSuffix(role, ".fg") && (strings.Contains(role, "action.") || strings.Contains(role, "status.")):
		return "#000000"
	case strings.Contains(role, "action.secondary.bg"), strings.Contains(role, "status.unknown.bg"):
		return "#d7dce2"
	case strings.Contains(role, "action.destructive.bg"), strings.Contains(role, "status.failure.bg"):
		return "#f14c4c"
	case strings.Contains(role, "status.success.bg"):
		return "#35d08f"
	case strings.Contains(role, "status.advisory.bg"):
		return "#f2c94c"
	case strings.Contains(role, "surface"):
		return "#101418"
	case strings.Contains(role, "border"), strings.Contains(role, "focus"):
		return "#aebbc5"
	case strings.Contains(role, "ink.link"), strings.Contains(role, "chart"):
		return "#88b5ff"
	default:
		return "#f2f6f8"
	}
}
