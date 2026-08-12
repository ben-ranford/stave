package theme

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/ben-ranford/stave/capability"
)

func fixtureTheme(id, primary string) Theme {
	auto := TokenSet{}
	light := TokenSet{}
	dark := TokenSet{}
	compact := TokenSet{}
	dense := TokenSet{}
	comfortable := TokenSet{}
	for _, role := range RequiredRoleIDs() {
		auto[role] = tokenLiteral(role, primary)
	}
	light["surface.canvas"] = Value{Kind: KindColor, Literal: "#f7f3ea"}
	dark["surface.canvas"] = Value{Kind: KindColor, Literal: "#0f1417"}
	dark["ink.primary"] = Value{Kind: KindColor, Literal: "#f2f6f8"}
	compact["space.1"] = Value{Kind: KindNumber, Literal: 1}
	compact["space.2"] = Value{Kind: KindNumber, Literal: 2}
	compact["space.3"] = Value{Kind: KindNumber, Literal: 3}
	dense["space.1"] = Value{Kind: KindNumber, Literal: 2}
	dense["space.2"] = Value{Kind: KindNumber, Literal: 4}
	comfortable["space.1"] = Value{Kind: KindNumber, Literal: 4}
	comfortable["space.2"] = Value{Kind: KindNumber, Literal: 8}

	alias := TokenID("ink.primary")
	auto["domain.alias"] = Value{Reference: &alias}

	return Theme{
		ID:      id,
		Version: "v1",
		Modes: map[Mode]TokenSet{
			ModeAuto:  auto,
			ModeLight: light,
			ModeDark:  dark,
		},
		Densities: map[Density]TokenSet{
			DensityCompact:     compact,
			DensityDense:       dense,
			DensityComfortable: comfortable,
		},
		Glyphs: map[string]GlyphSet{
			"status": {
				"success": {Unicode: "✓", ASCII: "v", Name: "success", Width: 1},
				"close":   {Unicode: "×", ASCII: "x", Name: "close", Width: 1},
			},
		},
		Assets: map[string]AssetRef{
			"brand.mark":            {ID: id + ".mark", Text: strings.ToUpper(id)},
			"brand.mark.ascii":      {ID: id + ".mark.ascii", Text: strings.ToUpper(id[:1])},
			"brand.banner.terminal": {ID: id + ".banner", Text: strings.ToUpper(id) + " TERMINAL"},
		},
		Metadata: Metadata{"family": id},
	}
}

func tokenLiteral(role TokenID, primary string) Value {
	name := string(role)
	switch {
	case strings.HasPrefix(name, "motion.duration"):
		return Value{Kind: KindDuration, Literal: "120ms"}
	case strings.HasPrefix(name, "motion.easing"):
		return Value{Kind: KindString, Literal: "ease-out"}
	case strings.HasPrefix(name, "space."), strings.HasPrefix(name, "radius."), strings.HasPrefix(name, "elevation."):
		return Value{Kind: KindNumber, Literal: 4}
	case strings.HasPrefix(name, "type."):
		return Value{Kind: KindString, Literal: "Iosevka"}
	default:
		return Value{Kind: KindColor, Literal: colorForRole(name, primary)}
	}
}

func colorForRole(role, primary string) string {
	switch {
	case role == "domain.primary.fg":
		return "#64c8ff"
	case role == "domain.primary.bg":
		return "#14384b"
	case strings.HasSuffix(role, ".fg") && (strings.Contains(role, "action.") || strings.Contains(role, "status.")):
		return "#000000"
	case strings.HasSuffix(role, ".bg") && strings.Contains(role, "action.primary"):
		return primary
	case strings.HasSuffix(role, ".bg") && strings.Contains(role, "action.secondary"):
		return "#d7dce2"
	case strings.HasSuffix(role, ".bg") && strings.Contains(role, "action.destructive"):
		return "#f14c4c"
	case strings.Contains(role, "failure"):
		return "#f14c4c"
	case strings.Contains(role, "success"):
		return "#35d08f"
	case strings.Contains(role, "advisory"):
		return "#f2c94c"
	case strings.Contains(role, "unknown"):
		return "#d7dce2"
	case strings.Contains(role, "surface"):
		return "#20262b"
	case strings.Contains(role, "border"):
		return "#68727c"
	case strings.Contains(role, "chart"), strings.Contains(role, "action.primary"), strings.Contains(role, "domain.primary"):
		return primary
	case strings.Contains(role, "ink.primary"):
		return "#f2f6f8"
	case strings.Contains(role, "terminal"):
		return "#ffffff"
	default:
		return "#d7dce2"
	}
}

func TestResolveReferencesAndCycle(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	r, err := tm.Resolve(ModeDark, DensityDense, capability.Manifest{Color: capability.ColorANSI16, Unicode: capability.UnicodeFull})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Token("domain.alias"); !ok {
		t.Fatal("alias missing")
	}

	cycle := TokenID("domain.loop")
	tm.Modes[ModeAuto]["domain.loop"] = Value{Reference: &cycle}
	if _, err = tm.Resolve(ModeDark, DensityDense, capability.Manifest{}); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestRoleResolutionAcrossTwoThemes(t *testing.T) {
	left := fixtureTheme("sapling", "#23836d")
	right := fixtureTheme("ember", "#d86a2b")
	manifest := capability.Manifest{Color: capability.ColorTrueColor, Unicode: capability.UnicodeFull}

	lr, err := left.Resolve(ModeDark, DensityComfortable, manifest)
	if err != nil {
		t.Fatal(err)
	}
	rr, err := right.Resolve(ModeDark, DensityComfortable, manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range RequiredRoleIDs() {
		if _, ok := lr.Token(role); !ok {
			t.Fatalf("left theme missing role %s", role)
		}
		if _, ok := rr.Token(role); !ok {
			t.Fatalf("right theme missing role %s", role)
		}
	}
	if lr.HashString() == rr.HashString() {
		t.Fatal("distinct fixtures should remain visibly distinct")
	}
}

func TestModeAndCapabilityOverrides(t *testing.T) {
	tm := fixtureTheme("acorn", "#1795a8")
	light, err := tm.Resolve(ModeAuto, DensityComfortable, capability.Manifest{Color: capability.ColorNone, Unicode: capability.UnicodeASCII})
	if err != nil {
		t.Fatal(err)
	}
	dark, err := tm.Resolve(ModeAuto, DensityComfortable, capability.Manifest{Color: capability.ColorTrueColor, Unicode: capability.UnicodeFull})
	if err != nil {
		t.Fatal(err)
	}
	if light.Mode != ModeLight || dark.Mode != ModeDark {
		t.Fatalf("unexpected mode policy: light=%s dark=%s", light.Mode, dark.Mode)
	}
	asset, _ := light.Asset("brand.mark")
	if got := asset.Text; got != "A" {
		t.Fatalf("ASCII asset fallback failed: %q", got)
	}
	if got, _ := dark.Token("chart.series.1"); got.Value != "#1795a8" {
		t.Fatalf("truecolor ceiling not preserved: %+v", got)
	}
}

func TestReducedMotionZeroesDurations(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	got, err := tm.Resolve(ModeDark, DensityDense, capability.Manifest{Color: capability.ColorTrueColor, ReducedMotion: true})
	if err != nil {
		t.Fatal(err)
	}
	if duration, _ := got.Token("motion.duration.fast"); duration.Value != "0ms" {
		t.Fatalf("duration not zeroed: %+v", duration)
	}
	if easing, _ := got.Token("motion.easing.standard"); easing.Value != "none" {
		t.Fatalf("easing not disabled: %+v", easing)
	}
}

func TestAssetAndGlyphFallbackSelection(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	got, err := tm.Resolve(ModeDark, DensityDense, capability.Manifest{Color: capability.ColorANSI16, Unicode: capability.UnicodeASCII})
	if err != nil {
		t.Fatal(err)
	}
	glyph, _ := got.Glyph("status.close")
	if glyph.Unicode != "x" {
		t.Fatalf("ASCII glyph fallback failed: %+v", glyph)
	}
	asset, _ := got.Asset("brand.mark")
	if asset.ID != "acorn.mark.ascii" {
		t.Fatalf("ASCII asset fallback failed: %+v", asset)
	}
}

func TestThemeRejectsMissingASCIIFallbacksAndInvalidLiterals(t *testing.T) {
	tm := fixtureTheme("broken", "#0b8d80")
	tm.Glyphs["status"]["close"] = Glyph{Unicode: "×", Width: 1}
	if _, err := tm.Resolve(ModeDark, DensityDense, capability.Manifest{Color: capability.ColorTrueColor, Unicode: capability.UnicodeASCII}); err == nil {
		t.Fatal("missing ascii glyph fallback accepted")
	}

	tm = fixtureTheme("broken", "#0b8d80")
	tm.Modes[ModeAuto]["ink.primary"] = Value{Kind: KindColor, Literal: "ansi16:12"}
	if err := tm.Validate(); err == nil {
		t.Fatal("invalid ansi16 literal accepted")
	}

	tm = fixtureTheme("broken", "#0b8d80")
	tm.Assets["brand.mark.ascii"] = AssetRef{ID: "broken.mark.ascii", Text: "µ"}
	if err := tm.Validate(); err == nil {
		t.Fatal("non-ascii asset fallback accepted")
	}
}

func TestQuantizeLadder(t *testing.T) {
	want := map[capability.ColorLevel]string{
		capability.ColorTrueColor:  "#123456",
		capability.ColorANSI256:    "ansi256:24",
		capability.ColorANSI16:     "ansi16:90",
		capability.ColorMonochrome: "mono:dark",
		capability.ColorNone:       "",
	}
	for level, expected := range want {
		if got := Quantize("#123456", level); got != expected {
			t.Fatalf("level %v => %q, want %q", level, got, expected)
		}
	}
}

func TestSemanticContrastAcrossColourLadder(t *testing.T) {
	tm := fixtureTheme("acorn", "#64c8ff")
	for _, level := range []capability.ColorLevel{capability.ColorTrueColor, capability.ColorANSI256, capability.ColorANSI16} {
		resolved, err := tm.Resolve(ModeDark, DensityComfortable, capability.Manifest{Color: level, Unicode: capability.UnicodeFull})
		if err != nil {
			t.Fatalf("resolve %s: %v", level, err)
		}
		if err := resolved.ValidateContrast(MinimumSemanticContrast); err != nil {
			t.Fatalf("%s contrast: %v", level, err)
		}
	}
}

func TestSemanticContrastRejectsUnreadablePair(t *testing.T) {
	tm := fixtureTheme("broken", "#64c8ff")
	tm.Modes[ModeAuto]["action.primary.fg"] = Value{Kind: KindColor, Literal: "#202020"}
	tm.Modes[ModeAuto]["action.primary.bg"] = Value{Kind: KindColor, Literal: "#222222"}
	if _, err := tm.Resolve(ModeDark, DensityComfortable, capability.Manifest{Color: capability.ColorTrueColor, Unicode: capability.UnicodeFull}); err == nil || !strings.Contains(err.Error(), "contrast pair action.primary.fg/action.primary.bg") {
		t.Fatalf("low-contrast pair was not rejected: %v", err)
	}
}

func TestPolicyInventoriesAreDefensiveCopies(t *testing.T) {
	roles := RequiredRoleIDs()
	pairs := ContrastPairs()
	roles[0] = "mutated"
	pairs[0].Foreground = "mutated"
	if RequiredRoleIDs()[0] == "mutated" || ContrastPairs()[0].Foreground == "mutated" {
		t.Fatal("theme policy inventory exposed mutable global storage")
	}
}

func TestThemeConcurrentResolveImmutable(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	before, err := tm.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			manifest := capability.Manifest{Color: capability.ColorANSI256, Unicode: capability.UnicodeFull, ReducedMotion: i%2 == 0}
			resolved, err := tm.Resolve(ModeAuto, DensityComfortable, manifest)
			if err != nil {
				t.Errorf("resolve failed: %v", err)
				return
			}
			if len(resolved.StableJSON()) == 0 {
				t.Error("resolved JSON empty")
			}
		}(i)
	}
	wg.Wait()

	after, err := tm.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("theme mutated during concurrent resolves")
	}
}

func TestThemeJSONRoundTripAndCanonicalHash(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	data, err := tm.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if tm.HashString() != roundTrip.HashString() {
		t.Fatalf("hash mismatch: %s != %s", tm.HashString(), roundTrip.HashString())
	}
}

func TestResolvedCollectionsAreImmutableSnapshots(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	resolved, err := tm.Resolve(ModeDark, DensityComfortable, capability.Manifest{Color: capability.ColorTrueColor, Unicode: capability.UnicodeFull})
	if err != nil {
		t.Fatal(err)
	}
	before := resolved.HashString()

	tokens := resolved.Tokens()
	tokens["ink.primary"] = ResolvedValue{Kind: KindColor, Value: "#ffffff"}
	glyphs := resolved.Glyphs()
	glyphs["status.close"] = Glyph{Unicode: "!", ASCII: "!", Width: 1}
	assets := resolved.Assets()
	assets["brand.mark"] = AssetRef{ID: "mutated", Text: "MUTATED"}

	if got, _ := resolved.Token("ink.primary"); got.Value == "#ffffff" {
		t.Fatal("token accessor leaked mutable storage")
	}
	if got, _ := resolved.Glyph("status.close"); got.Unicode == "!" {
		t.Fatal("glyph accessor leaked mutable storage")
	}
	if got, _ := resolved.Asset("brand.mark"); got.ID == "mutated" {
		t.Fatal("asset accessor leaked mutable storage")
	}
	if !resolved.Valid() || resolved.HashString() != before {
		t.Fatal("resolved theme mutation changed its frozen hash")
	}
}

func TestFromJSONRejectsTrailingValues(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	data, err := tm.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(` {"second":true}`)...)
	if _, err := FromJSON(data); err == nil {
		t.Fatal("trailing JSON value accepted")
	}
}

func TestThemeDTCGRoundTrip(t *testing.T) {
	tm := fixtureTheme("acorn", "#0b8d80")
	data, err := tm.ToDTCG()
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := FromDTCG(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{ModeAuto, ModeLight, ModeDark} {
		if _, ok := roundTrip.Modes[mode]; !ok {
			t.Fatalf("roundtrip missing mode %s", mode)
		}
	}
	if !strings.Contains(roundTrip.CanonicalVersion(), "1") {
		t.Fatalf("unexpected DTCG version: %s", roundTrip.CanonicalVersion())
	}
}

func TestThemeMissingRequiredTokensFails(t *testing.T) {
	tm := fixtureTheme("broken", "#0b8d80")
	delete(tm.Modes[ModeAuto], "ink.primary")
	if _, err := tm.Resolve(ModeDark, DensityComfortable, capability.Manifest{}); err == nil {
		t.Fatal("missing required token accepted")
	}
}

func TestValidateColorLiteralCoversResolvedModes(t *testing.T) {
	cases := []string{"#123456", "ansi256:42", "ansi16:96", "mono:dark", "none"}
	for _, literal := range cases {
		tm := fixtureTheme("palette", "#0b8d80")
		tm.Modes[ModeAuto]["ink.primary"] = Value{Kind: KindColor, Literal: literal}
		if err := tm.Validate(); err != nil {
			t.Fatalf("%s rejected: %v", literal, err)
		}
	}
}
