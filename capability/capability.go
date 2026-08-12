// Package capability models explicit terminal/client capability negotiation.
package capability

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ID string
type OutputMode string
type Profile string

const (
	OutputAuto           OutputMode = "auto"
	OutputMachineJSON    OutputMode = "machine-json"
	OutputMachineJSONL   OutputMode = "machine-jsonl"
	OutputPlain          OutputMode = "plain"
	OutputDumb           OutputMode = "dumb"
	OutputAccessibleLine OutputMode = "accessible-line"
)

const (
	ProfileMachineJSON    Profile = "machine-json"
	ProfileMachineJSONL   Profile = "machine-jsonl"
	ProfilePlain          Profile = "plain"
	ProfileDumb           Profile = "dumb"
	ProfileAccessibleLine Profile = "accessible-line"
	ProfileANSI16         Profile = "ansi16"
	ProfileANSI256        Profile = "ansi256"
	ProfileTrueColor      Profile = "truecolor"
	ProfileMonochrome     Profile = "mono"
	ProfileNoColor        Profile = "none"
	ProfileFullScreen     Profile = "full-screen"
)

// Common spelling aliases retained for API ergonomics.
const (
	ColorNoColor      = ColorNone
	ColorANSI16Level  = ColorANSI16
	ColorANSI256Level = ColorANSI256
	ColorTruecolor    = ColorTrueColor
)

type ColorLevel int

const (
	ColorNone ColorLevel = iota
	ColorMonochrome
	ColorANSI16
	ColorANSI256
	ColorTrueColor
)

func (c ColorLevel) String() string {
	if !c.Valid() {
		return "invalid"
	}
	return []string{"none", "monochrome", "ansi16", "ansi256", "truecolor"}[c]
}

func (c ColorLevel) Valid() bool { return c >= ColorNone && c <= ColorTrueColor }
func (c ColorLevel) MarshalJSON() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("invalid color level %d", c)
	}
	return json.Marshal(c.String())
}
func (c *ColorLevel) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	levels := map[string]ColorLevel{"none": ColorNone, "monochrome": ColorMonochrome, "ansi16": ColorANSI16, "ansi256": ColorANSI256, "truecolor": ColorTrueColor}
	level, ok := levels[value]
	if !ok {
		return fmt.Errorf("unknown color level %q", value)
	}
	*c = level
	return nil
}

type UnicodeLevel int

const (
	UnicodeNone UnicodeLevel = iota
	UnicodeASCII
	UnicodeFull
)

func (u UnicodeLevel) String() string {
	if !u.Valid() {
		return "invalid"
	}
	return []string{"none", "ascii", "full"}[u]
}

func (u UnicodeLevel) Valid() bool { return u >= UnicodeNone && u <= UnicodeFull }
func (u UnicodeLevel) MarshalJSON() ([]byte, error) {
	if !u.Valid() {
		return nil, fmt.Errorf("invalid unicode level %d", u)
	}
	return json.Marshal(u.String())
}
func (u *UnicodeLevel) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	levels := map[string]UnicodeLevel{"none": UnicodeNone, "ascii": UnicodeASCII, "full": UnicodeFull}
	level, ok := levels[value]
	if !ok {
		return fmt.Errorf("unknown unicode level %q", value)
	}
	*u = level
	return nil
}

type Limits struct {
	MaxTreeNodes    int `json:"maxTreeNodes,omitempty"`
	MaxMessageBytes int `json:"maxMessageBytes,omitempty"`
	MaxInputBytes   int `json:"maxInputBytes,omitempty"`
}

type Manifest struct {
	ProtocolVersions   []string     `json:"protocolVersions"`
	OutputMode         OutputMode   `json:"outputMode"`
	Interactive        bool         `json:"interactive"`
	TTY                bool         `json:"tty"`
	Width              int          `json:"width,omitempty"`
	Height             int          `json:"height,omitempty"`
	Color              ColorLevel   `json:"color"`
	HardwareColor      ColorLevel   `json:"hardwareColor"`
	ColorDisabled      bool         `json:"colorDisabled,omitempty"`
	Unicode            UnicodeLevel `json:"unicode"`
	CursorAddressing   bool         `json:"cursorAddressing"`
	AlternateScreen    bool         `json:"alternateScreen"`
	Mouse              bool         `json:"mouse"`
	BracketedPaste     bool         `json:"bracketedPaste"`
	KeyboardLevel      string       `json:"keyboardLevel,omitempty"`
	ReducedMotion      bool         `json:"reducedMotion"`
	ScreenReader       bool         `json:"screenReader"`
	SecureInput        bool         `json:"secureInput"`
	Clipboard          bool         `json:"clipboard"`
	CoordinateFallback bool         `json:"coordinateFallback"`
	SnapshotModes      []string     `json:"snapshotModes,omitempty"`
	ActionFamilies     []string     `json:"actionFamilies,omitempty"`
	Limits             Limits       `json:"limits"`
}

func (m Manifest) Clone() Manifest {
	m.ProtocolVersions = append([]string(nil), m.ProtocolVersions...)
	m.SnapshotModes = append([]string(nil), m.SnapshotModes...)
	m.ActionFamilies = append([]string(nil), m.ActionFamilies...)
	return m
}

type ColorPolicy string

const (
	ColorAuto   ColorPolicy = "auto"
	ColorNever  ColorPolicy = "never"
	ColorAlways ColorPolicy = "always"
)

type Policy struct {
	OutputMode                                                                                                      OutputMode
	Color                                                                                                           ColorPolicy
	Unicode, Motion, Mouse, AlternateScreen, CursorAddressing, BracketedPaste, Clipboard, SecureInput, ScreenReader *bool
	CoordinateFallback                                                                                              *bool
	MaxColor                                                                                                        ColorLevel
	RequireTTY                                                                                                      bool
}

type Overrides struct {
	OutputMode                                                                                                             OutputMode
	Color                                                                                                                  ColorPolicy
	Unicode, ReducedMotion, Mouse, AlternateScreen, CursorAddressing, BracketedPaste, Clipboard, SecureInput, ScreenReader *bool
	CoordinateFallback                                                                                                     *bool
}

type SecurityPolicy struct {
	Clipboard, SecureInput, CoordinateFallback, AlternateScreen, BracketedPaste *bool
}

type Negotiation struct {
	RuntimeDetected Manifest
	ClientOffered   Manifest
	Application     Policy
	UserOverrides   Overrides
	Security        SecurityPolicy
	// ClientPresent distinguishes an absent offer from an explicit all-false offer.
	ClientPresent bool
}

type Diagnostic struct {
	Code    string
	Field   string
	Message string
}

func (n Negotiation) Resolve() (Manifest, []Diagnostic) {
	r := sanitizeManifest(n.RuntimeDetected.Clone())
	diags := make([]Diagnostic, 0, 16)
	c := sanitizeManifest(n.ClientOffered.Clone())
	appDeniedColor := false
	userDeniedColor := false

	if n.ClientPresent {
		if c.Width > 0 && (r.Width == 0 || c.Width < r.Width) {
			r.Width = c.Width
			diags = append(diags, diag("client.viewport.width", "width", "client width limited the negotiated viewport"))
		}
		if c.Height > 0 && (r.Height == 0 || c.Height < r.Height) {
			r.Height = c.Height
			diags = append(diags, diag("client.viewport.height", "height", "client height limited the negotiated viewport"))
		}
		r.Color = minColor(r.Color, c.Color)
		r.HardwareColor = minColor(colorCeiling(r), c.Color)
		r.Unicode = minUnicode(r.Unicode, c.Unicode)
		r.Interactive = r.Interactive && c.Interactive
		r.TTY = r.TTY && c.TTY
		r.CursorAddressing = r.CursorAddressing && c.CursorAddressing
		r.AlternateScreen = r.AlternateScreen && c.AlternateScreen
		r.Mouse = r.Mouse && c.Mouse
		r.BracketedPaste = r.BracketedPaste && c.BracketedPaste
		r.SecureInput = r.SecureInput && c.SecureInput
		r.ScreenReader = r.ScreenReader && c.ScreenReader
		r.Clipboard = r.Clipboard && c.Clipboard
		r.CoordinateFallback = r.CoordinateFallback && c.CoordinateFallback
		if r.Color != colorCeiling(n.RuntimeDetected) {
			diags = append(diags, diag("client.color.ceiling", "color", "client offer reduced the available color ceiling"))
		}
		if !r.Mouse && n.RuntimeDetected.Mouse {
			diags = append(diags, diag("client.mouse.ceiling", "mouse", "client offer disabled mouse support"))
		}
		if !r.CoordinateFallback && n.RuntimeDetected.CoordinateFallback {
			diags = append(diags, diag("client.coordinate.ceiling", "coordinateFallback", "client offer disabled coordinate fallback"))
		}
	}

	if n.ClientPresent && c.OutputMode != "" && c.OutputMode != OutputAuto {
		r.OutputMode = c.OutputMode
	}
	if n.Application.OutputMode != "" && n.Application.OutputMode != OutputAuto {
		r.OutputMode = n.Application.OutputMode
	}
	if n.UserOverrides.OutputMode != "" && n.UserOverrides.OutputMode != OutputAuto {
		r.OutputMode = n.UserOverrides.OutputMode
	}

	if n.Application.MaxColor > 0 && r.HardwareColor > n.Application.MaxColor {
		r.HardwareColor = n.Application.MaxColor
	}
	if n.Application.MaxColor > 0 && r.Color > n.Application.MaxColor {
		r.Color = n.Application.MaxColor
		diags = append(diags, diag("application.color.ceiling", "color", "application policy lowered the color ceiling"))
	}

	if n.Application.Color == ColorNever {
		r.Color = ColorNone
		r.ColorDisabled = true
		appDeniedColor = true
		diags = append(diags, diag("application.color.never", "color", "application policy disabled color"))
	}
	if n.UserOverrides.Color == ColorNever {
		r.Color = ColorNone
		r.ColorDisabled = true
		userDeniedColor = true
		diags = append(diags, diag("user.color.never", "color", "user override disabled color"))
	}

	if n.Application.RequireTTY && !r.TTY {
		r.OutputMode = OutputPlain
		r.Interactive = false
		diags = append(diags, diag("application.require_tty", "tty", "application policy required a TTY so negotiation degraded to plain output"))
	}

	applyBool := func(field string, dst *bool, v *bool, code string, msg string) {
		if v != nil && !*v && *dst {
			*dst = false
			diags = append(diags, diag(code, field, msg))
		}
	}

	if n.Application.Unicode != nil && !*n.Application.Unicode && r.Unicode != UnicodeASCII {
		r.Unicode = UnicodeASCII
		diags = append(diags, diag("application.unicode.ascii", "unicode", "application policy forced ASCII-safe output"))
	}
	applyBool("reducedMotion", &r.ReducedMotion, n.Application.Motion, "application.motion.reduced", "application policy disabled nonessential motion")
	applyBool("mouse", &r.Mouse, n.Application.Mouse, "application.mouse.disabled", "application policy disabled mouse support")
	applyBool("alternateScreen", &r.AlternateScreen, n.Application.AlternateScreen, "application.alt.disabled", "application policy disabled alternate screen")
	applyBool("cursorAddressing", &r.CursorAddressing, n.Application.CursorAddressing, "application.cursor.disabled", "application policy disabled cursor addressing")
	applyBool("bracketedPaste", &r.BracketedPaste, n.Application.BracketedPaste, "application.paste.disabled", "application policy disabled bracketed paste")
	applyBool("clipboard", &r.Clipboard, n.Application.Clipboard, "application.clipboard.disabled", "application policy disabled clipboard support")
	applyBool("secureInput", &r.SecureInput, n.Application.SecureInput, "application.secure_input.disabled", "application policy disabled secure input")
	applyBool("screenReader", &r.ScreenReader, n.Application.ScreenReader, "application.screen_reader.disabled", "application policy disabled screen-reader mode")
	applyBool("coordinateFallback", &r.CoordinateFallback, n.Application.CoordinateFallback, "application.coordinate.disabled", "application policy disabled coordinate fallback")

	if n.UserOverrides.Unicode != nil && !*n.UserOverrides.Unicode && r.Unicode != UnicodeASCII {
		r.Unicode = UnicodeASCII
		diags = append(diags, diag("user.unicode.ascii", "unicode", "user override forced ASCII-safe output"))
	}
	applyBool("reducedMotion", &r.ReducedMotion, n.UserOverrides.ReducedMotion, "user.motion.reduced", "user override disabled nonessential motion")
	applyBool("mouse", &r.Mouse, n.UserOverrides.Mouse, "user.mouse.disabled", "user override disabled mouse support")
	applyBool("alternateScreen", &r.AlternateScreen, n.UserOverrides.AlternateScreen, "user.alt.disabled", "user override disabled alternate screen")
	applyBool("cursorAddressing", &r.CursorAddressing, n.UserOverrides.CursorAddressing, "user.cursor.disabled", "user override disabled cursor addressing")
	applyBool("bracketedPaste", &r.BracketedPaste, n.UserOverrides.BracketedPaste, "user.paste.disabled", "user override disabled bracketed paste")
	applyBool("clipboard", &r.Clipboard, n.UserOverrides.Clipboard, "user.clipboard.disabled", "user override disabled clipboard support")
	applyBool("secureInput", &r.SecureInput, n.UserOverrides.SecureInput, "user.secure_input.disabled", "user override disabled secure input")
	applyBool("screenReader", &r.ScreenReader, n.UserOverrides.ScreenReader, "user.screen_reader.disabled", "user override disabled screen-reader mode")
	applyBool("coordinateFallback", &r.CoordinateFallback, n.UserOverrides.CoordinateFallback, "user.coordinate.disabled", "user override disabled coordinate fallback")

	if n.UserOverrides.Color == ColorAlways && r.Color == ColorNone && !appDeniedColor && !userDeniedColor {
		if ceiling := minColor(r.HardwareColor, maxColorPolicy(n.Application.MaxColor, r.HardwareColor)); ceiling > ColorNone && r.TTY && r.OutputMode != OutputPlain && r.OutputMode != OutputDumb {
			r.Color = ceiling
			r.ColorDisabled = false
			diags = append(diags, diag("user.color.always", "color", "user override re-enabled color up to the negotiated ceiling"))
		}
	}

	applyBool("clipboard", &r.Clipboard, n.Security.Clipboard, "security.clipboard.denied", "security policy disabled clipboard support")
	applyBool("secureInput", &r.SecureInput, n.Security.SecureInput, "security.secure_input.denied", "security policy disabled secure input")
	applyBool("coordinateFallback", &r.CoordinateFallback, n.Security.CoordinateFallback, "security.coordinate.denied", "security policy disabled coordinate fallback")
	applyBool("alternateScreen", &r.AlternateScreen, n.Security.AlternateScreen, "security.alt.denied", "security policy disabled alternate screen")
	applyBool("bracketedPaste", &r.BracketedPaste, n.Security.BracketedPaste, "security.paste.denied", "security policy disabled bracketed paste")

	if r.OutputMode == OutputMachineJSON || r.OutputMode == OutputMachineJSONL {
		r.Color = ColorNone
		r.Unicode = minUnicode(r.Unicode, UnicodeASCII)
		r.CursorAddressing = false
		r.AlternateScreen = false
		r.Mouse = false
		r.BracketedPaste = false
		r.Interactive = false
		r.CoordinateFallback = false
		diags = append(diags, diag("output.machine", "outputMode", "machine output disabled interactive decoration"))
	}

	if r.ScreenReader && r.OutputMode == OutputAuto {
		r.OutputMode = OutputAccessibleLine
		r.CursorAddressing = false
		r.AlternateScreen = false
		r.Mouse = false
		r.BracketedPaste = false
		r.CoordinateFallback = false
		diags = append(diags, diag("screen_reader.accessible_line", "screenReader", "screen-reader mode selected accessible line output"))
	}

	if !r.TTY {
		if r.OutputMode != OutputMachineJSON && r.OutputMode != OutputMachineJSONL && r.OutputMode != OutputAccessibleLine {
			r.OutputMode = OutputPlain
		}
		r.Color = ColorNone
		r.Unicode = UnicodeASCII
		r.CursorAddressing = false
		r.AlternateScreen = false
		r.Mouse = false
		r.BracketedPaste = false
		r.Interactive = false
		r.CoordinateFallback = false
		diags = append(diags, diag("runtime.non_tty", "tty", "non-TTY output disabled interactive terminal features"))
	}

	if r.OutputMode == OutputDumb {
		r.Color = ColorNone
		r.Unicode = UnicodeASCII
		r.CursorAddressing = false
		r.AlternateScreen = false
		r.Mouse = false
		r.BracketedPaste = false
		r.CoordinateFallback = false
		diags = append(diags, diag("runtime.dumb", "outputMode", "dumb terminal fallback disabled advanced terminal features"))
	}

	if r.OutputMode == OutputAccessibleLine {
		r.CursorAddressing = false
		r.AlternateScreen = false
		r.Mouse = false
		r.BracketedPaste = false
		r.CoordinateFallback = false
		r.ReducedMotion = true
		diags = append(diags, diag("output.accessible_line", "outputMode", "accessible line output disabled redraw-dependent features"))
	}

	if r.Width > 0 && r.Width < 40 {
		diags = append(diags, diag("viewport.narrow.essential", "width", "viewport width limited rendering to essential content"))
	} else if r.Width > 0 && r.Width < 60 {
		diags = append(diags, diag("viewport.narrow.records", "width", "viewport width requires record-style table fallback"))
	} else if r.Width > 0 && r.Width < 90 {
		diags = append(diags, diag("viewport.narrow.single_pane", "width", "viewport width requires single-pane disclosure fallback"))
	}

	if r.HardwareColor == ColorNone {
		r.Color = ColorNone
	}
	if !r.CursorAddressing {
		r.AlternateScreen = false
	}
	if !r.TTY {
		r.SecureInput = false
	}

	return sanitizeManifest(r), stableDiagnostics(diags)
}

type Probe struct {
	Env           map[string]string
	TTY           bool
	Width, Height int
}

// DetectEnv is deterministic: callers provide the complete environment snapshot.
func DetectEnv(env map[string]string, tty bool, width, height int) Manifest {
	m, _ := DetectWithDiagnostics(Probe{Env: env, TTY: tty, Width: width, Height: height})
	return m
}

func DetectEnvWithDiagnostics(env map[string]string, tty bool, width, height int) (Manifest, []Diagnostic) {
	return DetectWithDiagnostics(Probe{Env: env, TTY: tty, Width: width, Height: height})
}

func Detect(p Probe) Manifest {
	m, _ := DetectWithDiagnostics(p)
	return m
}

func DetectWithDiagnostics(p Probe) (Manifest, []Diagnostic) {
	env := p.Env
	if env == nil {
		env = map[string]string{}
	}
	get := func(k string) string { return strings.ToLower(strings.TrimSpace(env[k])) }
	m := Manifest{
		OutputMode:         OutputAuto,
		TTY:                p.TTY,
		Interactive:        p.TTY,
		Width:              max(0, p.Width),
		Height:             max(0, p.Height),
		Color:              ColorNone,
		HardwareColor:      ColorNone,
		Unicode:            UnicodeFull,
		CursorAddressing:   p.TTY,
		AlternateScreen:    p.TTY,
		Mouse:              p.TTY,
		BracketedPaste:     p.TTY,
		KeyboardLevel:      "basic",
		CoordinateFallback: false,
	}
	diags := make([]Diagnostic, 0, 8)
	term := get("TERM")
	ct := get("COLORTERM")
	termProgram := get("TERM_PROGRAM")
	windowsTerminal := strings.TrimSpace(env["WT_SESSION"]) != ""
	noColor := strings.TrimSpace(env["NO_COLOR"]) != ""

	switch {
	case !p.TTY:
		m.OutputMode = OutputPlain
		m.Unicode = UnicodeASCII
		diags = append(diags, diag("runtime.non_tty", "tty", "non-TTY runtime selected plain output"))
	case term == "dumb":
		m.OutputMode = OutputDumb
		m.Unicode = UnicodeASCII
		diags = append(diags, diag("runtime.dumb", "TERM", "TERM=dumb selected the dumb profile"))
	case ct == "truecolor" || ct == "24bit" || strings.Contains(term, "truecolor") || windowsTerminal || strings.Contains(termProgram, "iterm") || strings.Contains(termProgram, "wezterm"):
		m.Color = ColorTrueColor
		m.HardwareColor = ColorTrueColor
	case strings.Contains(term, "256color"):
		m.Color = ColorANSI256
		m.HardwareColor = ColorANSI256
	case strings.Contains(term, "mono"):
		m.Color = ColorMonochrome
		m.HardwareColor = ColorMonochrome
	case term != "":
		m.Color = ColorANSI16
		m.HardwareColor = ColorANSI16
	}

	if noColor {
		m.ColorDisabled = true
		m.Color = ColorNone
		m.Unicode = UnicodeASCII
		diags = append(diags, diag("runtime.no_color", "NO_COLOR", "NO_COLOR disabled color and selected ASCII-safe output"))
	}
	if p.Width < 0 || p.Height < 0 {
		diags = append(diags, diag("runtime.viewport.invalid", "viewport", "negative viewport inputs were clamped to zero"))
	}

	return sanitizeManifest(m), stableDiagnostics(diags)
}

func (m Manifest) Profiles() []string {
	profiles := make([]string, 0, 4)
	switch m.OutputMode {
	case OutputMachineJSON:
		profiles = append(profiles, string(ProfileMachineJSON))
	case OutputMachineJSONL:
		profiles = append(profiles, string(ProfileMachineJSONL))
	case OutputPlain:
		profiles = append(profiles, string(ProfilePlain))
	case OutputDumb:
		profiles = append(profiles, string(ProfileDumb))
	case OutputAccessibleLine:
		profiles = append(profiles, string(ProfileAccessibleLine))
	}
	switch m.Color {
	case ColorTrueColor:
		profiles = append(profiles, string(ProfileTrueColor))
	case ColorANSI256:
		profiles = append(profiles, string(ProfileANSI256))
	case ColorANSI16:
		profiles = append(profiles, string(ProfileANSI16))
	case ColorMonochrome:
		profiles = append(profiles, string(ProfileMonochrome))
	default:
		profiles = append(profiles, string(ProfileNoColor))
	}
	if m.TTY && m.CursorAddressing && m.AlternateScreen {
		profiles = append(profiles, string(ProfileFullScreen))
	}
	sort.Strings(profiles)
	return profiles
}

func sanitizeManifest(m Manifest) Manifest {
	if m.Width < 0 {
		m.Width = 0
	}
	if m.Height < 0 {
		m.Height = 0
	}
	if m.HardwareColor < m.Color {
		m.HardwareColor = m.Color
	}
	if m.OutputMode == "" {
		m.OutputMode = OutputAuto
	}
	return m
}

func colorCeiling(m Manifest) ColorLevel {
	if m.HardwareColor > 0 {
		return m.HardwareColor
	}
	return m.Color
}

func maxColorPolicy(policy ColorLevel, fallback ColorLevel) ColorLevel {
	if policy > 0 {
		return policy
	}
	return fallback
}

func minColor(a, b ColorLevel) ColorLevel {
	if a < b {
		return a
	}
	return b
}

func minUnicode(a, b UnicodeLevel) UnicodeLevel {
	if a < b {
		return a
	}
	return b
}

func stableDiagnostics(in []Diagnostic) []Diagnostic {
	out := append([]Diagnostic(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func diag(code, field, message string) Diagnostic {
	return Diagnostic{Code: code, Field: field, Message: message}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
