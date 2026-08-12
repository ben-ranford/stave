package capability

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
)

func TestCapabilityEnumsRejectInvalidWireValues(t *testing.T) {
	for _, value := range []any{ColorLevel(-1), ColorLevel(99), UnicodeLevel(-1), UnicodeLevel(99)} {
		if _, err := json.Marshal(value); err == nil {
			t.Fatalf("invalid capability enum %v was serialized", value)
		}
	}

	for _, value := range []any{ColorNone, ColorTrueColor, UnicodeNone, UnicodeFull} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %v: %v", value, err)
		}
		switch want := value.(type) {
		case ColorLevel:
			var roundTrip ColorLevel
			if err := json.Unmarshal(encoded, &roundTrip); err != nil || roundTrip != want {
				t.Fatalf("color round trip %v => %q => %v (%v)", want, encoded, roundTrip, err)
			}
		case UnicodeLevel:
			var roundTrip UnicodeLevel
			if err := json.Unmarshal(encoded, &roundTrip); err != nil || roundTrip != want {
				t.Fatalf("unicode round trip %v => %q => %v (%v)", want, encoded, roundTrip, err)
			}
		}
	}
}

func boolp(v bool) *bool { return &v }

func hasCode(diags []Diagnostic, code string) bool {
	for _, diag := range diags {
		if diag.Code == code {
			return true
		}
	}
	return false
}

func TestDetectExplicitEnvironment(t *testing.T) {
	m, diags := DetectEnvWithDiagnostics(map[string]string{"TERM": "xterm-256color"}, true, 80, 24)
	if m.Color != ColorANSI256 || m.HardwareColor != ColorANSI256 || m.OutputMode != OutputAuto {
		t.Fatalf("unexpected detection: %+v", m)
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	no, diags := DetectEnvWithDiagnostics(map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"}, true, 80, 24)
	if no.Color != ColorNone || no.HardwareColor != ColorANSI256 || no.Unicode != UnicodeASCII || !no.ColorDisabled {
		t.Fatalf("NO_COLOR did not preserve the runtime ceiling safely: %+v", no)
	}
	if !hasCode(diags, "runtime.no_color") {
		t.Fatalf("missing NO_COLOR diagnostic: %+v", diags)
	}
}

func TestNegotiationMachineAndPolicy(t *testing.T) {
	runtime := DetectEnv(map[string]string{"COLORTERM": "truecolor"}, true, 80, 24)
	m, diags := (Negotiation{RuntimeDetected: runtime, Application: Policy{OutputMode: OutputMachineJSON}}).Resolve()
	if m.Color != ColorNone || m.Interactive || m.Mouse || m.BracketedPaste {
		t.Fatalf("machine output decorated: %+v", m)
	}
	if !hasCode(diags, "output.machine") {
		t.Fatalf("missing machine output diagnostic: %+v", diags)
	}
}

func TestNonTTYMachineOutputIsNotReclassifiedAsPlain(t *testing.T) {
	runtime := Manifest{TTY: false, Interactive: false, Width: 80, Height: 24, Color: ColorNone, HardwareColor: ColorNone, Unicode: UnicodeASCII}
	for _, mode := range []OutputMode{OutputMachineJSON, OutputMachineJSONL} {
		got, _ := (Negotiation{RuntimeDetected: runtime, Application: Policy{OutputMode: mode}}).Resolve()
		if got.OutputMode != mode || got.TTY || got.Interactive || got.Color != ColorNone {
			t.Fatalf("mode %s resolved to %+v", mode, got)
		}
	}
}

func TestNonTTYAccessibleOutputIsNotReclassifiedAsPlain(t *testing.T) {
	runtime := Manifest{TTY: false, Interactive: false, Width: 80, Height: 24, Color: ColorNone, HardwareColor: ColorNone, Unicode: UnicodeASCII, ScreenReader: true}
	got, _ := (Negotiation{RuntimeDetected: runtime, Application: Policy{OutputMode: OutputAccessibleLine}}).Resolve()
	if got.OutputMode != OutputAccessibleLine || !got.ScreenReader || got.TTY || got.Interactive || got.Color != ColorNone {
		t.Fatalf("accessible line resolved to %+v", got)
	}
}

func TestNonTTYScreenReaderAutoSelectsAccessibleOutput(t *testing.T) {
	runtime := Manifest{TTY: false, Interactive: false, Width: 80, Height: 24, Color: ColorNone, HardwareColor: ColorNone, Unicode: UnicodeASCII, ScreenReader: true}
	got, diags := (Negotiation{RuntimeDetected: runtime}).Resolve()
	if got.OutputMode != OutputAccessibleLine || !got.ScreenReader || !got.ReducedMotion {
		t.Fatalf("screen-reader auto profile resolved to %+v", got)
	}
	if !hasCode(diags, "screen_reader.accessible_line") || !hasCode(diags, "runtime.non_tty") {
		t.Fatalf("missing accessible/non-TTY diagnostics: %+v", diags)
	}
}

func TestStrictIntersectionAndDenials(t *testing.T) {
	rt := DetectEnv(map[string]string{"TERM": "xterm-256color"}, true, 80, 24)
	client := Manifest{
		TTY:                true,
		Interactive:        true,
		Color:              ColorTrueColor,
		Unicode:            UnicodeFull,
		CursorAddressing:   true,
		AlternateScreen:    true,
		Mouse:              true,
		BracketedPaste:     true,
		SecureInput:        true,
		Clipboard:          true,
		CoordinateFallback: true,
	}
	got, _ := (Negotiation{RuntimeDetected: rt, ClientOffered: client, ClientPresent: true}).Resolve()
	if got.Color != ColorANSI256 || !got.Mouse || got.CoordinateFallback {
		t.Fatalf("intersection incorrect: %+v", got)
	}

	got, _ = (Negotiation{RuntimeDetected: rt, ClientOffered: Manifest{}, ClientPresent: true}).Resolve()
	if got.Color != ColorNone || got.Mouse || got.TTY {
		t.Fatalf("zero offer not treated as a hard denial: %+v", got)
	}

	got, _ = (Negotiation{RuntimeDetected: rt}).Resolve()
	if got.Color != ColorANSI256 {
		t.Fatalf("absent offer constrained runtime: %+v", got)
	}

	got, _ = (Negotiation{
		RuntimeDetected: rt,
		Application:     Policy{Mouse: boolp(false), CoordinateFallback: boolp(false)},
		UserOverrides:   Overrides{Mouse: boolp(true), CoordinateFallback: boolp(true)},
	}).Resolve()
	if got.Mouse || got.CoordinateFallback {
		t.Fatalf("application denials were elevated by user overrides: %+v", got)
	}
}

func TestNoInventedANSI16(t *testing.T) {
	if got := DetectEnv(map[string]string{}, true, 80, 24); got.Color != ColorNone {
		t.Fatalf("unset TERM invented color: %v", got.Color)
	}
}

func TestColorAlwaysNeverInventsOrOverridesDenial(t *testing.T) {
	for _, runtime := range []Manifest{
		{Color: ColorNone, HardwareColor: ColorNone, TTY: true, Interactive: true},
		DetectEnv(map[string]string{"TERM": "dumb"}, true, 80, 24),
		DetectEnv(map[string]string{"TERM": "xterm-256color"}, false, 80, 24),
	} {
		got, _ := (Negotiation{RuntimeDetected: runtime, UserOverrides: Overrides{Color: ColorAlways}}).Resolve()
		if got.Color != ColorNone {
			t.Fatalf("always invented color from %+v: %v", runtime, got.Color)
		}
	}

	runtime := DetectEnv(map[string]string{"COLORTERM": "truecolor"}, true, 80, 24)
	got, _ := (Negotiation{RuntimeDetected: runtime, Application: Policy{Color: ColorNever}, UserOverrides: Overrides{Color: ColorAlways}}).Resolve()
	if got.Color != ColorNone {
		t.Fatalf("always overrode denial: %v", got.Color)
	}
}

func TestUserOverrideCanReenableNoColorWithinHardwareCeiling(t *testing.T) {
	runtime := DetectEnv(map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"}, true, 80, 24)
	got, diags := (Negotiation{RuntimeDetected: runtime, UserOverrides: Overrides{Color: ColorAlways}}).Resolve()
	if got.Color != ColorANSI256 {
		t.Fatalf("user override did not restore the detected ceiling: %+v", got)
	}
	if !hasCode(diags, "user.color.always") {
		t.Fatalf("missing user color restoration diagnostic: %+v", diags)
	}
}

func TestCapabilityProfileMatrix(t *testing.T) {
	cases := []struct {
		name        string
		manifest    Manifest
		profiles    []string
		color       ColorLevel
		interactive bool
	}{
		{"truecolor", DetectEnv(map[string]string{"COLORTERM": "truecolor"}, true, 120, 40), []string{"full-screen", "truecolor"}, ColorTrueColor, true},
		{"ansi256", DetectEnv(map[string]string{"TERM": "xterm-256color"}, true, 120, 40), []string{"ansi256", "full-screen"}, ColorANSI256, true},
		{"ansi16", DetectEnv(map[string]string{"TERM": "screen"}, true, 120, 40), []string{"ansi16", "full-screen"}, ColorANSI16, true},
		{"mono", Manifest{TTY: true, Interactive: true, Color: ColorMonochrome, HardwareColor: ColorMonochrome, Unicode: UnicodeFull, CursorAddressing: true, AlternateScreen: true}, []string{"full-screen", "mono"}, ColorMonochrome, true},
		{"none", Manifest{TTY: true, Interactive: true, Color: ColorNone, HardwareColor: ColorNone, Unicode: UnicodeASCII}, []string{"none"}, ColorNone, true},
		{"plain", DetectEnv(map[string]string{"TERM": "xterm-256color"}, false, 120, 40), []string{"none", "plain"}, ColorNone, false},
		{"dumb", DetectEnv(map[string]string{"TERM": "dumb"}, true, 120, 40), []string{"dumb", "none"}, ColorNone, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.manifest.Color != tc.color {
				t.Fatalf("unexpected color: %+v", tc.manifest)
			}
			if tc.manifest.Interactive != tc.interactive {
				t.Fatalf("unexpected interactivity: %+v", tc.manifest)
			}
			for _, want := range tc.profiles {
				if !slices.Contains(tc.manifest.Profiles(), want) {
					t.Fatalf("profiles %v missing %q", tc.manifest.Profiles(), want)
				}
			}
		})
	}
}

func TestCrossPlatformTerminalColourDetection(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want ColorLevel
	}{
		{"linux truecolor", map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, ColorTrueColor},
		{"macOS iTerm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "iTerm.app"}, ColorTrueColor},
		{"Windows Terminal", map[string]string{"TERM": "xterm-256color", "WT_SESSION": "opaque-session"}, ColorTrueColor},
		{"portable 256", map[string]string{"TERM": "screen-256color"}, ColorANSI256},
		{"portable ANSI16", map[string]string{"TERM": "vt100"}, ColorANSI16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectEnv(tc.env, true, 80, 24)
			if got.Color != tc.want || got.HardwareColor != tc.want {
				t.Fatalf("colour=%s hardware=%s want=%s", got.Color, got.HardwareColor, tc.want)
			}
		})
	}
}

func TestContradictoryOverridesDegradeSafely(t *testing.T) {
	runtime := DetectEnv(map[string]string{"COLORTERM": "truecolor"}, true, 50, 20)
	runtime.CoordinateFallback = true
	got, diags := (Negotiation{
		RuntimeDetected: runtime,
		ClientOffered: Manifest{
			TTY:                true,
			Interactive:        true,
			Color:              ColorANSI16,
			Unicode:            UnicodeASCII,
			CursorAddressing:   false,
			AlternateScreen:    false,
			Mouse:              false,
			CoordinateFallback: true,
		},
		ClientPresent: true,
		UserOverrides: Overrides{
			OutputMode:         OutputAccessibleLine,
			Mouse:              boolp(true),
			CoordinateFallback: boolp(true),
		},
		Security: SecurityPolicy{
			CoordinateFallback: boolp(false),
		},
	}).Resolve()

	if got.Color != ColorANSI16 || got.Unicode != UnicodeASCII || got.Mouse || got.CoordinateFallback || got.OutputMode != OutputAccessibleLine {
		t.Fatalf("contradictory overrides did not degrade safely: %+v", got)
	}
	if !hasCode(diags, "client.color.ceiling") || !hasCode(diags, "security.coordinate.denied") || !hasCode(diags, "output.accessible_line") {
		t.Fatalf("missing degradation reasons: %+v", diags)
	}
}

func TestConcurrentManifestCloneAndProfiles(t *testing.T) {
	manifest, _ := (Negotiation{
		RuntimeDetected: DetectEnv(map[string]string{"COLORTERM": "truecolor"}, true, 90, 30),
		UserOverrides:   Overrides{ReducedMotion: boolp(true)},
	}).Resolve()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clone := manifest.Clone()
			if clone.Color != manifest.Color {
				t.Errorf("clone mismatch: %+v != %+v", clone, manifest)
			}
			if len(clone.Profiles()) == 0 {
				t.Error("profiles unexpectedly empty")
			}
		}()
	}
	wg.Wait()
}
