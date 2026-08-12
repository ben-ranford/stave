package atlasrig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/conformance"
)

const ManifestSchemaVersion = "atlas-proof-rig/v1"

type Scenario struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tier        string `json:"tier"`
}

type Profile struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	Capabilities capability.Manifest `json:"capabilities"`
}

func (p Profile) Mode() conformance.Mode {
	return conformance.Mode{
		Width:         p.Capabilities.Width,
		Height:        p.Capabilities.Height,
		Color:         p.Capabilities.Color > capability.ColorMonochrome,
		Unicode:       p.Capabilities.Unicode == capability.UnicodeFull,
		Interactive:   p.Capabilities.Interactive,
		TTY:           p.Capabilities.TTY,
		ReducedMotion: p.Capabilities.ReducedMotion,
	}
}

type Manifest struct {
	SchemaVersion string     `json:"schemaVersion"`
	Scenarios     []Scenario `json:"scenarios"`
	Profiles      []Profile  `json:"profiles"`
}

func (m Manifest) Clone() Manifest {
	out := m
	out.Scenarios = append([]Scenario(nil), m.Scenarios...)
	out.Profiles = make([]Profile, len(m.Profiles))
	for i, profile := range m.Profiles {
		out.Profiles[i] = profile
		out.Profiles[i].Capabilities = profile.Capabilities.Clone()
	}
	return out
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported atlas manifest schema %q", m.SchemaVersion)
	}
	if len(m.Scenarios) == 0 || len(m.Profiles) == 0 {
		return errors.New("atlas manifest requires scenarios and profiles")
	}
	seen := make(map[string]struct{}, len(m.Scenarios))
	for _, scenario := range m.Scenarios {
		if strings.TrimSpace(scenario.Name) == "" || strings.TrimSpace(scenario.Description) == "" {
			return errors.New("atlas scenario name and description are required")
		}
		if scenario.Tier != "core" && scenario.Tier != "breadth" {
			return fmt.Errorf("atlas scenario %q has invalid tier %q", scenario.Name, scenario.Tier)
		}
		if _, ok := seen[scenario.Name]; ok {
			return fmt.Errorf("duplicate atlas scenario %q", scenario.Name)
		}
		seen[scenario.Name] = struct{}{}
	}
	seen = make(map[string]struct{}, len(m.Profiles))
	for _, profile := range m.Profiles {
		if strings.TrimSpace(profile.Name) == "" || strings.TrimSpace(profile.Description) == "" {
			return errors.New("atlas profile name and description are required")
		}
		if _, ok := seen[profile.Name]; ok {
			return fmt.Errorf("duplicate atlas profile %q", profile.Name)
		}
		seen[profile.Name] = struct{}{}
		caps := profile.Capabilities
		if caps.Width <= 0 || caps.Height <= 0 || !caps.Color.Valid() || !caps.HardwareColor.Valid() || !caps.Unicode.Valid() {
			return fmt.Errorf("atlas profile %q has invalid capabilities", profile.Name)
		}
		if caps.Color > caps.HardwareColor {
			return fmt.Errorf("atlas profile %q exceeds its hardware colour ceiling", profile.Name)
		}
		if caps.Limits.MaxTreeNodes <= 0 || caps.Limits.MaxMessageBytes <= 0 || caps.Limits.MaxInputBytes <= 0 {
			return fmt.Errorf("atlas profile %q requires finite resource limits", profile.Name)
		}
	}
	return nil
}

// ParseManifest accepts exactly one JSON object, rejects unknown fields and
// recursively rejects duplicate object keys before decoding.
func ParseManifest(data []byte) (Manifest, error) {
	if err := validateUniqueJSONKeys(bytes.TrimSpace(data), 0); err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ManifestJSON(manifest Manifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func DefaultManifest() Manifest {
	profile := func(name, description string, output capability.OutputMode, color capability.ColorLevel, unicode capability.UnicodeLevel, tty, interactive, screenReader, reducedMotion bool, width int) Profile {
		terminalFeatures := tty && interactive
		return Profile{
			Name:        name,
			Description: description,
			Capabilities: capability.Manifest{
				ProtocolVersions: []string{"1.0"},
				OutputMode:       output,
				TTY:              tty, Interactive: interactive,
				Width: width, Height: 24,
				Color: color, HardwareColor: color, Unicode: unicode,
				CursorAddressing: terminalFeatures, AlternateScreen: terminalFeatures,
				Mouse: terminalFeatures, BracketedPaste: terminalFeatures,
				ReducedMotion: reducedMotion, ScreenReader: screenReader,
				SecureInput:    terminalFeatures,
				SnapshotModes:  []string{"full", "patch"},
				ActionFamilies: []string{"stave.atlas.route", "stave.primitive.core"},
				Limits:         capability.Limits{MaxTreeNodes: 4096, MaxMessageBytes: 4 << 20, MaxInputBytes: 64 << 10},
			},
		}
	}
	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Scenarios: []Scenario{
			{Name: "ready", Description: "normal operational command deck", Tier: "core"},
			{Name: "empty", Description: "no routes are available", Tier: "core"},
			{Name: "loading", Description: "route data is loading", Tier: "core"},
			{Name: "error", Description: "route loading failed safely", Tier: "core"},
			{Name: "invalid-form", Description: "operator form exposes accessible validation", Tier: "breadth"},
			{Name: "modal-confirmation", Description: "consequential action requires a modal confirmation", Tier: "breadth"},
			{Name: "dense-table", Description: "windowed route table, chart, tabs and pagination", Tier: "breadth"},
		},
		Profiles: []Profile{
			profile("machine-json", "non-interactive machine output", capability.OutputMachineJSON, capability.ColorNone, capability.UnicodeASCII, false, false, false, false, 80),
			profile("truecolor", "interactive 24-bit colour terminal", capability.OutputAuto, capability.ColorTrueColor, capability.UnicodeFull, true, true, false, false, 100),
			profile("ansi256", "interactive 256-colour terminal", capability.OutputAuto, capability.ColorANSI256, capability.UnicodeFull, true, true, false, false, 100),
			profile("ansi16", "interactive 16-colour terminal", capability.OutputAuto, capability.ColorANSI16, capability.UnicodeFull, true, true, false, false, 100),
			profile("monochrome", "interactive terminal with semantic emphasis but no colour", capability.OutputAuto, capability.ColorMonochrome, capability.UnicodeFull, true, true, false, false, 100),
			profile("ascii-narrow", "narrow ASCII-only terminal", capability.OutputAuto, capability.ColorNone, capability.UnicodeASCII, true, true, false, false, 28),
			profile("non-tty", "redirected deterministic plain output", capability.OutputPlain, capability.ColorNone, capability.UnicodeASCII, false, false, false, false, 80),
			profile("reduced-motion", "interactive terminal with reduced motion", capability.OutputAuto, capability.ColorNone, capability.UnicodeASCII, true, true, false, true, 80),
			profile("accessible", "screen-reader-oriented line output", capability.OutputAccessibleLine, capability.ColorNone, capability.UnicodeASCII, false, false, true, true, 80),
		},
	}
}

func validateUniqueJSONKeys(data []byte, depth int) error {
	if depth > 64 {
		return errors.New("atlas manifest JSON nesting exceeds limit")
	}
	if len(data) == 0 {
		return errors.New("atlas manifest is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return errors.New("atlas manifest must be a JSON object")
	}
	if depth == 0 && delim != '{' {
		return errors.New("atlas manifest must be a JSON object")
	}
	seen := map[string]struct{}{}
	for decoder.More() {
		if delim == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("atlas manifest contains an invalid object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate atlas manifest key %q", key)
			}
			seen[key] = struct{}{}
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			if err := validateUniqueJSONKeys(trimmed, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("atlas manifest must contain exactly one JSON value")
		}
		return err
	}
	return nil
}
