package layout

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ben-ranford/stave/internal/width"
	"github.com/ben-ranford/stave/semantic"
)

type Size struct{ Width, Height int }
type Rect struct{ X, Y, Width, Height int }

type Constraints struct {
	MinWidth, MaxWidth   int
	MinHeight, MaxHeight int
}

type Measurement struct {
	Size    Size
	Content Size
	Nodes   int
	Hash    [32]byte
}

type Box struct {
	NodeID     semantic.NodeID
	Generation uint32
	Rect       Rect
	Clip       Rect
	Z          int
}

type Plan struct {
	Viewport Size
	Window   Rect
	Content  Size
	Boxes    []Box
	Hash     [32]byte
}

type Budgets struct {
	MaxNodes int
	MaxDepth int
	MaxWork  int
}

type Options struct {
	Budgets  Budgets
	Window   Rect
	TabWidth int
}

type Engine interface {
	Measure(context.Context, semantic.Node, Constraints) (Measurement, error)
	Arrange(context.Context, semantic.Node, Rect) (Plan, error)
}

type DefaultEngine struct {
	Options Options
}

func MeasureText(s string) int {
	return width.String(width.NormalizeTabs(s, DefaultOptions().TabWidth))
}

func DefaultOptions() Options {
	return Options{
		Budgets:  Budgets{MaxNodes: 100000, MaxDepth: 1024, MaxWork: 1000000},
		TabWidth: width.DefaultPolicy.TabWidth,
	}
}

func Arrange(ctx context.Context, root semantic.Node, viewport Size, maxNodes int) (Plan, error) {
	opts := DefaultOptions()
	if maxNodes > 0 {
		opts.Budgets.MaxNodes = maxNodes
		opts.Budgets.MaxWork = max(maxNodes*16, maxNodes)
	}
	engine := DefaultEngine{Options: opts}
	return engine.Arrange(ctx, root, Rect{Width: viewport.Width, Height: viewport.Height})
}

func (e DefaultEngine) Measure(ctx context.Context, root semantic.Node, constraints Constraints) (Measurement, error) {
	state, err := e.prepare(ctx, root)
	if err != nil {
		return Measurement{}, err
	}
	defer state.release()
	constraints, err = normalizeConstraints(constraints)
	if err != nil {
		return Measurement{}, err
	}
	size, err := state.measureNode(ctx, state.root, constraints, 0)
	if err != nil {
		return Measurement{}, err
	}
	var hashbuf []byte
	hashbuf = appendRectSize(hashbuf, size)
	binaryHash := sha256.Sum256(hashbuf)
	return Measurement{
		Size:    size,
		Content: state.root.measured,
		Nodes:   state.nodes,
		Hash:    binaryHash,
	}, nil
}

func (e DefaultEngine) Arrange(ctx context.Context, root semantic.Node, rect Rect) (Plan, error) {
	state, err := e.prepare(ctx, root)
	if err != nil {
		return Plan{}, err
	}
	defer state.release()
	if rect.Width < 0 || rect.Height < 0 {
		return Plan{}, fmt.Errorf("invalid rect")
	}
	constraints, err := normalizeConstraints(Constraints{
		MinWidth:  clampNonNegative(rect.Width),
		MaxWidth:  clampNonNegative(rect.Width),
		MinHeight: clampNonNegative(rect.Height),
		MaxHeight: clampNonNegative(rect.Height),
	})
	if err != nil {
		return Plan{}, err
	}
	rootSize, err := state.measureNode(ctx, state.root, constraints, 0)
	if err != nil {
		return Plan{}, err
	}
	window := rect
	if e.Options.Window.Width > 0 || e.Options.Window.Height > 0 {
		window = intersect(rect, e.Options.Window)
	}
	plan := Plan{
		Viewport: Size{Width: rect.Width, Height: rect.Height},
		Window:   window,
		Content:  rootSize,
		Boxes:    make([]Box, 0, state.nodes),
	}
	if err := state.arrangeNode(ctx, &plan, state.root, Rect{X: rect.X, Y: rect.Y, Width: rootSize.Width, Height: rootSize.Height}, window, 0); err != nil {
		return Plan{}, err
	}
	plan.Hash = hashPlan(plan)
	return plan, nil
}

type engineState struct {
	options   Options
	root      *nodeInfo
	nodes     int
	work      int
	allocated []*nodeInfo
}

type nodeInfo struct {
	node      semantic.Node
	children  []*nodeInfo
	inline    [4]*nodeInfo
	meta      nodeMeta
	measured  Size
	intrinsic Size
	flags     semantic.Flags
	hidden    bool
}

type nodeMeta struct {
	kind                 string
	direction            string
	gap                  int
	flex                 int
	minWidth, maxWidth   int
	minHeight, maxHeight int
	width, height        int
	scrollX, scrollY     int
	z                    int
	columns              []track
	activeIndex          int
	when                 string
	caseValue            string
	inset                edgeInsets
}

type track struct {
	kind   string
	value  int
	weight int
}

type edgeInsets struct {
	Top, Right, Bottom, Left int
}

var nodeInfoPool = sync.Pool{New: func() any { return new(nodeInfo) }}

const maxPooledHashBuffer = 512 << 10

var planHashBufferPool = sync.Pool{New: func() any {
	buf := make([]byte, 0, 64)
	return &buf
}}

func (e DefaultEngine) prepare(ctx context.Context, root semantic.Node) (*engineState, error) {
	opts := e.Options
	if opts.TabWidth <= 0 {
		opts.TabWidth = DefaultOptions().TabWidth
	}
	if opts.Budgets.MaxNodes <= 0 {
		opts.Budgets.MaxNodes = DefaultOptions().Budgets.MaxNodes
	}
	if opts.Budgets.MaxDepth <= 0 {
		opts.Budgets.MaxDepth = DefaultOptions().Budgets.MaxDepth
	}
	if opts.Budgets.MaxWork <= 0 {
		opts.Budgets.MaxWork = DefaultOptions().Budgets.MaxWork
	}
	state := &engineState{options: opts, allocated: make([]*nodeInfo, 0, min(opts.Budgets.MaxNodes, 4096))}
	info, err := state.build(ctx, root, 0)
	if err != nil {
		state.release()
		return nil, err
	}
	state.root = info
	return state, nil
}

func (s *engineState) build(ctx context.Context, node semantic.Node, depth int) (*nodeInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > s.options.Budgets.MaxDepth {
		return nil, fmt.Errorf("layout depth limit exceeded")
	}
	s.nodes++
	if s.nodes > s.options.Budgets.MaxNodes {
		return nil, fmt.Errorf("layout node limit exceeded")
	}
	flags := node.Flags()
	info := nodeInfoPool.Get().(*nodeInfo)
	*info = nodeInfo{
		node:   node,
		meta:   parseMeta(node),
		flags:  flags,
		hidden: !flags.Visible || flags.Offscreen,
	}
	info.children = info.inline[:0]
	s.allocated = append(s.allocated, info)
	if info.hidden {
		return info, nil
	}
	for index := 0; index < node.ChildCount(); index++ {
		child, _ := node.Child(index)
		next, err := s.build(ctx, child, depth+1)
		if err != nil {
			return nil, err
		}
		if next.hidden {
			continue
		}
		info.children = append(info.children, next)
	}
	return info, nil
}

func (s *engineState) release() {
	for _, info := range s.allocated {
		*info = nodeInfo{}
		nodeInfoPool.Put(info)
	}
	s.allocated = nil
	s.root = nil
}

func (s *engineState) measureNode(ctx context.Context, info *nodeInfo, constraints Constraints, depth int) (Size, error) {
	if err := ctx.Err(); err != nil {
		return Size{}, err
	}
	if info.hidden {
		info.measured = Size{}
		return Size{}, nil
	}
	s.work++
	if s.work > s.options.Budgets.MaxWork {
		return Size{}, fmt.Errorf("layout work budget exceeded")
	}
	constraints, err := normalizeConstraints(constraints)
	if err != nil {
		return Size{}, err
	}
	meta := info.meta
	intrinsic := s.intrinsicSize(info)
	info.intrinsic = intrinsic
	if len(info.children) == 0 {
		info.measured = clampSize(applyExplicit(meta, intrinsic), constraints, meta)
		return info.measured, nil
	}
	switch meta.kind {
	case "row":
		meta.direction = "horizontal"
		info.measured, err = s.measureFlexLike(ctx, info, constraints, meta, false)
	case "flex":
		info.measured, err = s.measureFlexLike(ctx, info, constraints, meta, true)
	case "split":
		info.measured, err = s.measureFlexLike(ctx, info, constraints, meta, true)
	case "grid":
		info.measured, err = s.measureGrid(ctx, info, constraints)
	case "records":
		info.measured, err = s.measureStack(ctx, info, constraints, meta)
	case "overlay":
		info.measured, err = s.measureOverlay(ctx, info, constraints, meta)
	case "inset":
		info.measured, err = s.measureInset(ctx, info, constraints, meta)
	case "conditional":
		info.measured, err = s.measureConditional(ctx, info, constraints, meta)
	case "scroll":
		if len(info.children) == 0 {
			info.measured = clampSize(applyExplicit(meta, intrinsic), constraints, meta)
			return info.measured, nil
		}
		size, measureErr := s.measureNode(ctx, info.children[0], unconstrainedWithin(constraints), depth+1)
		if measureErr != nil {
			return Size{}, measureErr
		}
		size = applyExplicit(meta, size)
		info.measured = clampSize(size, constraints, meta)
		info.measured.Width = min(info.measured.Width, max(constraints.MinWidth, constraints.MaxWidth))
		info.measured.Height = min(info.measured.Height, max(constraints.MinHeight, constraints.MaxHeight))
	default:
		info.measured, err = s.measureStack(ctx, info, constraints, meta)
	}
	if err != nil {
		return Size{}, err
	}
	return info.measured, nil
}

func (s *engineState) intrinsicSize(info *nodeInfo) Size {
	policy := width.Policy{TabWidth: s.options.TabWidth, InvalidWidth: width.DefaultPolicy.InvalidWidth, EmojiWide: width.DefaultPolicy.EmojiWide}
	name := info.node.Name()
	value := info.node.Value()
	measuredWidth := 0
	nameWidth, plainName := plainASCIIWidth(name)
	valueWidth, plainValue := plainASCIIWidth(value.Text)
	if plainName && plainValue {
		measuredWidth = nameWidth
		if value.HasValue && !value.Redacted && value.Text != "" {
			if name != "" {
				measuredWidth += 2
			}
			measuredWidth += valueWidth
		}
	} else if strings.ContainsAny(name, "\t\n\r") || strings.ContainsAny(value.Text, "\t\n\r") {
		measuredWidth = policy.StringWidth(width.NormalizeTabs(nodeLine(info.node), s.options.TabWidth))
	} else {
		if name != "" {
			measuredWidth = policy.StringWidth(name)
		}
		if value.HasValue && !value.Redacted && value.Text != "" {
			if name != "" {
				measuredWidth += 2
			}
			measuredWidth += policy.StringWidth(value.Text)
		}
	}
	if measuredWidth == 0 {
		measuredWidth = policy.StringWidth(string(info.node.Role()))
	}
	size := Size{Width: measuredWidth, Height: 1}
	if info.meta.width > 0 {
		size.Width = info.meta.width
	}
	if info.meta.height > 0 {
		size.Height = info.meta.height
	}
	return clampMeta(size, info.meta)
}

func plainASCIIWidth(value string) (int, bool) {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] >= 0x7f {
			return 0, false
		}
	}
	return len(value), true
}

func (s *engineState) measureStack(ctx context.Context, info *nodeInfo, constraints Constraints, meta nodeMeta) (Size, error) {
	totalHeight := 0
	maxWidth := info.intrinsic.Width
	for i, child := range info.children {
		childConstraints := unconstrainedWithin(constraints)
		size, err := s.measureNode(ctx, child, childConstraints, 0)
		if err != nil {
			return Size{}, err
		}
		if i > 0 {
			totalHeight += meta.gap
		}
		totalHeight += size.Height
		maxWidth = max(maxWidth, size.Width)
	}
	return clampSize(applyExplicit(meta, Size{Width: maxWidth, Height: totalHeight}), constraints, meta), nil
}

func (s *engineState) measureOverlay(ctx context.Context, info *nodeInfo, constraints Constraints, meta nodeMeta) (Size, error) {
	size := info.intrinsic
	for _, child := range overlayChildren(info.children) {
		childSize, err := s.measureNode(ctx, child, unconstrainedWithin(constraints), 0)
		if err != nil {
			return Size{}, err
		}
		size.Width = max(size.Width, childSize.Width)
		size.Height = max(size.Height, childSize.Height)
	}
	return clampSize(applyExplicit(meta, size), constraints, meta), nil
}

func (s *engineState) measureInset(ctx context.Context, info *nodeInfo, constraints Constraints, meta nodeMeta) (Size, error) {
	content, err := s.measureStack(ctx, info, insetConstraints(constraints, meta.inset), meta)
	if err != nil {
		return Size{}, err
	}
	content.Width += meta.inset.Left + meta.inset.Right
	content.Height += meta.inset.Top + meta.inset.Bottom
	return clampSize(applyExplicit(meta, content), constraints, meta), nil
}

func (s *engineState) measureConditional(ctx context.Context, info *nodeInfo, constraints Constraints, meta nodeMeta) (Size, error) {
	active := activeChildren(info, meta)
	if len(active) == 0 {
		return clampSize(applyExplicit(meta, info.intrinsic), constraints, meta), nil
	}
	childSize, err := s.measureNode(ctx, active[0], unconstrainedWithin(constraints), 0)
	if err != nil {
		return Size{}, err
	}
	size := info.intrinsic
	size.Width = max(size.Width, childSize.Width)
	size.Height = max(size.Height, childSize.Height)
	return clampSize(applyExplicit(meta, size), constraints, meta), nil
}

func (s *engineState) measureFlexLike(ctx context.Context, info *nodeInfo, constraints Constraints, meta nodeMeta, weighted bool) (Size, error) {
	horizontal := meta.direction != "vertical"
	main := 0
	cross := 0
	if horizontal {
		cross = info.intrinsic.Height
	} else {
		cross = info.intrinsic.Width
	}
	for i, child := range info.children {
		size, err := s.measureNode(ctx, child, unconstrainedWithin(constraints), 0)
		if err != nil {
			return Size{}, err
		}
		if i > 0 {
			main += meta.gap
		}
		if horizontal {
			main += size.Width
			cross = max(cross, size.Height)
		} else {
			main += size.Height
			cross = max(cross, size.Width)
		}
		if weighted && child.meta.flex > 0 {
			main = max(main, child.meta.flex)
		}
	}
	size := Size{Width: cross, Height: main}
	if horizontal {
		size = Size{Width: main, Height: cross}
	}
	return clampSize(applyExplicit(meta, size), constraints, meta), nil
}

func (s *engineState) measureGrid(ctx context.Context, info *nodeInfo, constraints Constraints) (Size, error) {
	cols := info.meta.columns
	if len(cols) == 0 {
		cols = []track{{kind: "fr", weight: 1}}
	}
	colWidths := make([]int, len(cols))
	rowHeights := make([]int, 0, max(1, int(math.Ceil(float64(len(info.children))/float64(len(cols))))))
	for i, child := range info.children {
		size, err := s.measureNode(ctx, child, unconstrainedWithin(constraints), 0)
		if err != nil {
			return Size{}, err
		}
		col := i % len(cols)
		row := i / len(cols)
		if row >= len(rowHeights) {
			rowHeights = append(rowHeights, 0)
		}
		colWidths[col] = max(colWidths[col], size.Width)
		rowHeights[row] = max(rowHeights[row], size.Height)
	}
	totalWidth := sum(colWidths) + max(0, len(colWidths)-1)*info.meta.gap
	totalHeight := sum(rowHeights) + max(0, len(rowHeights)-1)*info.meta.gap
	return clampSize(applyExplicit(info.meta, Size{Width: totalWidth, Height: totalHeight}), constraints, info.meta), nil
}

func (s *engineState) arrangeNode(ctx context.Context, plan *Plan, info *nodeInfo, rect Rect, clip Rect, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if info.hidden {
		return nil
	}
	s.work++
	if s.work > s.options.Budgets.MaxWork {
		return fmt.Errorf("layout work budget exceeded")
	}
	rect = normalizeRect(rect)
	clip = intersect(normalizeRect(clip), rect)
	if rect.Width == 0 || rect.Height == 0 || clip.Width == 0 || clip.Height == 0 {
		if info.flags.Focusable {
			return nil
		}
	}
	plan.Boxes = append(plan.Boxes, Box{
		NodeID:     info.node.ID(),
		Generation: info.node.Generation(),
		Rect:       rect,
		Clip:       clip,
		Z:          info.meta.z,
	})
	if len(info.children) == 0 {
		return nil
	}
	switch info.meta.kind {
	case "row":
		return s.arrangeFlexLike(ctx, plan, info, rect, clip, true)
	case "flex":
		return s.arrangeFlexLike(ctx, plan, info, rect, clip, info.meta.direction != "vertical")
	case "split":
		return s.arrangeFlexLike(ctx, plan, info, rect, clip, info.meta.direction != "vertical")
	case "grid":
		return s.arrangeGrid(ctx, plan, info, rect, clip)
	case "records":
		return s.arrangeStack(ctx, plan, info, rect, clip)
	case "overlay":
		return s.arrangeOverlay(ctx, plan, info, rect, clip)
	case "inset":
		return s.arrangeInset(ctx, plan, info, rect, clip)
	case "conditional":
		return s.arrangeConditional(ctx, plan, info, rect, clip)
	case "scroll":
		if len(info.children) == 0 {
			return nil
		}
		child := info.children[0]
		content := child.measured
		childRect := Rect{
			X:      rect.X - info.meta.scrollX,
			Y:      rect.Y - info.meta.scrollY,
			Width:  max(content.Width, rect.Width),
			Height: max(content.Height, rect.Height),
		}
		return s.arrangeNode(ctx, plan, child, childRect, clip, depth+1)
	default:
		return s.arrangeStack(ctx, plan, info, rect, clip)
	}
}

func (s *engineState) arrangeFlexLike(ctx context.Context, plan *Plan, info *nodeInfo, rect Rect, clip Rect, horizontal bool) error {
	sizes := allocateFlex(info.children, rect, info.meta, horizontal)
	cursorX, cursorY := rect.X, rect.Y
	for i, child := range info.children {
		size := sizes[i]
		childRect := Rect{X: cursorX, Y: cursorY, Width: rect.Width, Height: rect.Height}
		if horizontal {
			childRect.Width = size
			cursorX += size + info.meta.gap
		} else {
			childRect.Height = size
			cursorY += size + info.meta.gap
		}
		if err := s.arrangeNode(ctx, plan, child, childRect, clip, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *engineState) arrangeStack(ctx context.Context, plan *Plan, info *nodeInfo, rect Rect, clip Rect) error {
	y := rect.Y
	for i, child := range info.children {
		if i > 0 {
			y += info.meta.gap
		}
		h := min(child.measured.Height, max(0, rect.Height-(y-rect.Y)))
		childRect := Rect{X: rect.X, Y: y, Width: rect.Width, Height: h}
		if err := s.arrangeNode(ctx, plan, child, childRect, clip, 0); err != nil {
			return err
		}
		y += h
	}
	return nil
}

func (s *engineState) arrangeOverlay(ctx context.Context, plan *Plan, info *nodeInfo, rect Rect, clip Rect) error {
	for _, child := range overlayChildren(info.children) {
		if err := s.arrangeNode(ctx, plan, child, rect, clip, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *engineState) arrangeInset(ctx context.Context, plan *Plan, info *nodeInfo, rect Rect, clip Rect) error {
	content := insetRect(rect, info.meta.inset)
	if content.Width == 0 || content.Height == 0 {
		return nil
	}
	return s.arrangeStack(ctx, plan, info, content, intersect(clip, content))
}

func (s *engineState) arrangeConditional(ctx context.Context, plan *Plan, info *nodeInfo, rect Rect, clip Rect) error {
	active := activeChildren(info, info.meta)
	if len(active) == 0 {
		return nil
	}
	return s.arrangeNode(ctx, plan, active[0], rect, clip, 0)
}

func (s *engineState) arrangeGrid(ctx context.Context, plan *Plan, info *nodeInfo, rect Rect, clip Rect) error {
	cols := info.meta.columns
	if len(cols) == 0 {
		cols = []track{{kind: "fr", weight: 1}}
	}
	widths := allocateTracks(cols, rect.Width, info.meta.gap, info.children)
	rowHeights := make([]int, 0, max(1, len(info.children)/len(cols)+1))
	for i, child := range info.children {
		row := i / len(cols)
		if row >= len(rowHeights) {
			rowHeights = append(rowHeights, 0)
		}
		rowHeights[row] = max(rowHeights[row], child.measured.Height)
	}
	y := rect.Y
	for row := 0; row < len(rowHeights); row++ {
		x := rect.X
		for col := 0; col < len(cols); col++ {
			idx := row*len(cols) + col
			if idx >= len(info.children) {
				break
			}
			childRect := Rect{X: x, Y: y, Width: widths[col], Height: rowHeights[row]}
			if err := s.arrangeNode(ctx, plan, info.children[idx], childRect, clip, 0); err != nil {
				return err
			}
			x += widths[col] + info.meta.gap
		}
		y += rowHeights[row] + info.meta.gap
	}
	return nil
}

func allocateFlex(children []*nodeInfo, rect Rect, meta nodeMeta, horizontal bool) []int {
	totalGap := max(0, len(children)-1) * meta.gap
	available := rect.Height - totalGap
	if horizontal {
		available = rect.Width - totalGap
	}
	if available < 0 {
		available = 0
	}
	sizes := make([]int, len(children))
	flexTotal := 0
	used := 0
	for i, child := range children {
		base := child.measured.Height
		if horizontal {
			base = child.measured.Width
		}
		if child.meta.flex > 0 {
			flexTotal += child.meta.flex
			sizes[i] = base
			used += base
			continue
		}
		sizes[i] = min(base, available)
		used += sizes[i]
	}
	leftover := max(0, available-used)
	if flexTotal == 0 {
		return sizes
	}
	for i, child := range children {
		if child.meta.flex <= 0 {
			continue
		}
		share := leftover * child.meta.flex / flexTotal
		sizes[i] += share
	}
	consumed := sum(sizes)
	for consumed < available && len(sizes) > 0 {
		for i, child := range children {
			if child.meta.flex <= 0 || consumed >= available {
				continue
			}
			sizes[i]++
			consumed++
		}
	}
	return sizes
}

func allocateTracks(cols []track, widthValue, gap int, children []*nodeInfo) []int {
	totalGap := max(0, len(cols)-1) * gap
	available := max(0, widthValue-totalGap)
	widths := make([]int, len(cols))
	fracWeight := 0
	used := 0
	for i, col := range cols {
		switch col.kind {
		case "fixed":
			widths[i] = col.value
		case "auto":
			for childIndex := i; childIndex < len(children); childIndex += len(cols) {
				widths[i] = max(widths[i], children[childIndex].measured.Width)
			}
		case "fr":
			fracWeight += max(1, col.weight)
		}
		used += widths[i]
	}
	leftover := max(0, available-used)
	if fracWeight == 0 {
		return widths
	}
	for i, col := range cols {
		if col.kind != "fr" {
			continue
		}
		widths[i] = leftover * max(1, col.weight) / fracWeight
	}
	consumed := sum(widths)
	for consumed < available {
		for i, col := range cols {
			if col.kind != "fr" || consumed >= available {
				continue
			}
			widths[i]++
			consumed++
		}
	}
	return widths
}

func parseMeta(n semantic.Node) nodeMeta {
	meta := nodeMeta{
		kind:        "stack",
		direction:   "vertical",
		flex:        0,
		activeIndex: -1,
	}
	if n.ChildCount() == 0 {
		meta.kind = "leaf"
	}
	layoutSpec := n.Layout()
	meta.width, meta.height = layoutSpec.Width, layoutSpec.Height
	if n.MetadataLen() == 0 {
		return meta
	}
	if n.MetadataLen() > 0 {
		n.RangeMetadata(func(key, value string) bool {
			switch key {
			case "layout.kind":
				meta.kind = strings.ToLower(strings.TrimSpace(value))
			case "kind":
				if meta.kind == "stack" || meta.kind == "leaf" {
					meta.kind = strings.ToLower(strings.TrimSpace(value))
				}
			case "layout.direction":
				meta.direction = strings.ToLower(strings.TrimSpace(value))
			case "direction":
				if meta.direction == "vertical" {
					meta.direction = strings.ToLower(strings.TrimSpace(value))
				}
			case "layout.gap":
				meta.gap = atoi(value)
			case "gap":
				if meta.gap == 0 {
					meta.gap = atoi(value)
				}
			case "layout.flex":
				meta.flex = max(0, atoi(value))
			case "layout.minWidth":
				meta.minWidth = max(0, atoi(value))
			case "layout.maxWidth":
				meta.maxWidth = max(0, atoi(value))
			case "layout.minHeight":
				meta.minHeight = max(0, atoi(value))
			case "layout.maxHeight":
				meta.maxHeight = max(0, atoi(value))
			case "layout.scrollX":
				meta.scrollX = max(0, atoi(value))
			case "layout.scrollY":
				meta.scrollY = max(0, atoi(value))
			case "layout.z":
				meta.z = atoi(value)
			case "layout.columns":
				meta.columns = parseTracks(value)
			case "layout.active":
				meta.activeIndex = atoi(value)
			case "layout.when":
				meta.when = strings.TrimSpace(value)
			case "layout.case":
				meta.caseValue = strings.TrimSpace(value)
			case "layout.inset", "layout.padding", "padding":
				meta.inset = parseInsets(value)
			case "layout.insetTop", "layout.paddingTop":
				meta.inset.Top = max(0, atoi(value))
			case "layout.insetRight", "layout.paddingRight":
				meta.inset.Right = max(0, atoi(value))
			case "layout.insetBottom", "layout.paddingBottom":
				meta.inset.Bottom = max(0, atoi(value))
			case "layout.insetLeft", "layout.paddingLeft":
				meta.inset.Left = max(0, atoi(value))
			}
			return true
		})
	}
	if meta.kind == "row" {
		meta.direction = "horizontal"
	}
	if meta.kind == "stack" && meta.direction == "horizontal" {
		meta.kind = "row"
	}
	if meta.kind == "grid" && len(meta.columns) == 0 {
		meta.columns = []track{{kind: "fr", weight: 1}}
	}
	if meta.kind == "split" && meta.direction == "" {
		meta.direction = "horizontal"
	}
	if meta.kind == "records" {
		meta.direction = "vertical"
	}
	return meta
}

func nodeLine(n semantic.Node) string {
	line := n.Name()
	if v := n.Value(); v.HasValue && !v.Redacted && v.Text != "" {
		if line != "" {
			line += ": "
		}
		line += v.Text
	}
	if line == "" {
		line = string(n.Role())
	}
	return line
}

func parseInsets(raw string) edgeInsets {
	if strings.TrimSpace(raw) == "" {
		return edgeInsets{}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		values = append(values, max(0, atoi(part)))
	}
	switch len(values) {
	case 1:
		return edgeInsets{Top: values[0], Right: values[0], Bottom: values[0], Left: values[0]}
	case 2:
		return edgeInsets{Top: values[0], Right: values[1], Bottom: values[0], Left: values[1]}
	case 3:
		return edgeInsets{Top: values[0], Right: values[1], Bottom: values[2], Left: values[1]}
	case 4:
		return edgeInsets{Top: values[0], Right: values[1], Bottom: values[2], Left: values[3]}
	default:
		return edgeInsets{}
	}
}

func insetConstraints(c Constraints, in edgeInsets) Constraints {
	widthInset := in.Left + in.Right
	heightInset := in.Top + in.Bottom
	c.MinWidth = max(0, c.MinWidth-widthInset)
	c.MaxWidth = max(0, c.MaxWidth-widthInset)
	c.MinHeight = max(0, c.MinHeight-heightInset)
	c.MaxHeight = max(0, c.MaxHeight-heightInset)
	return c
}

func insetRect(r Rect, in edgeInsets) Rect {
	return normalizeRect(Rect{
		X:      r.X + in.Left,
		Y:      r.Y + in.Top,
		Width:  max(0, r.Width-in.Left-in.Right),
		Height: max(0, r.Height-in.Top-in.Bottom),
	})
}

func activeChildren(info *nodeInfo, meta nodeMeta) []*nodeInfo {
	if len(info.children) == 0 {
		return nil
	}
	if meta.when != "" {
		for _, child := range info.children {
			if child.meta.caseValue == meta.when {
				return []*nodeInfo{child}
			}
		}
		return nil
	}
	if meta.activeIndex >= 0 && meta.activeIndex < len(info.children) {
		return []*nodeInfo{info.children[meta.activeIndex]}
	}
	return info.children[:1]
}

func overlayChildren(children []*nodeInfo) []*nodeInfo {
	out := append([]*nodeInfo(nil), children...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].meta.z < out[j].meta.z
	})
	return out
}

func parseTracks(raw string) []track {
	parts := strings.Split(raw, ",")
	out := make([]track, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		switch {
		case part == "", part == "auto", part == "intrinsic":
			out = append(out, track{kind: "auto"})
		case strings.HasSuffix(part, "fr"):
			out = append(out, track{kind: "fr", weight: max(1, atoi(strings.TrimSuffix(part, "fr")))})
		default:
			out = append(out, track{kind: "fixed", value: max(0, atoi(part))})
		}
	}
	return out
}

func normalizeConstraints(c Constraints) (Constraints, error) {
	if c.MinWidth < 0 || c.MinHeight < 0 || c.MaxWidth < 0 || c.MaxHeight < 0 {
		return Constraints{}, fmt.Errorf("negative constraint")
	}
	if c.MaxWidth == 0 {
		c.MaxWidth = math.MaxInt / 4
	}
	if c.MaxHeight == 0 {
		c.MaxHeight = math.MaxInt / 4
	}
	if c.MinWidth > c.MaxWidth || c.MinHeight > c.MaxHeight {
		return Constraints{}, fmt.Errorf("invalid constraint range")
	}
	return c, nil
}

func unconstrainedWithin(c Constraints) Constraints {
	return Constraints{MaxWidth: c.MaxWidth, MaxHeight: c.MaxHeight}
}

func applyExplicit(meta nodeMeta, size Size) Size {
	if meta.width > 0 {
		size.Width = meta.width
	}
	if meta.height > 0 {
		size.Height = meta.height
	}
	return clampMeta(size, meta)
}

func clampMeta(size Size, meta nodeMeta) Size {
	if meta.minWidth > 0 {
		size.Width = max(size.Width, meta.minWidth)
	}
	if meta.maxWidth > 0 {
		size.Width = min(size.Width, meta.maxWidth)
	}
	if meta.minHeight > 0 {
		size.Height = max(size.Height, meta.minHeight)
	}
	if meta.maxHeight > 0 {
		size.Height = min(size.Height, meta.maxHeight)
	}
	return size
}

func clampSize(size Size, constraints Constraints, meta nodeMeta) Size {
	size = clampMeta(size, meta)
	size.Width = min(max(size.Width, constraints.MinWidth), constraints.MaxWidth)
	size.Height = min(max(size.Height, constraints.MinHeight), constraints.MaxHeight)
	return size
}

func normalizeRect(r Rect) Rect {
	if r.Width < 0 {
		r.Width = 0
	}
	if r.Height < 0 {
		r.Height = 0
	}
	return r
}

func intersect(a, b Rect) Rect {
	a = normalizeRect(a)
	b = normalizeRect(b)
	x1 := max(a.X, b.X)
	y1 := max(a.Y, b.Y)
	x2 := min(a.X+a.Width, b.X+b.Width)
	y2 := min(a.Y+a.Height, b.Y+b.Height)
	if x2 < x1 || y2 < y1 {
		return Rect{X: x1, Y: y1}
	}
	return Rect{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1}
}

func hashPlan(plan Plan) [32]byte {
	required := len(plan.Boxes)*104 + 64
	pooled := planHashBufferPool.Get().(*[]byte)
	buf := (*pooled)[:0]
	if cap(buf) < required {
		buf = make([]byte, 0, required)
	}
	buf = appendRectSize(buf, Size{Width: plan.Viewport.Width, Height: plan.Viewport.Height})
	buf = appendRect(buf, plan.Window)
	buf = appendRectSize(buf, plan.Content)
	for _, box := range plan.Boxes {
		buf = append(buf, []byte(box.NodeID)...)
		var scratch [4]byte
		binary.BigEndian.PutUint32(scratch[:], box.Generation)
		buf = append(buf, scratch[:]...)
		buf = appendRect(buf, box.Rect)
		buf = appendRect(buf, box.Clip)
		binary.BigEndian.PutUint32(scratch[:], uint32(box.Z))
		buf = append(buf, scratch[:]...)
	}
	hash := sha256.Sum256(buf)
	if cap(buf) <= maxPooledHashBuffer {
		*pooled = buf[:0]
		planHashBufferPool.Put(pooled)
	}
	return hash
}

func (b Box) GenerationCmp(other Box) int {
	switch {
	case b.Generation < other.Generation:
		return -1
	case b.Generation > other.Generation:
		return 1
	default:
		return 0
	}
}

func appendRect(buf []byte, r Rect) []byte {
	for _, v := range []int{r.X, r.Y, r.Width, r.Height} {
		var scratch [8]byte
		binary.BigEndian.PutUint64(scratch[:], uint64(v))
		buf = append(buf, scratch[:]...)
	}
	return buf
}

func appendRectSize(buf []byte, s Size) []byte {
	for _, v := range []int{s.Width, s.Height} {
		var scratch [8]byte
		binary.BigEndian.PutUint64(scratch[:], uint64(v))
		buf = append(buf, scratch[:]...)
	}
	return buf
}

func sum(vs []int) int {
	total := 0
	for _, v := range vs {
		total += v
	}
	return total
}

func atoi(v string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}

func clampNonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
