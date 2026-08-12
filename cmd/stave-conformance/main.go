package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/conformance"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/internal/examples"
	"github.com/ben-ranford/stave/internal/width"
	"github.com/ben-ranford/stave/keymap"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/testfixture"
	"github.com/ben-ranford/stave/theme"
	"os"
	"path/filepath"
)

const primitiveManifestSchemaVersion = "stave.primitive.conformance.v1"

var supportedPrimitiveInvariants = map[string]struct{}{
	"stable-node-ids":       {},
	"accessible-names":      {},
	"keyboard-agent-parity": {},
	"colour-not-meaning":    {},
	"secret-redaction":      {},
}

type catalogAdapter struct {
	tree     semantic.Node
	registry *action.Registry
	km       keymap.Map
}

type primitiveAdapter interface {
	Name() string
	Render(semantic.Node, conformance.Mode) (string, error)
	ActionRegistry() *action.Registry
	Keymap() keymap.Map
}

func (a catalogAdapter) Name() string { return "catalog" }
func (a catalogAdapter) Render(root semantic.Node, mode conformance.Mode) (string, error) {
	caps := capability.Manifest{
		ProtocolVersions: []string{"1.0"},
		OutputMode:       capability.OutputPlain,
		Interactive:      mode.Interactive,
		TTY:              mode.TTY,
		Width:            mode.Width,
		Height:           mode.Height,
		Color:            capability.ColorNone,
		HardwareColor:    capability.ColorNone,
		Unicode:          capability.UnicodeFull,
		ReducedMotion:    mode.ReducedMotion,
	}
	if mode.Color {
		caps.Color, caps.HardwareColor = capability.ColorTrueColor, capability.ColorTrueColor
	}
	if mode.Unicode {
		caps.Unicode = capability.UnicodeFull
	} else {
		caps.Unicode = capability.UnicodeASCII
	}
	if mode.TTY {
		caps.OutputMode = capability.OutputAuto
	}
	resolved, err := examples.BrandTheme("catalog", "#5fafff", "STAVE / CATALOG").Resolve(theme.ModeDark, theme.DensityComfortable, caps)
	if err != nil {
		return "", err
	}
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		return "", err
	}
	result, err := render.Render(render.Request{Tree: tree, Theme: resolved, Capabilities: caps, Viewport: layout.Size{Width: mode.Width, Height: mode.Height}})
	if err != nil {
		return "", err
	}
	output := result.Terminal
	if output == "" {
		output = result.Plain
	}
	if strings.TrimSpace(output) == "" {
		return "", fmt.Errorf("renderer produced empty output")
	}
	if !mode.Color && strings.Contains(output, "\x1b[") {
		return "", fmt.Errorf("no-colour mode emitted ANSI")
	}
	if !mode.Unicode && !asciiOnly(output) {
		return "", fmt.Errorf("ASCII mode emitted non-ASCII text")
	}
	for _, line := range strings.Split(result.Surface.String(), "\n") {
		if width.String(line) > mode.Width {
			return "", fmt.Errorf("rendered line width %d exceeds viewport %d", width.String(line), mode.Width)
		}
	}
	return output, nil
}
func (a catalogAdapter) ActionRegistry() *action.Registry { return a.registry }
func (a catalogAdapter) Keymap() keymap.Map               { return a.km }

func asciiOnly(value string) bool {
	for len(value) > 0 {
		r, n := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && n == 1 || r > 127 {
			return false
		}
		value = value[n:]
	}
	return true
}

func conformanceMode(name string) (conformance.Mode, error) {
	mode := conformance.Mode{Width: 80, Height: 24, Unicode: true}
	switch name {
	case "tty-color":
		mode.TTY, mode.Interactive, mode.Color = true, true, true
	case "tty-no-color":
		mode.TTY, mode.Interactive = true, true
	case "ascii":
		mode.TTY, mode.Interactive, mode.Unicode = true, true, false
	case "narrow":
		mode.TTY, mode.Interactive, mode.Unicode, mode.Width = true, true, false, 30
	case "non-tty":
		mode.TTY, mode.Interactive = false, false
	case "reduced-motion":
		mode.TTY, mode.Interactive, mode.Color, mode.ReducedMotion = true, true, true, true
	default:
		return conformance.Mode{}, fmt.Errorf("unknown conformance mode %q", name)
	}
	return mode, nil
}

func main() {
	data, err := os.ReadFile(filepath.Join("testdata", "primitive-manifest.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	manifest, err := loadPrimitiveManifest(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	modes, modesByName, err := conformanceModes(manifest.Modes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(modes) == 0 {
		fmt.Fprintln(os.Stderr, "primitive manifest defines no conformance modes")
		os.Exit(1)
	}
	invariants := append([]string(nil), manifest.Invariants...)
	for _, name := range manifest.Fixtures {
		tree, err := testfixture.Primitive(name, "catalog")
		if err != nil {
			fmt.Fprintln(os.Stderr, name, err)
			os.Exit(1)
		}
		registry := action.NewRegistry()
		ids := map[action.ID]bool{}
		var walk func(semantic.Node)
		walk = func(n semantic.Node) {
			for _, ref := range n.Actions() {
				ids[action.ID(ref.ID)] = true
			}
			for _, c := range n.Children() {
				walk(c)
			}
		}
		walk(tree)
		orderedIDs := make([]action.ID, 0, len(ids))
		for id := range ids {
			orderedIDs = append(orderedIDs, id)
		}
		sort.Slice(orderedIDs, func(i, j int) bool { return orderedIDs[i] < orderedIDs[j] })
		mappings := make([]keymap.Mapping, 0, len(orderedIDs))
		i := 0
		for _, id := range orderedIDs {
			def := action.Definition{ID: id, Version: "1", Title: string(id), InputSchema: action.Schema{ID: "in", JSON: json.RawMessage(`{}`)}, OutputSchema: action.Schema{ID: "out", JSON: json.RawMessage(`{}`)}, Safety: action.ReadOnly}
			if e := registry.Register(def, func(context.Context, action.Call, any) (any, error) { return json.RawMessage(`{}`), nil }); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
			key, _ := input.ParseKey(fmt.Sprintf("ctrl+%c", 'a'+rune(i%26)))
			mappings = append(mappings, keymap.Mapping{Binding: keymap.Binding{Sequence: []input.KeyChord{key}, Command: keymap.CommandID("catalog." + string(id))}, Route: keymap.Route{Kind: keymap.RouteAction, ActionID: id}})
			i++
		}
		km, e := keymap.New("catalog", mappings)
		if e != nil {
			fmt.Fprintln(os.Stderr, name, e)
			os.Exit(1)
		}
		adapter := catalogAdapter{tree: tree, registry: registry, km: km}
		if err := enforceDeclaredPrimitiveInvariants(adapter, tree, invariants, modesByName); err != nil {
			fmt.Fprintln(os.Stderr, name, err)
			os.Exit(1)
		}
		report := conformance.CheckClient(adapter, []conformance.ClientFixture{{Name: name, Tree: tree, Modes: modes}})
		if len(report.Failures) > 0 {
			for _, f := range report.Failures {
				fmt.Fprintln(os.Stderr, name, f)
			}
			os.Exit(1)
		}
	}
	fmt.Println("stave primitive conformance: ok")
}

type primitiveManifest struct {
	SchemaVersion string   `json:"schemaVersion"`
	Fixtures      []string `json:"fixtures"`
	Modes         []string `json:"modes"`
	Invariants    []string `json:"invariants"`
}

func loadPrimitiveManifest(data []byte) (primitiveManifest, error) {
	var manifest primitiveManifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return primitiveManifest{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return primitiveManifest{}, fmt.Errorf("trailing primitive manifest JSON")
	}
	if manifest.SchemaVersion != primitiveManifestSchemaVersion {
		return primitiveManifest{}, fmt.Errorf("unexpected primitive manifest schema version %q", manifest.SchemaVersion)
	}
	if len(manifest.Fixtures) == 0 {
		return primitiveManifest{}, fmt.Errorf("primitive manifest defines no fixtures")
	}
	if len(manifest.Modes) == 0 {
		return primitiveManifest{}, fmt.Errorf("primitive manifest defines no conformance modes")
	}
	if len(manifest.Invariants) == 0 {
		return primitiveManifest{}, fmt.Errorf("primitive manifest defines no invariants")
	}
	if err := validatePrimitiveManifestValues(manifest.Fixtures, "fixture"); err != nil {
		return primitiveManifest{}, err
	}
	if err := validatePrimitiveManifestValues(manifest.Modes, "mode"); err != nil {
		return primitiveManifest{}, err
	}
	if err := validatePrimitiveManifestValues(manifest.Invariants, "invariant"); err != nil {
		return primitiveManifest{}, err
	}
	for _, invariant := range manifest.Invariants {
		if _, ok := supportedPrimitiveInvariants[invariant]; !ok {
			return primitiveManifest{}, fmt.Errorf("unsupported primitive invariant %q", invariant)
		}
	}
	return manifest, nil
}

func validatePrimitiveManifestValues(values []string, kind string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("primitive manifest defines an empty %s", kind)
		}
		if seen[value] {
			return fmt.Errorf("primitive manifest defines duplicate %s %q", kind, value)
		}
		seen[value] = true
	}
	return nil
}

func conformanceModes(names []string) ([]conformance.Mode, map[string]conformance.Mode, error) {
	modes := make([]conformance.Mode, 0, len(names))
	byName := make(map[string]conformance.Mode, len(names))
	for _, name := range names {
		mode, err := conformanceMode(name)
		if err != nil {
			return nil, nil, err
		}
		modes = append(modes, mode)
		byName[name] = mode
	}
	return modes, byName, nil
}

func enforceDeclaredPrimitiveInvariants(adapter primitiveAdapter, tree semantic.Node, invariants []string, modesByName map[string]conformance.Mode) error {
	failures := conformance.ValidateTree(tree)
	for _, invariant := range invariants {
		switch invariant {
		case "stable-node-ids":
			if hasConformanceFailureRule(failures, "unique-node-id") {
				return fmt.Errorf("stable-node-ids invariant failed")
			}
		case "accessible-names":
			if hasConformanceFailureRule(failures, "accessible-name") {
				return fmt.Errorf("accessible-names invariant failed")
			}
		case "keyboard-agent-parity":
			mode, ok := firstConformanceMode(modesByName)
			if !ok {
				return fmt.Errorf("keyboard-agent-parity requires at least one conformance mode")
			}
			report := conformance.CheckAdapter(adapter, tree, []conformance.Mode{mode})
			for _, failure := range report.Failures {
				if failure.Rule == "action-registry" || failure.Rule == "action-registry-authority" || failure.Rule == "keymap-action-authority" || failure.Rule == "keymap-binding-authority" {
					return fmt.Errorf("keyboard-agent-parity invariant failed: %s", failure.Detail)
				}
			}
		case "colour-not-meaning":
			colorMode, ok := modesByName["tty-color"]
			if !ok {
				return fmt.Errorf("colour-not-meaning requires tty-color mode")
			}
			plainMode, ok := modesByName["tty-no-color"]
			if !ok {
				return fmt.Errorf("colour-not-meaning requires tty-no-color mode")
			}
			coloured, err := adapter.Render(tree, colorMode)
			if err != nil {
				return err
			}
			plain, err := adapter.Render(tree, plainMode)
			if err != nil {
				return err
			}
			colouredPlain := normalizeRenderedMeaning(stripANSI(coloured))
			plainText := normalizeRenderedMeaning(stripANSI(plain))
			if colouredPlain != plainText {
				return fmt.Errorf("colour-not-meaning invariant failed: colour=%q no-colour=%q", colouredPlain, plainText)
			}
		case "secret-redaction":
			if hasConformanceFailureRule(failures, "secret-redaction") {
				return fmt.Errorf("secret-redaction invariant failed")
			}
		default:
			return fmt.Errorf("unsupported primitive invariant %q", invariant)
		}
	}
	return nil
}

func firstConformanceMode(modes map[string]conformance.Mode) (conformance.Mode, bool) {
	for _, mode := range modes {
		return mode, true
	}
	return conformance.Mode{}, false
}

func hasConformanceFailureRule(failures []conformance.Failure, rule string) bool {
	for _, failure := range failures {
		if failure.Rule == rule {
			return true
		}
	}
	return false
}

func stripANSI(value string) string {
	if !strings.Contains(value, "\x1b[") {
		return value
	}
	var b strings.Builder
	for i := 0; i < len(value); {
		if value[i] != 0x1b {
			b.WriteByte(value[i])
			i++
			continue
		}
		i++
		if i < len(value) && value[i] == '[' {
			i++
			for i < len(value) {
				ch := value[i]
				i++
				if ch >= '@' && ch <= '~' {
					break
				}
			}
			continue
		}
	}
	return b.String()
}

func normalizeRenderedMeaning(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t")
	}
	return strings.Join(lines, "\n")
}
