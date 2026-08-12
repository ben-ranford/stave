// Package atlasrig is Atlas's executable regression and proof client for
// Stave. It is application-owned and imports only public Stave contracts.
package atlasrig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/conformance"
	"github.com/ben-ranford/stave/keymap"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/theme"
)

type Artifact struct {
	Scenario        string                `json:"scenario"`
	Tier            string                `json:"tier"`
	Profile         string                `json:"profile"`
	OutputMode      capability.OutputMode `json:"outputMode"`
	Color           string                `json:"color"`
	Unicode         string                `json:"unicode"`
	TTY             bool                  `json:"tty"`
	Interactive     bool                  `json:"interactive"`
	ScreenReader    bool                  `json:"screenReader,omitempty"`
	ReducedMotion   bool                  `json:"reducedMotion,omitempty"`
	Viewport        layout.Size           `json:"viewport"`
	NodeCount       int                   `json:"nodeCount"`
	TreeHash        string                `json:"treeHash"`
	SnapshotHash    string                `json:"snapshotHash"`
	ThemeHash       string                `json:"themeHash"`
	PlanHash        string                `json:"planHash"`
	SurfaceHash     string                `json:"surfaceHash"`
	PlainHash       string                `json:"plainHash"`
	MachineHash     string                `json:"machineHash"`
	TerminalHash    string                `json:"terminalHash"`
	ActionIDs       []string              `json:"actionIds"`
	KeymapActionIDs []string              `json:"keymapActionIds"`
	ContainsANSI    bool                  `json:"containsAnsi"`
	PlainASCII      bool                  `json:"plainAscii"`

	Plain    string `json:"-"`
	Machine  []byte `json:"-"`
	Terminal string `json:"-"`
}

type Matrix struct {
	SchemaVersion string     `json:"schemaVersion"`
	ManifestHash  string     `json:"manifestHash"`
	Scenarios     int        `json:"scenarios"`
	Profiles      int        `json:"profiles"`
	Artifacts     []Artifact `json:"artifacts"`
}

type Rig struct {
	manifest Manifest
	registry *action.Registry
	keymap   keymap.Map
	theme    theme.Theme
}

func New() (*Rig, error) {
	manifest := DefaultManifest()
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	for _, scenario := range manifest.Scenarios {
		if _, err := buildScenario(scenario.Name, 0); err != nil {
			return nil, fmt.Errorf("atlas scenario %s: %w", scenario.Name, err)
		}
	}
	registry, km, err := buildAuthority()
	if err != nil {
		return nil, err
	}
	return &Rig{
		manifest: manifest.Clone(), registry: registry, keymap: km,
		theme: atlasTheme(),
	}, nil
}

func (r *Rig) Name() string                     { return "atlas-proof-rig" }
func (r *Rig) Manifest() Manifest               { return r.manifest.Clone() }
func (r *Rig) ActionRegistry() *action.Registry { return r.registry }
func (r *Rig) Keymap() keymap.Map               { return r.keymap }

func (r *Rig) Tree(scenario string) (semantic.Node, error) {
	if !r.hasScenario(scenario) {
		return semantic.Node{}, fmt.Errorf("unsupported atlas scenario %q", scenario)
	}
	return buildScenario(scenario, 0)
}

func (r *Rig) Render(root semantic.Node, mode conformance.Mode) (string, error) {
	caps := capability.Manifest{
		OutputMode: capability.OutputPlain,
		TTY:        mode.TTY, Interactive: mode.Interactive,
		Width: mode.Width, Height: mode.Height,
		Color: capability.ColorNone, HardwareColor: capability.ColorNone,
		Unicode: capability.UnicodeASCII, ReducedMotion: mode.ReducedMotion,
		Limits: capability.Limits{MaxTreeNodes: 4096, MaxMessageBytes: 4 << 20, MaxInputBytes: 64 << 10},
	}
	if mode.Color {
		caps.Color, caps.HardwareColor = capability.ColorTrueColor, capability.ColorTrueColor
	}
	if mode.Unicode {
		caps.Unicode = capability.UnicodeFull
	}
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		return "", err
	}
	result, err := r.render(tree, caps)
	if err != nil {
		return "", err
	}
	return result.Plain, nil
}

func (r *Rig) Prepare(ctx context.Context, scenario, profile string) (Artifact, error) {
	selectedProfile, ok := r.findProfile(profile)
	if !ok {
		return Artifact{}, fmt.Errorf("unsupported atlas profile %q", profile)
	}
	selectedScenario, ok := r.findScenario(scenario)
	if !ok {
		return Artifact{}, fmt.Errorf("unsupported atlas scenario %q", scenario)
	}
	prepared, err := r.NewSession(ctx, scenario, profile, "atlas-proof-"+scenario+"-"+profile)
	if err != nil {
		return Artifact{}, err
	}
	defer prepared.Session.Close()
	snapshot, err := prepared.Session.Snapshot()
	if err != nil {
		return Artifact{}, err
	}
	semanticSnapshot := snapshot.Tree.Snapshot()
	if err := semanticSnapshot.Validate(); err != nil {
		return Artifact{}, err
	}
	result, err := render.Render(render.Request{
		Context: ctx, Tree: snapshot.Tree, Theme: prepared.Theme,
		Capabilities: prepared.Capabilities,
		Viewport:     layout.Size{Width: prepared.Capabilities.Width, Height: prepared.Capabilities.Height},
	})
	if err != nil {
		return Artifact{}, err
	}
	snapshotJSON, err := json.Marshal(semanticSnapshot)
	if err != nil {
		return Artifact{}, err
	}
	planHash := hex.EncodeToString(result.Plan.Hash[:])
	surfaceHash := result.Surface.Hash()
	actions := registryIDs(r.registry)
	keyActions := keymapIDs(r.keymap)
	artifact := Artifact{
		Scenario: scenario, Tier: selectedScenario.Tier, Profile: profile,
		OutputMode: prepared.Capabilities.OutputMode,
		Color:      prepared.Capabilities.Color.String(), Unicode: prepared.Capabilities.Unicode.String(),
		TTY: prepared.Capabilities.TTY, Interactive: prepared.Capabilities.Interactive,
		ScreenReader: prepared.Capabilities.ScreenReader, ReducedMotion: prepared.Capabilities.ReducedMotion,
		Viewport: result.Viewport, NodeCount: countNodes(snapshot.Tree.Root()),
		TreeHash: snapshot.Tree.Hash(), SnapshotHash: hashBytes(snapshotJSON), ThemeHash: result.ThemeHash,
		PlanHash: planHash, SurfaceHash: hex.EncodeToString(surfaceHash[:]),
		PlainHash: hashBytes([]byte(result.Plain)), MachineHash: hashBytes(result.Machine), TerminalHash: hashBytes([]byte(result.Terminal)),
		ActionIDs: actions, KeymapActionIDs: keyActions,
		ContainsANSI: strings.ContainsRune(result.Terminal, 0x1b), PlainASCII: isASCII(result.Plain),
		Plain: result.Plain, Machine: append([]byte(nil), result.Machine...), Terminal: result.Terminal,
	}
	if err := artifact.validate(selectedProfile, semanticSnapshot); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func (r *Rig) Matrix(ctx context.Context) (Matrix, error) {
	manifestJSON, err := ManifestJSON(r.manifest)
	if err != nil {
		return Matrix{}, err
	}
	matrix := Matrix{
		SchemaVersion: ManifestSchemaVersion, ManifestHash: hashBytes(manifestJSON),
		Scenarios: len(r.manifest.Scenarios), Profiles: len(r.manifest.Profiles),
		Artifacts: make([]Artifact, 0, len(r.manifest.Scenarios)*len(r.manifest.Profiles)),
	}
	for _, scenario := range r.manifest.Scenarios {
		for _, profile := range r.manifest.Profiles {
			artifact, err := r.Prepare(ctx, scenario.Name, profile.Name)
			if err != nil {
				return Matrix{}, fmt.Errorf("atlas matrix %s/%s: %w", scenario.Name, profile.Name, err)
			}
			matrix.Artifacts = append(matrix.Artifacts, artifact)
		}
	}
	return matrix, nil
}

func (r *Rig) Verify(ctx context.Context) error {
	if err := r.manifest.Validate(); err != nil {
		return err
	}
	fixtures := make([]conformance.ClientFixture, 0, len(r.manifest.Scenarios))
	modes := make([]conformance.Mode, 0, len(r.manifest.Profiles))
	for _, profile := range r.manifest.Profiles {
		modes = append(modes, profile.Mode())
	}
	for _, scenario := range r.manifest.Scenarios {
		root, err := r.Tree(scenario.Name)
		if err != nil {
			return err
		}
		fixtures = append(fixtures, conformance.ClientFixture{Name: scenario.Name, Tree: root, Modes: modes})
	}
	report := conformance.CheckClient(r, fixtures)
	if len(report.Failures) > 0 {
		return fmt.Errorf("atlas client conformance failed: %+v", report.Failures)
	}
	first, err := r.Matrix(ctx)
	if err != nil {
		return err
	}
	second, err := r.Matrix(ctx)
	if err != nil {
		return err
	}
	left, err := json.Marshal(first)
	if err != nil {
		return err
	}
	right, err := json.Marshal(second)
	if err != nil {
		return err
	}
	if !bytes.Equal(left, right) {
		return errors.New("atlas proof matrix is nondeterministic")
	}
	return nil
}

func (r *Rig) render(tree semantic.Tree, caps capability.Manifest) (render.Result, error) {
	resolved, err := r.theme.Resolve(theme.ModeDark, theme.DensityComfortable, caps)
	if err != nil {
		return render.Result{}, err
	}
	return render.Render(render.Request{
		Context: context.Background(), Tree: tree, Theme: resolved, Capabilities: caps,
		Viewport: layout.Size{Width: caps.Width, Height: caps.Height},
	})
}

func (a Artifact) validate(profile Profile, snapshot semantic.Snapshot) error {
	if a.TreeHash == "" || a.SnapshotHash == "" || a.ThemeHash == "" || a.NodeCount == 0 {
		return errors.New("atlas artifact is missing deterministic identity")
	}
	if a.TreeHash != snapshot.TreeHash {
		return errors.New("atlas artifact tree hash diverged from semantic snapshot")
	}
	if !slices.Equal(a.ActionIDs, a.KeymapActionIDs) {
		return errors.New("atlas semantic actions diverged from independent registry/keymap authority")
	}
	if a.Viewport.Width != profile.Capabilities.Width || a.Viewport.Height != profile.Capabilities.Height {
		return errors.New("atlas artifact viewport diverged from profile")
	}
	if a.OutputMode != profile.Capabilities.OutputMode || a.Color != profile.Capabilities.Color.String() || a.Unicode != profile.Capabilities.Unicode.String() || a.TTY != profile.Capabilities.TTY || a.Interactive != profile.Capabilities.Interactive || a.ScreenReader != profile.Capabilities.ScreenReader || a.ReducedMotion != profile.Capabilities.ReducedMotion {
		return fmt.Errorf("atlas artifact capabilities diverged from profile %s", profile.Name)
	}
	var machine struct {
		TreeHash string `json:"treeHash"`
		Plain    string `json:"plain"`
	}
	if err := json.Unmarshal(a.Machine, &machine); err != nil {
		return fmt.Errorf("atlas machine output: %w", err)
	}
	if machine.TreeHash != a.TreeHash || machine.Plain != a.Plain {
		return errors.New("atlas machine, plain and semantic projections diverged")
	}
	shouldContainANSI := profile.Capabilities.TTY && profile.Capabilities.Color != capability.ColorNone
	if a.ContainsANSI != shouldContainANSI {
		return fmt.Errorf("atlas profile %s ANSI=%t want %t", profile.Name, a.ContainsANSI, shouldContainANSI)
	}
	if profile.Capabilities.Unicode == capability.UnicodeASCII && !a.PlainASCII {
		return fmt.Errorf("atlas profile %s emitted non-ASCII plain output", profile.Name)
	}
	if strings.Contains(a.Plain, "Lopper") || strings.Contains(a.Terminal, "Lopper") {
		return errors.New("atlas output contains Lopper product language")
	}
	if profile.Name == "ascii-narrow" {
		for _, line := range strings.Split(a.Terminal, "\n") {
			if utf8.RuneCountInString(line) > profile.Capabilities.Width {
				return fmt.Errorf("atlas narrow output exceeds %d cells", profile.Capabilities.Width)
			}
		}
	}
	return nil
}

func (r *Rig) findScenario(name string) (Scenario, bool) {
	for _, scenario := range r.manifest.Scenarios {
		if scenario.Name == name {
			return scenario, true
		}
	}
	return Scenario{}, false
}

func (r *Rig) hasScenario(name string) bool { _, ok := r.findScenario(name); return ok }

func (r *Rig) findProfile(name string) (Profile, bool) {
	for _, profile := range r.manifest.Profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return Profile{}, false
}

func registryIDs(registry *action.Registry) []string {
	definitions := registry.Manifest()
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, string(definition.ID))
	}
	sort.Strings(ids)
	return ids
}

func keymapIDs(km keymap.Map) []string {
	ids := make([]string, 0, len(km.ActionIDs()))
	for _, id := range km.ActionIDs() {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	return ids
}

func countNodes(root semantic.Node) int {
	count := 1
	for _, child := range root.Children() {
		count += countNodes(child)
	}
	return count
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 0x7f {
			return false
		}
	}
	return true
}

var _ conformance.Client = (*Rig)(nil)
