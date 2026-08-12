package theme

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ben-ranford/stave/capability"
	itokens "github.com/ben-ranford/stave/internal/tokens"
	"github.com/ben-ranford/stave/internal/width"
)

type TokenID string
type Mode string
type Density string

const (
	ModeAuto  Mode = "auto"
	ModeLight Mode = "light"
	ModeDark  Mode = "dark"

	DensityCompact     Density = "compact"
	DensityDense       Density = "dense"
	DensityComfortable Density = "comfortable"
)

type Kind string

const (
	KindString   Kind = "string"
	KindNumber   Kind = "number"
	KindColor    Kind = "color"
	KindDuration Kind = "duration"
	KindBool     Kind = "bool"
)

type Value struct {
	Kind      Kind     `json:"kind,omitempty"`
	Literal   any      `json:"value,omitempty"`
	Reference *TokenID `json:"$ref,omitempty"`
}

type TokenSet map[TokenID]Value

type Glyph struct {
	Unicode string `json:"unicode,omitempty"`
	ASCII   string `json:"ascii,omitempty"`
	Name    string `json:"name,omitempty"`
	Width   int    `json:"width,omitempty"`
}

type GlyphSet map[string]Glyph

type AssetRef struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
}

type Metadata map[string]string

type Theme struct {
	ID, Version string
	Modes       map[Mode]TokenSet
	Densities   map[Density]TokenSet
	Glyphs      map[string]GlyphSet
	Assets      map[string]AssetRef
	Metadata    Metadata
}

type ResolvedValue struct {
	Value any  `json:"value"`
	Kind  Kind `json:"kind"`
}

type Resolved struct {
	ThemeID, Version string
	Mode             Mode
	Density          Density
	tokens           map[TokenID]ResolvedValue
	glyphs           GlyphSet
	assets           map[string]AssetRef
	Hash             [32]byte
}

var requiredRoles = [...]TokenID{
	"surface.canvas", "surface.raised", "surface.overlay", "surface.inset", "surface.hover", "surface.selected",
	"ink.primary", "ink.secondary", "ink.tertiary", "ink.inverse", "ink.link", "ink.code",
	"border.subtle", "border.default", "border.strong", "border.focus",
	"action.primary.fg", "action.primary.bg", "action.primary.border",
	"action.secondary.fg", "action.secondary.bg", "action.secondary.border",
	"action.destructive.fg", "action.destructive.bg", "action.destructive.border",
	"status.success.fg", "status.success.bg", "status.success.border",
	"status.advisory.fg", "status.advisory.bg", "status.advisory.border",
	"status.failure.fg", "status.failure.bg", "status.failure.border",
	"status.unknown.fg", "status.unknown.bg", "status.unknown.border",
	"domain.primary.fg", "domain.primary.bg",
	"chart.series.1", "chart.axis",
	"type.display", "type.heading", "type.body", "type.label", "type.micro", "type.code",
	"space.1", "space.2", "space.3",
	"radius.sm", "radius.md",
	"motion.duration.fast", "motion.duration.slow", "motion.easing.standard",
	"elevation.1", "terminal.cursor", "terminal.selection",
	"table.header", "form.label", "overlay.backdrop", "progress.track", "focus.ring",
}

var requiredAssets = []string{"brand.mark", "brand.mark.ascii", "brand.banner.terminal"}

const MinimumSemanticContrast = 3.0

type ContrastPair struct {
	Foreground TokenID
	Background TokenID
}

var requiredContrastPairs = [...]ContrastPair{
	{"ink.primary", "surface.canvas"},
	{"ink.secondary", "surface.canvas"},
	{"ink.tertiary", "surface.canvas"},
	{"ink.inverse", "surface.raised"},
	{"ink.link", "surface.canvas"},
	{"ink.code", "surface.inset"},
	{"ink.primary", "surface.overlay"},
	{"ink.primary", "surface.inset"},
	{"ink.primary", "surface.hover"},
	{"ink.primary", "surface.selected"},
	{"action.primary.fg", "action.primary.bg"},
	{"action.secondary.fg", "action.secondary.bg"},
	{"action.destructive.fg", "action.destructive.bg"},
	{"status.success.fg", "status.success.bg"},
	{"status.advisory.fg", "status.advisory.bg"},
	{"status.failure.fg", "status.failure.bg"},
	{"status.unknown.fg", "status.unknown.bg"},
	{"domain.primary.fg", "domain.primary.bg"},
	{"table.header", "surface.canvas"},
	{"form.label", "surface.canvas"},
}

func RequiredRoleIDs() []TokenID { return append([]TokenID(nil), requiredRoles[:]...) }
func ContrastPairs() []ContrastPair {
	return append([]ContrastPair(nil), requiredContrastPairs[:]...)
}

func (t Theme) Resolve(mode Mode, density Density, c capability.Manifest) (Resolved, error) {
	if err := t.Validate(); err != nil {
		return Resolved{}, err
	}

	selectedMode := chooseMode(t, mode, c)
	selectedDensity := chooseDensity(t, density)
	base := merge(merge(cloneTokenSet(t.Modes[ModeAuto]), cloneTokenSet(t.Modes[selectedMode])), cloneTokenSet(t.Densities[selectedDensity]))
	if base == nil {
		base = TokenSet{}
	}

	out := Resolved{
		ThemeID: t.ID,
		Version: t.CanonicalVersion(),
		Mode:    selectedMode,
		Density: selectedDensity,
		tokens:  make(map[TokenID]ResolvedValue, len(base)),
		glyphs:  make(GlyphSet),
		assets:  make(map[string]AssetRef, len(t.Assets)),
	}

	keys := make([]string, 0, len(base))
	for key := range base {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	for _, rawKey := range keys {
		key := TokenID(rawKey)
		v, err := resolve(key, base, map[TokenID]bool{})
		if err != nil {
			return Resolved{}, err
		}
		if v.Kind == KindColor {
			if s, ok := v.Value.(string); ok {
				v.Value = Quantize(s, c.Color)
			}
		}
		out.tokens[key] = v
	}

	for groupName, glyphs := range t.Glyphs {
		for role, glyph := range glyphs {
			key := groupName + "." + role
			switch c.Unicode {
			case capability.UnicodeNone, capability.UnicodeASCII:
				if glyph.ASCII == "" {
					return Resolved{}, fmt.Errorf("glyph %s requires ascii fallback", key)
				}
				out.glyphs[key] = Glyph{ASCII: glyph.ASCII, Unicode: glyph.ASCII, Name: glyph.Name, Width: resolvedGlyphWidth(glyph)}
			default:
				if glyph.Unicode != "" {
					glyph.Width = resolvedGlyphWidth(glyph)
					out.glyphs[key] = glyph
				} else {
					out.glyphs[key] = Glyph{ASCII: glyph.ASCII, Unicode: glyph.ASCII, Name: glyph.Name, Width: resolvedGlyphWidth(glyph)}
				}
			}
		}
	}

	for name, asset := range t.Assets {
		out.assets[name] = asset
	}
	if c.Unicode == capability.UnicodeASCII || c.Unicode == capability.UnicodeNone {
		for name := range out.assets {
			if ascii, ok := out.assets[name+".ascii"]; ok {
				out.assets[name] = ascii
				continue
			}
			if strings.HasSuffix(name, ".ascii") {
				continue
			}
			if !isASCIIText(out.assets[name].Text) {
				return Resolved{}, fmt.Errorf("asset %s requires ascii fallback", name)
			}
		}
	}
	if c.ReducedMotion {
		for key, v := range out.tokens {
			switch {
			case strings.HasPrefix(string(key), "motion.duration"):
				v.Value = "0ms"
				out.tokens[key] = v
			case strings.HasPrefix(string(key), "motion.easing"):
				v.Value = "none"
				out.tokens[key] = v
			}
		}
	}
	if c.Color == capability.ColorTrueColor || c.Color == capability.ColorANSI256 || c.Color == capability.ColorANSI16 {
		if err := out.ValidateContrast(MinimumSemanticContrast); err != nil {
			return Resolved{}, err
		}
	}

	hash, err := itokens.CanonicalHash(out.canonicalView())
	if err != nil {
		return Resolved{}, err
	}
	out.Hash = hash
	return out, nil
}

func (t Theme) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("theme id is required")
	}
	if len(t.Modes) == 0 {
		return fmt.Errorf("theme requires at least one mode token set")
	}
	if len(t.Densities) == 0 {
		return fmt.Errorf("theme requires at least one density token set")
	}
	for mode, set := range t.Modes {
		if !validMode(mode) {
			return fmt.Errorf("unsupported mode: %s", mode)
		}
		if err := validateTokenSet("mode "+string(mode), set); err != nil {
			return err
		}
	}
	for density, set := range t.Densities {
		if !validDensity(density) {
			return fmt.Errorf("unsupported density: %s", density)
		}
		if err := validateTokenSet("density "+string(density), set); err != nil {
			return err
		}
	}
	for _, assetID := range requiredAssets {
		if _, ok := t.Assets[assetID]; !ok {
			return fmt.Errorf("missing required asset: %s", assetID)
		}
	}
	for _, role := range requiredRoles {
		if _, err := resolve(role, merge(merge(cloneTokenSet(t.Modes[ModeAuto]), cloneTokenSet(preferredModeSet(t))), cloneTokenSet(preferredDensitySet(t))), map[TokenID]bool{}); err != nil {
			return fmt.Errorf("required role %s: %w", role, err)
		}
	}
	for mode, set := range t.Modes {
		for key := range set {
			if _, err := resolve(key, merge(merge(cloneTokenSet(t.Modes[ModeAuto]), cloneTokenSet(set)), cloneTokenSet(preferredDensitySet(t))), map[TokenID]bool{}); err != nil {
				return fmt.Errorf("mode %s token %s: %w", mode, key, err)
			}
		}
	}
	for density, set := range t.Densities {
		for key := range set {
			if _, err := resolve(key, merge(merge(cloneTokenSet(t.Modes[ModeAuto]), cloneTokenSet(preferredModeSet(t))), cloneTokenSet(set)), map[TokenID]bool{}); err != nil {
				return fmt.Errorf("density %s token %s: %w", density, key, err)
			}
		}
	}
	for group, glyphs := range t.Glyphs {
		for name, glyph := range glyphs {
			key := group + "." + name
			if err := validateGlyph(key, glyph); err != nil {
				return err
			}
		}
	}
	for name, asset := range t.Assets {
		if err := validateAsset(name, asset, t.Assets); err != nil {
			return err
		}
	}
	return nil
}

func (t Theme) CanonicalJSON() ([]byte, error) {
	return itokens.CanonicalJSON(t.canonicalView())
}

func (t Theme) Hash() [32]byte {
	sum, err := itokens.CanonicalHash(t.canonicalView())
	if err != nil {
		return [32]byte{}
	}
	return sum
}

func (t Theme) HashString() string {
	sum := t.Hash()
	return hex.EncodeToString(sum[:])
}

func (t Theme) CanonicalVersion() string {
	if strings.TrimSpace(t.Version) != "" {
		return t.Version
	}
	hash := t.HashString()
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return "theme-auto-" + hash
}

func (r Resolved) Token(id TokenID) (ResolvedValue, bool) {
	v, ok := r.tokens[id]
	return v, ok
}

func (r Resolved) Tokens() map[TokenID]ResolvedValue { return cloneResolvedTokens(r.tokens) }
func (r Resolved) Glyph(id string) (Glyph, bool)     { v, ok := r.glyphs[id]; return v, ok }
func (r Resolved) Glyphs() GlyphSet                  { return cloneGlyphSet(r.glyphs) }
func (r Resolved) Asset(id string) (AssetRef, bool)  { v, ok := r.assets[id]; return v, ok }
func (r Resolved) Assets() map[string]AssetRef       { return cloneAssets(r.assets) }

// Valid detects zero values and any internal hash inconsistency. Resolved is
// immutable after construction because all collection accessors return clones.
func (r Resolved) Valid() bool {
	if strings.TrimSpace(r.ThemeID) == "" || strings.TrimSpace(r.Version) == "" || len(r.tokens) == 0 || len(r.assets) == 0 {
		return false
	}
	sum, err := itokens.CanonicalHash(r.canonicalView())
	return err == nil && sum == r.Hash
}

// ContrastRatio reports the WCAG relative-luminance contrast ratio for two
// resolved terminal colours. It understands Stave truecolour, ANSI-256 and
// ANSI-16 literals after capability quantization.
func ContrastRatio(foreground, background string) (float64, error) {
	fr, fg, fb, ok := terminalRGB(foreground)
	if !ok {
		return 0, fmt.Errorf("unsupported foreground colour %q", foreground)
	}
	br, bg, bb, ok := terminalRGB(background)
	if !ok {
		return 0, fmt.Errorf("unsupported background colour %q", background)
	}
	fl := relativeLuminance(fr, fg, fb)
	bl := relativeLuminance(br, bg, bb)
	if fl < bl {
		fl, bl = bl, fl
	}
	return (fl + 0.05) / (bl + 0.05), nil
}

func (r Resolved) ValidateContrast(minimum float64) error {
	if minimum <= 1 {
		return fmt.Errorf("minimum contrast ratio must exceed 1")
	}
	for _, pair := range requiredContrastPairs {
		fg, fgOK := r.Token(pair.Foreground)
		bg, bgOK := r.Token(pair.Background)
		if !fgOK || !bgOK {
			return fmt.Errorf("contrast pair %s/%s is incomplete", pair.Foreground, pair.Background)
		}
		fgText, fgOK := fg.Value.(string)
		bgText, bgOK := bg.Value.(string)
		if !fgOK || !bgOK {
			return fmt.Errorf("contrast pair %s/%s must resolve to colours", pair.Foreground, pair.Background)
		}
		ratio, err := ContrastRatio(fgText, bgText)
		if err != nil {
			return fmt.Errorf("contrast pair %s/%s: %w", pair.Foreground, pair.Background, err)
		}
		if ratio+1e-9 < minimum {
			return fmt.Errorf("contrast pair %s/%s ratio %.2f is below %.2f", pair.Foreground, pair.Background, ratio, minimum)
		}
	}
	return nil
}

func (r Resolved) StableJSON() []byte {
	b, _ := itokens.CanonicalJSON(r.canonicalView())
	return b
}

func (r Resolved) HashString() string { return hex.EncodeToString(r.Hash[:]) }

func (t Theme) ToJSON() ([]byte, error) {
	return t.CanonicalJSON()
}

func (t Theme) ToDTCG() ([]byte, error) {
	doc := map[string]any{
		"modes":     map[string]any{},
		"densities": map[string]any{},
	}
	modes := doc["modes"].(map[string]any)
	modeNames := make([]string, 0, len(t.Modes))
	for mode := range t.Modes {
		modeNames = append(modeNames, string(mode))
	}
	sort.Strings(modeNames)
	for _, rawMode := range modeNames {
		mode := Mode(rawMode)
		modes[rawMode] = tokenSetToDTCGMap(t.Modes[mode])
	}
	densities := doc["densities"].(map[string]any)
	densityNames := make([]string, 0, len(t.Densities))
	for density := range t.Densities {
		densityNames = append(densityNames, string(density))
	}
	sort.Strings(densityNames)
	for _, rawDensity := range densityNames {
		density := Density(rawDensity)
		densities[rawDensity] = tokenSetToDTCGMap(t.Densities[density])
	}
	return itokens.CanonicalJSON(doc)
}

func FromJSON(data []byte) (Theme, error) {
	var raw struct {
		ID        string               `json:"id"`
		Version   string               `json:"version"`
		Modes     map[Mode]TokenSet    `json:"modes"`
		Densities map[Density]TokenSet `json:"densities"`
		Glyphs    map[string]GlyphSet  `json:"glyphs"`
		Assets    map[string]AssetRef  `json:"assets"`
		Metadata  Metadata             `json:"metadata"`
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Theme{}, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Theme{}, fmt.Errorf("theme JSON must contain exactly one object")
	}
	theme := Theme{
		ID:        raw.ID,
		Version:   raw.Version,
		Modes:     cloneModes(raw.Modes),
		Densities: cloneDensities(raw.Densities),
		Glyphs:    cloneGlyphs(raw.Glyphs),
		Assets:    cloneAssets(raw.Assets),
		Metadata:  cloneMetadata(raw.Metadata),
	}
	return theme, theme.Validate()
}

func FromDTCG(data []byte) (Theme, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Theme{}, err
	}
	theme := Theme{
		ID:        "dtcg",
		Version:   "1",
		Modes:     map[Mode]TokenSet{ModeAuto: {}},
		Densities: map[Density]TokenSet{DensityComfortable: {}},
		Assets: map[string]AssetRef{
			"brand.mark":            {ID: "brand.mark", Text: "DTCG"},
			"brand.mark.ascii":      {ID: "brand.mark.ascii", Text: "DTCG"},
			"brand.banner.terminal": {ID: "brand.banner.terminal", Text: "DTCG"},
		},
	}

	if modesRaw, ok := raw["modes"]; ok {
		var modes map[string]map[string]any
		if err := json.Unmarshal(modesRaw, &modes); err != nil {
			return Theme{}, err
		}
		theme.Modes = map[Mode]TokenSet{}
		for name, payload := range modes {
			set, err := dtcgMapToTokenSet(payload)
			if err != nil {
				return Theme{}, err
			}
			theme.Modes[Mode(name)] = set
		}
	}
	if densitiesRaw, ok := raw["densities"]; ok {
		var densities map[string]map[string]any
		if err := json.Unmarshal(densitiesRaw, &densities); err != nil {
			return Theme{}, err
		}
		theme.Densities = map[Density]TokenSet{}
		for name, payload := range densities {
			set, err := dtcgMapToTokenSet(payload)
			if err != nil {
				return Theme{}, err
			}
			theme.Densities[Density(name)] = set
		}
	}
	if _, ok := raw["modes"]; !ok {
		set, err := dtcgFromRoot(raw)
		if err != nil {
			return Theme{}, err
		}
		theme.Modes = map[Mode]TokenSet{ModeAuto: set}
	}

	return theme, theme.Validate()
}

func merge(a, b TokenSet) TokenSet {
	if a == nil && b == nil {
		return nil
	}
	out := cloneTokenSet(a)
	for key, value := range b {
		out[key] = value
	}
	return out
}

func resolve(k TokenID, set TokenSet, seen map[TokenID]bool) (ResolvedValue, error) {
	if seen[k] {
		return ResolvedValue{}, fmt.Errorf("cyclic token reference: %s", k)
	}
	v, ok := set[k]
	if !ok {
		return ResolvedValue{}, fmt.Errorf("missing token: %s", k)
	}
	if v.Reference != nil {
		seen[k] = true
		r, err := resolve(*v.Reference, set, seen)
		delete(seen, k)
		return r, err
	}
	return ResolvedValue{Value: v.Literal, Kind: v.Kind}, nil
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeAuto, ModeLight, ModeDark:
		return true
	default:
		return false
	}
}

func validDensity(density Density) bool {
	switch density {
	case DensityCompact, DensityDense, DensityComfortable:
		return true
	default:
		return false
	}
}

func validKind(kind Kind) bool {
	switch kind {
	case KindString, KindNumber, KindColor, KindDuration, KindBool:
		return true
	default:
		return false
	}
}

func validateTokenSet(scope string, set TokenSet) error {
	for key, value := range set {
		if err := validateValue(key, value); err != nil {
			return fmt.Errorf("%s token %s: %w", scope, key, err)
		}
	}
	return nil
}

func validateValue(key TokenID, value Value) error {
	if value.Reference != nil {
		if strings.TrimSpace(string(*value.Reference)) == "" {
			return fmt.Errorf("reference is required")
		}
		if value.Kind != "" || value.Literal != nil {
			return fmt.Errorf("reference tokens must not define literal payload")
		}
		return nil
	}
	if !validKind(value.Kind) {
		return fmt.Errorf("unsupported kind %q", value.Kind)
	}
	switch value.Kind {
	case KindString, KindDuration:
		s, ok := value.Literal.(string)
		if !ok {
			return fmt.Errorf("literal must be a string")
		}
		if containsControl(s) {
			return fmt.Errorf("literal contains control bytes")
		}
	case KindColor:
		s, ok := value.Literal.(string)
		if !ok {
			return fmt.Errorf("literal must be a string")
		}
		if err := validateColorLiteral(s); err != nil {
			return err
		}
	case KindNumber:
		switch value.Literal.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		default:
			return fmt.Errorf("literal must be numeric")
		}
	case KindBool:
		if _, ok := value.Literal.(bool); !ok {
			return fmt.Errorf("literal must be boolean")
		}
	}
	if strings.TrimSpace(string(key)) == "" {
		return fmt.Errorf("token id is required")
	}
	return nil
}

func validateColorLiteral(s string) error {
	color := strings.TrimSpace(strings.ToLower(s))
	if color == "" {
		return fmt.Errorf("color literal is required")
	}
	if containsControl(color) {
		return fmt.Errorf("color literal contains control bytes")
	}
	if _, _, _, ok := rgb(color); ok {
		return nil
	}
	if color == "none" {
		return nil
	}
	if strings.HasPrefix(color, "ansi256:") {
		n, err := strconv.Atoi(strings.TrimPrefix(color, "ansi256:"))
		if err != nil || n < 0 || n > 255 {
			return fmt.Errorf("ansi256 literal out of range")
		}
		return nil
	}
	if strings.HasPrefix(color, "ansi16:") {
		n, err := strconv.Atoi(strings.TrimPrefix(color, "ansi16:"))
		if err != nil {
			return fmt.Errorf("ansi16 literal is invalid")
		}
		switch {
		case n >= 30 && n <= 37:
			return nil
		case n >= 90 && n <= 97:
			return nil
		default:
			return fmt.Errorf("ansi16 literal out of range")
		}
	}
	if color == "mono:dark" || color == "mono:light" {
		return nil
	}
	return fmt.Errorf("unsupported color literal")
}

func validateGlyph(key string, glyph Glyph) error {
	if glyph.Unicode == "" && glyph.ASCII == "" {
		return fmt.Errorf("glyph %s requires unicode or ascii content", key)
	}
	if glyph.ASCII == "" {
		return fmt.Errorf("glyph %s requires ascii content", key)
	}
	if !isASCIIText(glyph.ASCII) {
		return fmt.Errorf("glyph %s ascii content must be printable ASCII", key)
	}
	if containsControl(glyph.ASCII) || containsControl(glyph.Unicode) || containsControl(glyph.Name) {
		return fmt.Errorf("glyph %s contains control bytes", key)
	}
	if glyph.Width > 0 && glyph.Width != width.String(glyph.ASCII) {
		return fmt.Errorf("glyph %s width must match ascii fallback width", key)
	}
	return nil
}

func validateAsset(name string, asset AssetRef, assets map[string]AssetRef) error {
	if strings.TrimSpace(asset.ID) == "" {
		return fmt.Errorf("asset %s id is required", name)
	}
	if !utf8.ValidString(asset.Text) || containsControl(asset.Text) {
		return fmt.Errorf("asset %s contains invalid text", name)
	}
	if strings.HasSuffix(name, ".ascii") {
		if !isASCIIText(asset.Text) {
			return fmt.Errorf("asset %s must be ASCII-safe", name)
		}
		return nil
	}
	if isASCIIText(asset.Text) {
		return nil
	}
	if _, ok := assets[name+".ascii"]; !ok {
		return fmt.Errorf("asset %s requires ascii fallback", name)
	}
	return nil
}

func resolvedGlyphWidth(glyph Glyph) int {
	if glyph.Width > 0 {
		return glyph.Width
	}
	if glyph.ASCII != "" {
		return width.String(glyph.ASCII)
	}
	return width.String(glyph.Unicode)
}

func isASCIIText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r > 0x7f {
			return false
		}
	}
	return true
}

func containsControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func Quantize(hexColor string, level capability.ColorLevel) string {
	r, g, b, ok := rgb(hexColor)
	if !ok {
		return hexColor
	}
	switch level {
	case capability.ColorTrueColor:
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	case capability.ColorANSI256:
		return fmt.Sprintf("ansi256:%d", 16+36*round(r, 255, 5)+6*round(g, 255, 5)+round(b, 255, 5))
	case capability.ColorANSI16:
		return fmt.Sprintf("ansi16:%d", ansi16(r, g, b))
	case capability.ColorMonochrome:
		if luminance(r, g, b) >= 128 {
			return "mono:light"
		}
		return "mono:dark"
	default:
		return ""
	}
}

func rgb(s string) (uint8, uint8, uint8, bool) {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	return uint8(v >> 16), uint8(v >> 8), uint8(v), err == nil
}

func round(v uint8, maxv, steps int) int {
	return int((int(v)*steps + maxv/2) / maxv)
}

func ansi16(r, g, b uint8) int {
	bestCode := 30
	bestDistance := int(^uint(0) >> 1)
	for _, candidate := range ansi16Palette() {
		distance := colorDistance(r, g, b, candidate.r, candidate.g, candidate.b)
		if distance < bestDistance {
			bestDistance = distance
			bestCode = candidate.code
		}
	}
	return bestCode
}

func luminance(r, g, b uint8) int {
	return (299*int(r) + 587*int(g) + 114*int(b)) / 1000
}

func colorDistance(r1, g1, b1, r2, g2, b2 uint8) int {
	dr := int(r1) - int(r2)
	dg := int(g1) - int(g2)
	db := int(b1) - int(b2)
	return dr*dr + dg*dg + db*db
}

func terminalRGB(value string) (uint8, uint8, uint8, bool) {
	if r, g, b, ok := rgb(value); ok {
		return r, g, b, true
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "ansi16:") {
		code, err := strconv.Atoi(strings.TrimPrefix(value, "ansi16:"))
		if err != nil {
			return 0, 0, 0, false
		}
		for _, entry := range ansi16Palette() {
			if entry.code == code {
				return entry.r, entry.g, entry.b, true
			}
		}
		return 0, 0, 0, false
	}
	if strings.HasPrefix(value, "ansi256:") {
		index, err := strconv.Atoi(strings.TrimPrefix(value, "ansi256:"))
		if err != nil || index < 0 || index > 255 {
			return 0, 0, 0, false
		}
		return ansi256RGB(index)
	}
	return 0, 0, 0, false
}

type paletteEntry struct {
	code    int
	r, g, b uint8
}

func ansi16Palette() []paletteEntry {
	return []paletteEntry{
		{30, 0, 0, 0}, {31, 205, 49, 49}, {32, 13, 188, 121}, {33, 229, 229, 16},
		{34, 36, 114, 200}, {35, 188, 63, 188}, {36, 17, 168, 205}, {37, 229, 229, 229},
		{90, 102, 102, 102}, {91, 241, 76, 76}, {92, 35, 209, 139}, {93, 245, 245, 67},
		{94, 59, 142, 234}, {95, 214, 112, 214}, {96, 41, 184, 219}, {97, 255, 255, 255},
	}
}

func ansi256RGB(index int) (uint8, uint8, uint8, bool) {
	if index < 16 {
		codes := []int{30, 31, 32, 33, 34, 35, 36, 37, 90, 91, 92, 93, 94, 95, 96, 97}
		for _, entry := range ansi16Palette() {
			if entry.code == codes[index] {
				return entry.r, entry.g, entry.b, true
			}
		}
	}
	if index >= 232 {
		v := uint8(8 + (index-232)*10)
		return v, v, v, true
	}
	cube := index - 16
	levels := []uint8{0, 95, 135, 175, 215, 255}
	return levels[cube/36], levels[(cube/6)%6], levels[cube%6], true
}

func relativeLuminance(r, g, b uint8) float64 {
	linear := func(component uint8) float64 {
		value := float64(component) / 255
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

func chooseMode(t Theme, requested Mode, c capability.Manifest) Mode {
	if requested != "" && requested != ModeAuto {
		if _, ok := t.Modes[requested]; ok {
			return requested
		}
	}
	if c.Color == capability.ColorNone || c.ScreenReader {
		if _, ok := t.Modes[ModeLight]; ok {
			return ModeLight
		}
	}
	if _, ok := t.Modes[ModeDark]; ok {
		return ModeDark
	}
	if _, ok := t.Modes[ModeLight]; ok {
		return ModeLight
	}
	if _, ok := t.Modes[ModeAuto]; ok {
		return ModeAuto
	}
	names := make([]string, 0, len(t.Modes))
	for mode := range t.Modes {
		names = append(names, string(mode))
	}
	sort.Strings(names)
	return Mode(names[0])
}

func chooseDensity(t Theme, requested Density) Density {
	if requested != "" {
		if _, ok := t.Densities[requested]; ok {
			return requested
		}
	}
	if _, ok := t.Densities[DensityComfortable]; ok {
		return DensityComfortable
	}
	if _, ok := t.Densities[DensityDense]; ok {
		return DensityDense
	}
	if _, ok := t.Densities[DensityCompact]; ok {
		return DensityCompact
	}
	names := make([]string, 0, len(t.Densities))
	for density := range t.Densities {
		names = append(names, string(density))
	}
	sort.Strings(names)
	return Density(names[0])
}

func preferredModeSet(t Theme) TokenSet {
	return t.Modes[chooseMode(t, ModeAuto, capability.Manifest{Color: capability.ColorTrueColor, Unicode: capability.UnicodeFull})]
}

func preferredDensitySet(t Theme) TokenSet {
	return t.Densities[chooseDensity(t, DensityComfortable)]
}

func cloneTokenSet(in TokenSet) TokenSet {
	if in == nil {
		return nil
	}
	out := make(TokenSet, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneModes(in map[Mode]TokenSet) map[Mode]TokenSet {
	if in == nil {
		return nil
	}
	out := make(map[Mode]TokenSet, len(in))
	for key, value := range in {
		out[key] = cloneTokenSet(value)
	}
	return out
}

func cloneDensities(in map[Density]TokenSet) map[Density]TokenSet {
	if in == nil {
		return nil
	}
	out := make(map[Density]TokenSet, len(in))
	for key, value := range in {
		out[key] = cloneTokenSet(value)
	}
	return out
}

func cloneGlyphs(in map[string]GlyphSet) map[string]GlyphSet {
	if in == nil {
		return nil
	}
	out := make(map[string]GlyphSet, len(in))
	for key, set := range in {
		next := make(GlyphSet, len(set))
		for name, glyph := range set {
			next[name] = glyph
		}
		out[key] = next
	}
	return out
}

func cloneAssets(in map[string]AssetRef) map[string]AssetRef {
	if in == nil {
		return nil
	}
	out := make(map[string]AssetRef, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneMetadata(in Metadata) Metadata {
	if in == nil {
		return nil
	}
	out := make(Metadata, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (t Theme) canonicalView() map[string]any {
	return map[string]any{
		"id":        t.ID,
		"version":   t.Version,
		"modes":     cloneModes(t.Modes),
		"densities": cloneDensities(t.Densities),
		"glyphs":    cloneGlyphs(t.Glyphs),
		"assets":    cloneAssets(t.Assets),
		"metadata":  cloneMetadata(t.Metadata),
	}
}

func (r Resolved) canonicalView() map[string]any {
	return map[string]any{
		"themeId": r.ThemeID,
		"version": r.Version,
		"mode":    r.Mode,
		"density": r.Density,
		"tokens":  r.tokens,
		"glyphs":  r.glyphs,
		"assets":  r.assets,
	}
}

func cloneResolvedTokens(in map[TokenID]ResolvedValue) map[TokenID]ResolvedValue {
	out := make(map[TokenID]ResolvedValue, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneGlyphSet(in GlyphSet) GlyphSet {
	out := make(GlyphSet, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func tokenSetToDTCGMap(set TokenSet) map[string]any {
	out := map[string]any{}
	for key, value := range set {
		assignDTCGToken(out, strings.Split(string(key), "."), value)
	}
	return out
}

func assignDTCGToken(dst map[string]any, path []string, value Value) {
	if len(path) == 1 {
		entry := map[string]any{}
		if value.Reference != nil {
			entry["$ref"] = strings.ReplaceAll(string(*value.Reference), ".", "/")
		} else {
			entry["$value"] = value.Literal
			if value.Kind != "" {
				entry["$type"] = string(value.Kind)
			}
		}
		dst[path[0]] = entry
		return
	}
	child, _ := dst[path[0]].(map[string]any)
	if child == nil {
		child = map[string]any{}
		dst[path[0]] = child
	}
	assignDTCGToken(child, path[1:], value)
}

func dtcgFromRoot(raw map[string]json.RawMessage) (TokenSet, error) {
	flattened := map[string]any{}
	for key, value := range raw {
		var parsed any
		if err := json.Unmarshal(value, &parsed); err != nil {
			return nil, err
		}
		flattened[key] = parsed
	}
	return dtcgMapToTokenSet(flattened)
}

func dtcgMapToTokenSet(raw map[string]any) (TokenSet, error) {
	out := TokenSet{}
	if err := walkDTCG("", raw, out); err != nil {
		return nil, err
	}
	return out, nil
}

func walkDTCG(prefix string, current map[string]any, out TokenSet) error {
	for key, value := range current {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := node["$ref"].(string); ok {
			id := TokenID(strings.ReplaceAll(ref, "/", "."))
			out[TokenID(path)] = Value{Reference: &id}
			continue
		}
		if literal, ok := node["$value"]; ok {
			kind := KindString
			if kindValue, ok := node["$type"].(string); ok {
				kind = Kind(kindValue)
			}
			out[TokenID(path)] = Value{Kind: kind, Literal: literal}
			continue
		}
		if err := walkDTCG(path, node, out); err != nil {
			return err
		}
	}
	return nil
}
