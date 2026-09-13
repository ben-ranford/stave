package render

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/surface"
	"github.com/ben-ranford/stave/theme"
)

func testNode(t *testing.T, role semantic.Role, name string, meta map[string]string, children ...semantic.Node) semantic.Node {
	t.Helper()
	return testNodeWith(t, role, name, meta, semantic.Flags{Visible: true}, semantic.StyleIntent{}, nil, children...)
}

func testNodeWith(t *testing.T, role semantic.Role, name string, meta map[string]string, flags semantic.Flags, style semantic.StyleIntent, states []semantic.State, children ...semantic.Node) semantic.Node {
	t.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "app", View: "main", Kind: string(role), Entity: name, Slot: "slot"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := semantic.NewNode(semantic.NodeSpec{
		ID:       id,
		Role:     role,
		Name:     name,
		Metadata: meta,
		Children: children,
		Flags:    flags,
		Style:    style,
		States:   states,
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func testTheme(tb testing.TB, color capability.ColorLevel, unicode capability.UnicodeLevel) theme.Resolved {
	tb.Helper()
	auto := theme.TokenSet{}
	light := theme.TokenSet{}
	dark := theme.TokenSet{}
	compact := theme.TokenSet{}
	dense := theme.TokenSet{}
	comfortable := theme.TokenSet{}
	for _, role := range theme.RequiredRoleIDs() {
		auto[role] = renderToken(role)
	}
	light["surface.canvas"] = theme.Value{Kind: theme.KindColor, Literal: "#f7f3ea"}
	dark["surface.canvas"] = theme.Value{Kind: theme.KindColor, Literal: "#101418"}
	dark["surface.overlay"] = theme.Value{Kind: theme.KindColor, Literal: "#1d252b"}
	dark["surface.inset"] = theme.Value{Kind: theme.KindColor, Literal: "#0d1114"}
	dark["domain.primary.fg"] = theme.Value{Kind: theme.KindColor, Literal: "#64c8ff"}
	dark["domain.primary.bg"] = theme.Value{Kind: theme.KindColor, Literal: "#14384b"}
	compact["space.1"] = theme.Value{Kind: theme.KindNumber, Literal: 1}
	dense["space.1"] = theme.Value{Kind: theme.KindNumber, Literal: 2}
	comfortable["space.1"] = theme.Value{Kind: theme.KindNumber, Literal: 4}
	compact["space.2"] = theme.Value{Kind: theme.KindNumber, Literal: 2}
	dense["space.2"] = theme.Value{Kind: theme.KindNumber, Literal: 4}
	comfortable["space.2"] = theme.Value{Kind: theme.KindNumber, Literal: 8}
	compact["space.3"] = theme.Value{Kind: theme.KindNumber, Literal: 3}
	dense["space.3"] = theme.Value{Kind: theme.KindNumber, Literal: 6}
	comfortable["space.3"] = theme.Value{Kind: theme.KindNumber, Literal: 10}

	resolved, err := theme.Theme{
		ID:      "render-fixture",
		Version: "v1",
		Modes: map[theme.Mode]theme.TokenSet{
			theme.ModeAuto:  auto,
			theme.ModeLight: light,
			theme.ModeDark:  dark,
		},
		Densities: map[theme.Density]theme.TokenSet{
			theme.DensityCompact:     compact,
			theme.DensityDense:       dense,
			theme.DensityComfortable: comfortable,
		},
		Glyphs: map[string]theme.GlyphSet{
			"render": {
				"truncation": {Unicode: "…", ASCII: "...", Width: 3},
			},
		},
		Assets: map[string]theme.AssetRef{
			"brand.mark":            {ID: "render.mark", Text: "Render"},
			"brand.mark.ascii":      {ID: "render.mark.ascii", Text: "R"},
			"brand.banner.terminal": {ID: "render.banner", Text: "Render Terminal"},
		},
	}.Resolve(theme.ModeDark, theme.DensityCompact, capability.Manifest{Color: color, Unicode: unicode})
	if err != nil {
		tb.Fatal(err)
	}
	return resolved
}

func renderToken(role theme.TokenID) theme.Value {
	name := string(role)
	switch {
	case strings.HasPrefix(name, "motion.duration"):
		return theme.Value{Kind: theme.KindDuration, Literal: "120ms"}
	case strings.HasPrefix(name, "motion.easing"):
		return theme.Value{Kind: theme.KindString, Literal: "ease-out"}
	case strings.HasPrefix(name, "space."), strings.HasPrefix(name, "radius."), strings.HasPrefix(name, "elevation."):
		return theme.Value{Kind: theme.KindNumber, Literal: 4}
	case strings.HasPrefix(name, "type."):
		return theme.Value{Kind: theme.KindString, Literal: "Iosevka"}
	default:
		return theme.Value{Kind: theme.KindColor, Literal: renderColor(role)}
	}
}

func renderColor(role theme.TokenID) string {
	name := string(role)
	switch {
	case strings.HasSuffix(name, ".fg") && (strings.Contains(name, "action.") || strings.Contains(name, "status.")):
		return "#000000"
	case strings.HasSuffix(name, ".bg") && strings.Contains(name, "action.primary"):
		return "#64c8ff"
	case strings.HasSuffix(name, ".bg") && strings.Contains(name, "action.secondary"):
		return "#d7dce2"
	case strings.HasSuffix(name, ".bg") && strings.Contains(name, "action.destructive"):
		return "#f14c4c"
	case strings.Contains(name, "surface.overlay"):
		return "#1c2429"
	case strings.Contains(name, "surface.inset"):
		return "#0d1114"
	case strings.Contains(name, "surface"):
		return "#141a1f"
	case strings.Contains(name, "failure"):
		return "#f14c4c"
	case strings.Contains(name, "success"):
		return "#35d08f"
	case strings.Contains(name, "advisory"):
		return "#f2c94c"
	case strings.Contains(name, "unknown"):
		return "#d7dce2"
	case strings.Contains(name, "border"), strings.Contains(name, "focus"):
		return "#7b8b99"
	case strings.Contains(name, "action.primary"), strings.Contains(name, "chart"), strings.Contains(name, "domain.primary"):
		return "#64c8ff"
	case strings.Contains(name, "link"):
		return "#88b5ff"
	case strings.Contains(name, "code"):
		return "#7dd3fc"
	case strings.Contains(name, "inverse"):
		return "#f7f3ea"
	default:
		return "#d7dce2"
	}
}

func TestANSILadderAndQuantization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		level capability.ColorLevel
		want  string
	}{
		{capability.ColorTrueColor, "\x1b[38;2;255;0;0mred\x1b[0m"},
		{capability.ColorANSI256, "\x1b[38;5;196mred\x1b[0m"},
		{capability.ColorANSI16, "\x1b[91mred\x1b[0m"},
		{capability.ColorMonochrome, "red"},
		{capability.ColorNone, "red"},
	}
	for _, tc := range cases {
		if got := ANSI("red", tc.level, "#ff0000"); got != tc.want {
			t.Fatalf("level %v: %q != %q", tc.level, got, tc.want)
		}
	}
	if got := quantize256(255, 0, 0); got != 196 {
		t.Fatalf("ansi256 mismatch: %d", got)
	}
	if got := quantize16(255, 255, 255); got != 97 {
		t.Fatalf("ansi16 mismatch: %d", got)
	}
}

func TestRenderFailsClosedOnZeroTheme(t *testing.T) {
	t.Parallel()
	root := testNode(t, "application", "App", map[string]string{"layout.kind": "stack"}, testNode(t, "text", "Visible", nil))
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Render(Request{
		Tree:     tree,
		Viewport: layout.Size{Width: 20, Height: 4},
		Capabilities: capability.Manifest{
			Width: 20, Height: 4,
			Limits: capability.Limits{MaxTreeNodes: 16, MaxMessageBytes: 1 << 20},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "render theme") {
		t.Fatalf("expected invalid theme error, got %v", err)
	}
}

func TestRenderRequestIsExplicitAndDeterministic(t *testing.T) {
	t.Parallel()
	root := testNode(t, "application", "App", map[string]string{"layout.kind": "stack"},
		testNode(t, "heading", "Build", nil),
		testNode(t, "link", "Docs", nil),
	)
	tree, err := semantic.NewTree(7, root)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		Context: context.Background(),
		Tree:    tree,
		Theme:   testTheme(t, capability.ColorTrueColor, capability.UnicodeFull),
		Capabilities: capability.Manifest{
			TTY:        true,
			Color:      capability.ColorTrueColor,
			Unicode:    capability.UnicodeFull,
			Width:      20,
			Height:     6,
			OutputMode: capability.OutputAuto,
			Limits: capability.Limits{
				MaxTreeNodes:    64,
				MaxMessageBytes: 1 << 20,
			},
		},
		Viewport: layout.Size{Width: 20, Height: 6},
		Event:    EventContext{Reason: "test", Revision: 7},
	}
	a, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}
	if a.Plan.Hash != b.Plan.Hash || a.Surface.Hash() != b.Surface.Hash() || a.Terminal != b.Terminal {
		t.Fatalf("render not deterministic")
	}
	if a.Viewport.Width != 20 || a.Event.Revision != 7 || len(a.Machine) == 0 {
		t.Fatalf("explicit request/result lost metadata: %#v", a)
	}
	var payload struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := json.Unmarshal(a.Machine, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != machineSchemaVersion {
		t.Fatalf("unexpected machine schema %q", payload.SchemaVersion)
	}
}

func TestRenderSelectedPreservesDefaultAndOmitsUnselectedProducts(t *testing.T) {
	t.Parallel()
	root := testNode(t, "application", "App", nil, testNode(t, "text", "hello", nil))
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Context: context.Background(), Tree: tree, Theme: testTheme(t, capability.ColorTrueColor, capability.UnicodeFull), Capabilities: capability.Manifest{TTY: true, Color: capability.ColorTrueColor, Unicode: capability.UnicodeFull, Width: 20, Height: 4, Limits: capability.Limits{MaxTreeNodes: 64, MaxMessageBytes: 1 << 20}}, Viewport: layout.Size{Width: 20, Height: 4}}
	all, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := RenderSelected(req, OutputAll)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, selected) {
		t.Fatal("Render default differs from OutputAll")
	}
	for _, tc := range []struct {
		name    string
		outputs Outputs
		check   func(Result) bool
	}{
		{"patch", OutputPatch, func(result Result) bool {
			return result.Patch.ToHash != ([32]byte{}) && result.Plain == "" && result.Machine == nil && result.Terminal == ""
		}},
		{"plain", OutputPlain, func(result Result) bool {
			return result.Patch.ToHash == ([32]byte{}) && result.Plain != "" && result.Machine == nil && result.Terminal == ""
		}},
		{"machine", OutputMachine, func(result Result) bool {
			return result.Patch.ToHash == ([32]byte{}) && result.Plain == "" && len(result.Machine) != 0 && result.Terminal == ""
		}},
		{"terminal", OutputTerminal, func(result Result) bool {
			return result.Patch.ToHash == ([32]byte{}) && result.Plain == "" && result.Machine == nil && result.Terminal != ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := RenderSelected(req, tc.outputs)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.check(result) {
				t.Fatalf("selection materialized unexpected products: %#v", result)
			}
		})
	}
	if _, err := RenderSelected(req, 0); err == nil {
		t.Fatal("zero output selection accepted")
	}
	if _, err := RenderSelected(req, Outputs(128)); err == nil {
		t.Fatal("unknown output selection accepted")
	}
	if _, err := RenderSelected(req, OutputTerminal|Outputs(128)); err == nil {
		t.Fatal("mixed known and unknown output selection accepted")
	}
}

func TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics(t *testing.T) {
	t.Parallel()
	root := testNode(t, "application", "Status", map[string]string{"layout.kind": "stack"},
		testNodeWith(t, "status", "OK", nil, semantic.Flags{Visible: true}, semantic.StyleIntent{Role: "success"}, []semantic.State{"success"}),
		testNode(t, "text", "pi", nil),
	)
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		Tree:  tree,
		Theme: testTheme(t, capability.ColorNone, capability.UnicodeASCII),
		Capabilities: capability.Manifest{
			TTY:        false,
			Color:      capability.ColorNone,
			Unicode:    capability.UnicodeASCII,
			Width:      12,
			Height:     4,
			OutputMode: capability.OutputMachineJSON,
			Limits: capability.Limits{
				MaxTreeNodes:    32,
				MaxMessageBytes: 1 << 20,
			},
		},
		Viewport: layout.Size{Width: 12, Height: 4},
	}
	result, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(result.Plain, 0x1b) || strings.ContainsRune(string(result.Machine), 0x1b) || strings.ContainsRune(result.Terminal, 0x1b) {
		t.Fatalf("escape leaked into non-ansi output")
	}
	if !strings.Contains(result.Plain, "Status") || !strings.Contains(result.Plain, "OK") {
		t.Fatalf("semantic loss in plain output: %q", result.Plain)
	}
}

func TestRenderConsumesStyleIntentAndStatusVariantsAcrossColorModes(t *testing.T) {
	t.Parallel()
	root := testNode(t, "application", "App", map[string]string{"layout.kind": "stack"},
		testNodeWith(t, "status", "Healthy", nil, semantic.Flags{Visible: true}, semantic.StyleIntent{Role: "success"}, []semantic.State{"success"}),
		testNodeWith(t, "text", "Accent", nil, semantic.Flags{Visible: true}, semantic.StyleIntent{Role: "accent"}, nil),
	)
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []capability.ColorLevel{
		capability.ColorTrueColor,
		capability.ColorANSI256,
		capability.ColorANSI16,
		capability.ColorMonochrome,
		capability.ColorNone,
	} {
		req := Request{
			Tree:  tree,
			Theme: testTheme(t, level, capability.UnicodeFull),
			Capabilities: capability.Manifest{
				TTY:        true,
				Color:      level,
				Unicode:    capability.UnicodeFull,
				Width:      24,
				Height:     6,
				OutputMode: capability.OutputAuto,
				Limits: capability.Limits{
					MaxTreeNodes:    32,
					MaxMessageBytes: 1 << 20,
				},
			},
			Viewport: layout.Size{Width: 24, Height: 6},
		}
		result, err := Render(req)
		if err != nil {
			t.Fatalf("level %v: %v", level, err)
		}
		if level == capability.ColorNone {
			if strings.Contains(result.Terminal, "\x1b[") {
				t.Fatalf("no-color output still emitted ansi: %q", result.Terminal)
			}
			continue
		}
		if !strings.Contains(result.Terminal, "\x1b[") {
			t.Fatalf("level %v missing ansi output", level)
		}
	}
}

func TestRenderOmitsHiddenAndOffscreenNodes(t *testing.T) {
	t.Parallel()
	root := testNode(t, "application", "App", map[string]string{"layout.kind": "stack"},
		testNode(t, "text", "Visible", nil),
		testNodeWith(t, "text", "Hidden", nil, semantic.Flags{Visible: false}, semantic.StyleIntent{}, nil),
		testNodeWith(t, "text", "Offscreen", nil, semantic.Flags{Visible: true, Offscreen: true}, semantic.StyleIntent{}, nil),
	)
	tree, err := semantic.NewTree(2, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Render(Request{
		Tree:  tree,
		Theme: testTheme(t, capability.ColorTrueColor, capability.UnicodeFull),
		Capabilities: capability.Manifest{
			TTY:        true,
			Color:      capability.ColorTrueColor,
			Unicode:    capability.UnicodeFull,
			Width:      24,
			Height:     6,
			OutputMode: capability.OutputAuto,
			Limits: capability.Limits{
				MaxTreeNodes:    32,
				MaxMessageBytes: 1 << 20,
			},
		},
		Viewport: layout.Size{Width: 24, Height: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Plain, "Hidden") || strings.Contains(result.Plain, "Offscreen") {
		t.Fatalf("hidden nodes leaked into plain output: %q", result.Plain)
	}
	if strings.Contains(result.Surface.String(), "Hidden") || strings.Contains(result.Surface.String(), "Offscreen") {
		t.Fatalf("hidden nodes leaked into surface output: %q", result.Surface.String())
	}
}

func TestWriterSanitizesHostileCellContentAndNarrowResize(t *testing.T) {
	t.Parallel()
	grid := surface.New(12, 1)
	var err error
	grid, err = grid.WithText(0, 0, "a\x1b]52;c;secret\x07b", surface.ResolvedStyle{Foreground: "#ff0000"}, "", semantic.NodeID("n1_iiiiiiiiiiiiiiiiiiiiiiiiii"), 1, layout.Rect{Width: 12, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	writer := Writer{Manifest: capability.Manifest{TTY: true, Color: capability.ColorANSI16}}
	hostile, err := writer.Render(grid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(hostile, 0x07) || strings.Contains(hostile, "secret") {
		t.Fatalf("terminal sink leaked hostile control: %q", hostile)
	}
	root := testNode(t, "application", "Root", map[string]string{"layout.kind": "stack"},
		testNode(t, "text", "safe text", nil),
		testNode(t, "text", "developer workstation", nil),
	)
	tree, err := semantic.NewTree(2, root)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		Tree:  tree,
		Theme: testTheme(t, capability.ColorANSI16, capability.UnicodeASCII),
		Capabilities: capability.Manifest{
			TTY:        true,
			Color:      capability.ColorANSI16,
			Unicode:    capability.UnicodeASCII,
			Width:      8,
			Height:     4,
			OutputMode: capability.OutputAuto,
			Limits: capability.Limits{
				MaxTreeNodes:    32,
				MaxMessageBytes: 1 << 20,
			},
		},
		Viewport: layout.Size{Width: 8, Height: 4},
	}
	result, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Terminal, "...") {
		t.Fatalf("expected narrow truncation fallback: %q", result.Terminal)
	}
	req.Viewport = layout.Size{Width: 20, Height: 4}
	req.Capabilities.Width = 20
	req.Previous = &result.Surface
	wide, err := Render(req)
	if err != nil {
		t.Fatal(err)
	}
	if wide.Surface.Hash() == result.Surface.Hash() {
		t.Fatalf("resize did not change surface hash")
	}
	if !wide.Patch.Resize || wide.Patch.Width != 20 || wide.Patch.Height != 4 {
		t.Fatalf("resize patch did not carry new surface dimensions: %#v", wide.Patch)
	}
}

func TestWriterExactANSISequenceFixture(t *testing.T) {
	t.Parallel()
	grid := surface.New(3, 1)
	var err error
	grid, err = grid.WithText(0, 0, "go", surface.ResolvedStyle{Foreground: "#ff0000", Background: "#0000ff", Bold: true}, "", semantic.NodeID("n1_hhhhhhhhhhhhhhhhhhhhhhhhhh"), 1, layout.Rect{Width: 3, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	writer := Writer{Manifest: capability.Manifest{TTY: true, Color: capability.ColorTrueColor}}
	got, err := writer.Render(grid)
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[1;38;2;255;0;0;48;2;0;0;255mgo \x1b[0m"
	if got != want {
		t.Fatalf("ansi fixture mismatch:\nwant %q\ngot  %q", want, got)
	}
}

func TestCanonicalPaintOrderUsesZBeforeTreeOrder(t *testing.T) {
	highID, _ := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "app", View: "overlay", Kind: "text", Entity: "high", Slot: "body"})
	lowID, _ := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "app", View: "overlay", Kind: "text", Entity: "low", Slot: "body"})
	boxes := []layout.Box{{NodeID: highID, Z: 5}, {NodeID: lowID, Z: 0}}
	order := map[semantic.NodeID]renderNode{
		highID: {treeOrder: 0},
		lowID:  {treeOrder: 1},
	}
	painted := canonicalPaintBoxes(boxes, order)
	if painted[0].NodeID != lowID || painted[1].NodeID != highID {
		t.Fatalf("overlay paint order ignored z: %#v", painted)
	}
}

func TestDefaultRenderByteBudgetSupportsSemanticSnapshots(t *testing.T) {
	got := normalizeBudgets(Budgets{}, capability.Manifest{}, layout.Size{Width: 80, Height: 24})
	if got.MaxBytes != 4<<20 {
		t.Fatalf("default MaxBytes = %d, want %d", got.MaxBytes, 4<<20)
	}
	limited := normalizeBudgets(Budgets{}, capability.Manifest{Limits: capability.Limits{MaxMessageBytes: 1024}}, layout.Size{Width: 80, Height: 24})
	if limited.MaxBytes != 4096 {
		t.Fatalf("minimum viable MaxBytes = %d, want 4096", limited.MaxBytes)
	}
}

func BenchmarkRender120x40(b *testing.B) {
	children := make([]semantic.Node, 0, 2000)
	for i := 0; i < 2000; i++ {
		role := semantic.Role("text")
		if i%20 == 0 {
			role = "status"
		}
		children = append(children, testNodeBench(b, role, fmt.Sprintf("leaf-%04d", i), nil))
	}
	root := testNodeBench(b, "application", "Root", map[string]string{"layout.kind": "records", "layout.gap": "0"}, children...)
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		b.Fatal(err)
	}
	req := Request{
		Tree:  tree,
		Theme: testTheme(b, capability.ColorANSI256, capability.UnicodeFull),
		Capabilities: capability.Manifest{
			TTY:        true,
			Color:      capability.ColorANSI256,
			Unicode:    capability.UnicodeFull,
			Width:      120,
			Height:     40,
			OutputMode: capability.OutputAuto,
			Limits: capability.Limits{
				MaxTreeNodes:    4096,
				MaxMessageBytes: 1 << 20,
			},
		},
		Viewport: layout.Size{Width: 120, Height: 40},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Render(req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderSelected120x40(b *testing.B) {
	children := make([]semantic.Node, 0, 2000)
	for i := 0; i < 2000; i++ {
		role := semantic.Role("text")
		if i%20 == 0 {
			role = "status"
		}
		children = append(children, testNodeBench(b, role, fmt.Sprintf("leaf-%04d", i), nil))
	}
	root := testNodeBench(b, "application", "Root", map[string]string{"layout.kind": "records", "layout.gap": "0"}, children...)
	tree, err := semantic.NewTree(1, root)
	if err != nil {
		b.Fatal(err)
	}
	req := Request{
		Tree:  tree,
		Theme: testTheme(b, capability.ColorANSI256, capability.UnicodeFull),
		Capabilities: capability.Manifest{
			TTY: true, Color: capability.ColorANSI256, Unicode: capability.UnicodeFull, Width: 120, Height: 40,
			Limits: capability.Limits{MaxTreeNodes: 4096, MaxMessageBytes: 1 << 20},
		},
		Viewport: layout.Size{Width: 120, Height: 40},
	}
	for _, tc := range []struct {
		name    string
		outputs Outputs
	}{
		{"all", OutputAll},
		{"terminal_only", OutputTerminal},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := RenderSelected(req, tc.outputs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func testNodeBench(tb testing.TB, role semantic.Role, name string, meta map[string]string, children ...semantic.Node) semantic.Node {
	tb.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "app", View: "main", Kind: string(role), Entity: name, Slot: "bench"})
	if err != nil {
		tb.Fatal(err)
	}
	n, err := semantic.NewNode(semantic.NodeSpec{
		ID:       id,
		Role:     role,
		Name:     name,
		Metadata: meta,
		Children: children,
		Flags:    semantic.Flags{Visible: true},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return n
}
