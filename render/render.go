package render

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ben-ranford/stave/capability"
	iterminal "github.com/ben-ranford/stave/internal/terminal"
	"github.com/ben-ranford/stave/internal/width"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/surface"
	"github.com/ben-ranford/stave/theme"
)

const machineSchemaVersion = "stave-render-v1"

type EventContext struct {
	Reason   string          `json:"reason,omitempty"`
	Focused  semantic.NodeID `json:"focused,omitempty"`
	Hovered  semantic.NodeID `json:"hovered,omitempty"`
	Revision uint64          `json:"revision,omitempty"`
}

type Budgets struct {
	MaxBytes int
	MaxCells int
	MaxDepth int
	MaxWork  int
}

type Request struct {
	Context      context.Context
	Tree         semantic.Tree
	Theme        theme.Resolved
	Capabilities capability.Manifest
	Viewport     layout.Size
	Window       layout.Rect
	Previous     *surface.Surface
	Event        EventContext
	Budgets      Budgets
}

type Result struct {
	Viewport     layout.Size
	Capabilities capability.Manifest
	ThemeHash    string
	Event        EventContext
	Plan         layout.Plan
	Surface      surface.Surface
	Patch        surface.Patch
	Plain        string
	Machine      []byte
	Terminal     string
}

// Outputs selects optional render products. A zero value preserves Render's
// compatibility behavior by producing every product.
type Outputs uint8

const (
	OutputPatch Outputs = 1 << iota
	OutputPlain
	OutputMachine
	OutputTerminal
	OutputAll = OutputPatch | OutputPlain | OutputMachine | OutputTerminal
)

func ANSI(text string, level capability.ColorLevel, fg string) string {
	w := Writer{Manifest: capability.Manifest{TTY: true, Color: level}}
	out, _ := w.wrapText(iterminal.Sanitize(text), surface.ResolvedStyle{Foreground: fg})
	return out
}

func Render(r Request) (Result, error) {
	return RenderSelected(r, OutputAll)
}

// RenderSelected shares validation, layout, and surface construction while
// materializing only the requested products.
func RenderSelected(r Request, outputs Outputs) (Result, error) {
	if outputs == 0 || outputs&^OutputAll != 0 {
		return Result{}, fmt.Errorf("invalid render output selection")
	}
	ctx := r.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := r.Tree.Validate(); err != nil {
		return Result{}, err
	}
	if err := validateResolvedTheme(r.Theme); err != nil {
		return Result{}, err
	}
	viewport := r.Viewport
	if viewport.Width <= 0 {
		viewport.Width = max(1, r.Capabilities.Width)
	}
	if viewport.Height <= 0 {
		viewport.Height = max(1, r.Capabilities.Height)
	}
	budgets := normalizeBudgets(r.Budgets, r.Capabilities, viewport)
	if viewport.Width*viewport.Height > budgets.MaxCells {
		return Result{}, fmt.Errorf("render cell budget exceeded")
	}
	indexed, err := indexRenderableTree(ctx, r.Tree.Root(), budgets, r.Capabilities)
	if err != nil {
		return Result{}, err
	}
	layoutEngine := layout.DefaultEngine{
		Options: layout.Options{
			Budgets: layout.Budgets{
				MaxNodes: r.Capabilities.Limits.MaxTreeNodes,
				MaxDepth: budgets.MaxDepth,
				MaxWork:  budgets.MaxWork,
			},
			Window:   r.Window,
			TabWidth: width.DefaultPolicy.TabWidth,
		},
	}
	plan, err := layoutEngine.Arrange(ctx, r.Tree.Root(), layout.Rect{Width: viewport.Width, Height: viewport.Height})
	if err != nil {
		return Result{}, err
	}
	grid, err := buildSurface(r, plan, budgets, indexed)
	if err != nil {
		return Result{}, err
	}
	var patch surface.Patch
	if outputs&OutputPatch != 0 {
		patch = surface.Diff(zeroSurface(grid, r.Previous), grid)
	}
	plain := ""
	if outputs&(OutputPlain|OutputMachine) != 0 {
		plain = renderPlain(indexed, r.Capabilities)
		if err := enforceByteBudget(plain, budgets.MaxBytes); err != nil {
			return Result{}, err
		}
	}
	machine := []byte(nil)
	if outputs&OutputMachine != 0 {
		machine, err = renderMachine(r, plan, grid, plain, budgets.MaxBytes)
		if err != nil {
			return Result{}, err
		}
	}
	terminal := ""
	if outputs&OutputTerminal != 0 {
		writer := Writer{Manifest: r.Capabilities, ByteLimit: budgets.MaxBytes}
		terminal, err = writer.Render(grid)
		if err != nil {
			return Result{}, err
		}
	}
	return Result{
		Viewport:     viewport,
		Capabilities: r.Capabilities,
		ThemeHash:    r.Theme.HashString(),
		Event:        r.Event,
		Plan:         plan,
		Surface:      grid,
		Patch:        patch,
		Plain:        plain,
		Machine:      machine,
		Terminal:     terminal,
	}, nil
}

type renderNode struct {
	node      semantic.Node
	depth     int
	treeOrder int
}

type renderIndex struct {
	byID  map[semantic.NodeID]renderNode
	nodes []renderNode
	depth int
	work  int
}

func buildSurface(r Request, plan layout.Plan, budgets Budgets, indexed renderIndex) (surface.Surface, error) {
	builder, err := surface.NewBuilder(plan.Viewport.Width, plan.Viewport.Height, budgets.MaxCells)
	if err != nil {
		return surface.Surface{}, err
	}
	for _, box := range canonicalPaintBoxes(plan.Boxes, indexed.byID) {
		record, ok := indexed.byID[box.NodeID]
		if !ok {
			continue
		}
		node := record.node
		text := nodeLine(node)
		if r.Capabilities.Unicode == capability.UnicodeASCII {
			text = width.ASCIIOnly(text)
		}
		text = width.Truncate(text, box.Clip.Width, truncationIndicator(r))
		if err := builder.WithText(
			box.Rect.X,
			box.Rect.Y,
			text,
			styleForNode(node, r.Theme),
			linkForNode(node),
			node.ID(),
			node.Generation(),
			box.Clip,
		); err != nil {
			return surface.Surface{}, err
		}
	}
	return builder.Surface(), nil
}

func renderPlain(indexed renderIndex, caps capability.Manifest) string {
	lines := make([]string, 0, len(indexed.nodes))
	for _, record := range indexed.nodes {
		line := strings.Repeat("  ", record.depth) + nodeLine(record.node)
		if caps.Unicode == capability.UnicodeASCII {
			line = width.ASCIIOnly(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func renderMachine(r Request, plan layout.Plan, grid surface.Surface, plain string, maxBytes int) ([]byte, error) {
	payload := struct {
		SchemaVersion string              `json:"schemaVersion"`
		Revision      uint64              `json:"revision"`
		TreeHash      string              `json:"treeHash"`
		Viewport      layout.Size         `json:"viewport"`
		Capabilities  capability.Manifest `json:"capabilities"`
		ThemeHash     string              `json:"themeHash"`
		Event         EventContext        `json:"event"`
		PlanHash      string              `json:"planHash"`
		SurfaceText   string              `json:"surfaceText"`
		Plain         string              `json:"plain"`
	}{
		SchemaVersion: machineSchemaVersion,
		Revision:      r.Tree.Revision(),
		TreeHash:      r.Tree.Hash(),
		Viewport:      plan.Viewport,
		Capabilities:  r.Capabilities,
		ThemeHash:     r.Theme.HashString(),
		Event:         r.Event,
		PlanHash:      fmt.Sprintf("%x", plan.Hash),
		SurfaceText:   grid.String(),
		Plain:         plain,
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if strings.ContainsRune(string(out), 0x1b) {
		return nil, fmt.Errorf("machine output contains escape")
	}
	if len(out) > maxBytes {
		return nil, fmt.Errorf("render byte budget exceeded")
	}
	return out, nil
}

func normalizeBudgets(in Budgets, caps capability.Manifest, viewport layout.Size) Budgets {
	if in.MaxBytes <= 0 {
		in.MaxBytes = caps.Limits.MaxMessageBytes
		if in.MaxBytes <= 0 {
			in.MaxBytes = 4 << 20
		} else if in.MaxBytes < 4096 {
			in.MaxBytes = 4096
		}
	}
	if in.MaxCells <= 0 {
		in.MaxCells = max(1, viewport.Width*viewport.Height)
	}
	if in.MaxDepth <= 0 {
		in.MaxDepth = 1024
	}
	if in.MaxWork <= 0 {
		in.MaxWork = max(1024, viewport.Width*viewport.Height*8)
	}
	if caps.Limits.MaxTreeNodes > 0 && in.MaxWork < caps.Limits.MaxTreeNodes {
		in.MaxWork = caps.Limits.MaxTreeNodes
	}
	return in
}

func zeroSurface(current surface.Surface, previous *surface.Surface) surface.Surface {
	if previous != nil {
		return *previous
	}
	base, _ := surface.NewBounded(current.Width, current.Height, current.Width*current.Height)
	return base
}

func nodeLine(node semantic.Node) string {
	line := node.Name()
	if line == "" {
		line = string(node.Role())
	}
	if value := node.Value(); value.HasValue && !value.Redacted && value.Text != "" {
		line += ": " + value.Text
	}
	if states := node.States(); len(states) > 0 {
		items := make([]string, len(states))
		for i, state := range states {
			items[i] = string(state)
		}
		line += " [" + strings.Join(items, ", ") + "]"
	}
	return line
}

func styleForNode(node semantic.Node, resolved theme.Resolved) surface.ResolvedStyle {
	intent := nodeStyleIntent(node)
	variant := statusVariant(node, intent)
	style := surface.ResolvedStyle{
		Foreground: tokenString(resolved, "ink.primary", "#d0d0d0"),
		Background: tokenString(resolved, "surface.canvas", "#000000"),
	}
	switch intent {
	case "muted", "secondary":
		style.Foreground = tokenString(resolved, "ink.secondary", style.Foreground)
	case "accent":
		style.Foreground = tokenString(resolved, "domain.primary.fg", style.Foreground)
		style.Background = tokenString(resolved, "domain.primary.bg", style.Background)
	case "inverse":
		style.Foreground = tokenString(resolved, "ink.inverse", style.Foreground)
		style.Background = tokenString(resolved, "surface.raised", style.Background)
	case "error", "danger", "failure":
		style = applyStatusVariant(style, resolved, "failure")
	case "success":
		style = applyStatusVariant(style, resolved, "success")
	case "advisory", "warning":
		style = applyStatusVariant(style, resolved, "advisory")
	case "unknown":
		style = applyStatusVariant(style, resolved, "unknown")
	}
	switch node.Role() {
	case "heading":
		style.Bold = true
	case "link":
		style.Foreground = tokenString(resolved, "ink.link", "#4aa3ff")
		style.Underline = true
	case "status", "alert":
		style = applyStatusVariant(style, resolved, variant)
	case "code":
		style.Foreground = tokenString(resolved, "ink.code", "#7dd3fc")
		style.Background = tokenString(resolved, "surface.inset", style.Background)
	case "button", "menuitem", "tab":
		style.Foreground = tokenString(resolved, "action.primary.fg", style.Foreground)
		style.Background = tokenString(resolved, "action.primary.bg", style.Background)
	case "dialog", "tooltip":
		style.Background = tokenString(resolved, "surface.overlay", style.Background)
	case "columnheader", "rowheader":
		style.Foreground = tokenString(resolved, "table.header", style.Foreground)
	case "textbox", "searchbox", "combobox":
		style.Background = tokenString(resolved, "surface.inset", style.Background)
	case "form":
		style.Foreground = tokenString(resolved, "form.label", style.Foreground)
	}
	if hasState(node, "focused") {
		style.Underline = true
		style.Background = tokenString(resolved, "focus.ring", style.Background)
	}
	if hasState(node, "selected") {
		style.Background = tokenString(resolved, "surface.selected", style.Background)
	}
	if hasState(node, "hovered") {
		style.Background = tokenString(resolved, "surface.hover", style.Background)
	}
	if node.Flags().Disabled {
		style.Dim = true
	}
	return style
}

func linkForNode(node semantic.Node) string {
	if node.Role() != "link" {
		return ""
	}
	for _, rel := range node.Relations() {
		if rel.Kind == "href" {
			return rel.Target.String()
		}
	}
	return node.Name()
}

func tokenString(resolved theme.Resolved, tokenID theme.TokenID, fallback string) string {
	value, ok := resolved.Token(tokenID)
	if !ok {
		return fallback
	}
	switch v := value.Value.(type) {
	case string:
		if v != "" {
			return v
		}
	case fmt.Stringer:
		return v.String()
	}
	return fallback
}

func truncationIndicator(r Request) string {
	for _, key := range []string{"truncation", "render.truncation", "truncation.default"} {
		glyph, ok := r.Theme.Glyph(key)
		if !ok {
			continue
		}
		if r.Capabilities.Unicode == capability.UnicodeASCII && glyph.ASCII != "" {
			return glyph.ASCII
		}
		if glyph.Unicode != "" {
			return glyph.Unicode
		}
	}
	if r.Capabilities.Unicode == capability.UnicodeASCII {
		return "..."
	}
	return "…"
}

func validateResolvedTheme(resolved theme.Resolved) error {
	if !resolved.Valid() {
		return fmt.Errorf("render theme is invalid")
	}
	for _, tokenID := range []theme.TokenID{
		"surface.canvas",
		"surface.raised",
		"surface.overlay",
		"surface.inset",
		"surface.selected",
		"ink.primary",
		"ink.secondary",
		"ink.inverse",
		"ink.link",
		"ink.code",
		"action.primary.fg",
		"action.primary.bg",
		"status.success.fg",
		"status.success.bg",
		"status.advisory.fg",
		"status.advisory.bg",
		"status.failure.fg",
		"status.failure.bg",
		"status.unknown.fg",
		"status.unknown.bg",
		"domain.primary.fg",
		"domain.primary.bg",
		"focus.ring",
		"table.header",
		"form.label",
	} {
		if _, ok := resolved.Token(tokenID); !ok {
			return fmt.Errorf("render theme missing token %s", tokenID)
		}
	}
	return nil
}

func indexRenderableTree(ctx context.Context, root semantic.Node, budgets Budgets, caps capability.Manifest) (renderIndex, error) {
	type frame struct {
		node  semantic.Node
		depth int
	}
	stack := []frame{{node: root, depth: 0}}
	indexed := renderIndex{
		byID: make(map[semantic.NodeID]renderNode, min(max(1, caps.Limits.MaxTreeNodes), 1024)),
	}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return renderIndex{}, err
		}
		indexed.work++
		if indexed.work > budgets.MaxWork {
			return renderIndex{}, fmt.Errorf("render work budget exceeded")
		}
		last := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if last.depth+1 > budgets.MaxDepth {
			return renderIndex{}, fmt.Errorf("render depth budget exceeded")
		}
		flags := last.node.Flags()
		if !flags.Visible || flags.Offscreen {
			continue
		}
		record := renderNode{
			node:      last.node,
			depth:     last.depth,
			treeOrder: len(indexed.nodes),
		}
		indexed.byID[last.node.ID()] = record
		indexed.nodes = append(indexed.nodes, record)
		indexed.depth = max(indexed.depth, last.depth+1)
		if caps.Limits.MaxTreeNodes > 0 && len(indexed.nodes) > caps.Limits.MaxTreeNodes {
			return renderIndex{}, fmt.Errorf("render node budget exceeded")
		}
		for i := last.node.ChildCount() - 1; i >= 0; i-- {
			child, _ := last.node.Child(i)
			stack = append(stack, frame{node: child, depth: last.depth + 1})
		}
	}
	return indexed, nil
}

func canonicalPaintBoxes(boxes []layout.Box, order map[semantic.NodeID]renderNode) []layout.Box {
	out := append([]layout.Box(nil), boxes...)
	sort.SliceStable(out, func(i, j int) bool {
		left := order[out[i].NodeID]
		right := order[out[j].NodeID]
		if out[i].Z != out[j].Z {
			return out[i].Z < out[j].Z
		}
		return left.treeOrder < right.treeOrder
	})
	return out
}

func nodeStyleIntent(node semantic.Node) string {
	return strings.ToLower(strings.TrimSpace(node.Style().Role))
}

func hasState(node semantic.Node, want semantic.State) bool {
	for _, state := range node.States() {
		if state == want {
			return true
		}
	}
	return false
}

func statusVariant(node semantic.Node, intent string) string {
	switch intent {
	case "success", "advisory", "warning", "unknown", "error", "danger", "failure":
		if intent == "error" || intent == "danger" {
			return "failure"
		}
		if intent == "warning" {
			return "advisory"
		}
		return intent
	}
	for _, state := range node.States() {
		switch strings.ToLower(string(state)) {
		case "success", "ok", "ready":
			return "success"
		case "warning", "advisory", "loading":
			return "advisory"
		case "error", "failure", "invalid":
			return "failure"
		case "unknown":
			return "unknown"
		}
	}
	return "unknown"
}

func applyStatusVariant(style surface.ResolvedStyle, resolved theme.Resolved, variant string) surface.ResolvedStyle {
	style.Foreground = tokenString(resolved, theme.TokenID("status."+variant+".fg"), style.Foreground)
	style.Background = tokenString(resolved, theme.TokenID("status."+variant+".bg"), style.Background)
	return style
}

func enforceByteBudget(s string, limit int) error {
	if limit > 0 && len(s) > limit {
		return fmt.Errorf("render byte budget exceeded")
	}
	return nil
}

type Writer struct {
	Manifest  capability.Manifest
	ByteLimit int
}

func (w Writer) Render(s surface.Surface) (string, error) {
	if !w.allowANSI() {
		out := iterminal.Sanitize(s.String())
		if err := enforceByteBudget(out, w.ByteLimit); err != nil {
			return "", err
		}
		return out, nil
	}
	var b strings.Builder
	var current surface.ResolvedStyle
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			cell := s.At(x, y)
			if cell.Continuation {
				continue
			}
			style := styleByID(s, cell.Style)
			if cell.Grapheme == "" && style == (surface.ResolvedStyle{}) && current != (surface.ResolvedStyle{}) {
				b.WriteByte(' ')
				continue
			}
			if style != current {
				if current != (surface.ResolvedStyle{}) {
					b.WriteString("\x1b[0m")
				}
				seq, err := w.styleSequence(style)
				if err != nil {
					return "", err
				}
				b.WriteString(seq)
				current = style
			}
			text := cell.Grapheme
			if text == "" {
				text = " "
			}
			b.WriteString(iterminal.Sanitize(text))
		}
		if current != (surface.ResolvedStyle{}) {
			b.WriteString("\x1b[0m")
			current = surface.ResolvedStyle{}
		}
		if y+1 < s.Height {
			b.WriteByte('\n')
		}
	}
	out := b.String()
	if err := enforceByteBudget(out, w.ByteLimit); err != nil {
		return "", err
	}
	return out, nil
}

func (w Writer) wrapText(text string, style surface.ResolvedStyle) (string, error) {
	if !w.allowANSI() {
		return text, nil
	}
	seq, err := w.styleSequence(style)
	if err != nil {
		return "", err
	}
	if seq == "" {
		return text, nil
	}
	return seq + text + "\x1b[0m", nil
}

func (w Writer) allowANSI() bool {
	if !w.Manifest.TTY {
		return false
	}
	switch w.Manifest.OutputMode {
	case capability.OutputMachineJSON, capability.OutputMachineJSONL, capability.OutputPlain, capability.OutputDumb:
		return false
	}
	return w.Manifest.Color != capability.ColorNone
}

func (w Writer) styleSequence(style surface.ResolvedStyle) (string, error) {
	var parts []string
	if style.Bold {
		parts = append(parts, "1")
	}
	if style.Dim {
		parts = append(parts, "2")
	}
	if style.Italic {
		parts = append(parts, "3")
	}
	if style.Underline {
		parts = append(parts, "4")
	}
	if style.Blink && !w.Manifest.ReducedMotion {
		parts = append(parts, "5")
	}
	if style.Reverse {
		parts = append(parts, "7")
	}
	switch w.Manifest.Color {
	case capability.ColorTrueColor:
		if fg, ok := colorSequence(style.Foreground, "38", w.Manifest.Color); ok {
			parts = append(parts, fg)
		}
		if bg, ok := colorSequence(style.Background, "48", w.Manifest.Color); ok {
			parts = append(parts, bg)
		}
	case capability.ColorANSI256:
		if fg, ok := colorSequence(style.Foreground, "38", w.Manifest.Color); ok {
			parts = append(parts, fg)
		}
		if bg, ok := colorSequence(style.Background, "48", w.Manifest.Color); ok {
			parts = append(parts, bg)
		}
	case capability.ColorANSI16:
		if fg, ok := colorSequence(style.Foreground, "38", w.Manifest.Color); ok {
			parts = append(parts, fg)
		}
		if bg, ok := colorSequence(style.Background, "48", w.Manifest.Color); ok {
			parts = append(parts, bg)
		}
	case capability.ColorMonochrome:
	default:
		return "", nil
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "\x1b[" + strings.Join(parts, ";") + "m", nil
}

func styleByID(s surface.Surface, id surface.StyleID) surface.ResolvedStyle {
	if id == 0 {
		return surface.ResolvedStyle{}
	}
	styles := s.Styles()
	index := int(id) - 1
	if index < 0 || index >= len(styles) {
		return surface.ResolvedStyle{}
	}
	return styles[index]
}

func colorSequence(color, prefix string, level capability.ColorLevel) (string, bool) {
	color = strings.TrimSpace(strings.ToLower(color))
	if color == "" {
		return "", false
	}
	if strings.HasPrefix(color, "ansi256:") {
		return prefix + ";5;" + strings.TrimPrefix(color, "ansi256:"), true
	}
	if strings.HasPrefix(color, "ansi16:") {
		code, err := strconv.Atoi(strings.TrimPrefix(color, "ansi16:"))
		if err != nil {
			return "", false
		}
		if prefix == "48" {
			return strconv.Itoa(ansi16Background(code)), true
		}
		return strconv.Itoa(ansi16Foreground(code)), true
	}
	if strings.HasPrefix(color, "mono:") {
		return "", false
	}
	r, g, b, ok := parseHex(color)
	if !ok {
		return "", false
	}
	switch level {
	case capability.ColorTrueColor:
		return prefix + ";2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b), true
	case capability.ColorANSI256:
		return prefix + ";5;" + strconv.Itoa(quantize256(r, g, b)), true
	case capability.ColorANSI16:
		code := quantize16(r, g, b)
		if prefix == "48" {
			return strconv.Itoa(ansi16Background(code)), true
		}
		return strconv.Itoa(ansi16Foreground(code)), true
	default:
		return "", false
	}
}

func parseHex(color string) (int, int, int, bool) {
	color = strings.TrimPrefix(color, "#")
	if len(color) != 6 {
		return 0, 0, 0, false
	}
	r, err := strconv.ParseInt(color[0:2], 16, 0)
	if err != nil {
		return 0, 0, 0, false
	}
	g, err := strconv.ParseInt(color[2:4], 16, 0)
	if err != nil {
		return 0, 0, 0, false
	}
	b, err := strconv.ParseInt(color[4:6], 16, 0)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(r), int(g), int(b), true
}

func quantize256(r, g, b int) int {
	if r == g && g == b {
		if r < 8 {
			return 16
		}
		if r > 248 {
			return 231
		}
		return 232 + (r-8)/10
	}
	return 16 + 36*scale6(r) + 6*scale6(g) + scale6(b)
}

func scale6(v int) int {
	return min(5, max(0, int(float64(v)/255.0*5.0+0.5)))
}

func quantize16(r, g, b int) int {
	palette := []struct {
		r, g, b int
		code    int
	}{
		{0, 0, 0, 30}, {205, 0, 0, 31}, {0, 205, 0, 32}, {205, 205, 0, 33},
		{0, 0, 238, 34}, {205, 0, 205, 35}, {0, 205, 205, 36}, {229, 229, 229, 37},
		{127, 127, 127, 90}, {255, 0, 0, 91}, {0, 255, 0, 92}, {255, 255, 0, 93},
		{92, 92, 255, 94}, {255, 0, 255, 95}, {0, 255, 255, 96}, {255, 255, 255, 97},
	}
	best := palette[0]
	bestDist := 1<<31 - 1
	for _, candidate := range palette {
		dist := sq(candidate.r-r) + sq(candidate.g-g) + sq(candidate.b-b)
		if dist < bestDist {
			best = candidate
			bestDist = dist
		}
	}
	return best.code
}

func ansi16Foreground(code int) int {
	if code >= 30 && code <= 37 || code >= 90 && code <= 97 {
		return code
	}
	return 39
}

func ansi16Background(code int) int {
	switch {
	case code >= 30 && code <= 37:
		return code + 10
	case code >= 90 && code <= 97:
		return code + 10
	default:
		return 49
	}
}

func sq(v int) int { return v * v }
