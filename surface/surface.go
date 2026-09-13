package surface

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ben-ranford/stave/internal/width"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/semantic"
)

type StyleID uint32
type LinkID uint32

type ResolvedStyle struct {
	Foreground string `json:"foreground,omitempty"`
	Background string `json:"background,omitempty"`
	Bold       bool   `json:"bold,omitempty"`
	Dim        bool   `json:"dim,omitempty"`
	Italic     bool   `json:"italic,omitempty"`
	Underline  bool   `json:"underline,omitempty"`
	Blink      bool   `json:"blink,omitempty"`
	Reverse    bool   `json:"reverse,omitempty"`
}

type Cell struct {
	Grapheme     string          `json:"grapheme,omitempty"`
	Width        uint8           `json:"width,omitempty"`
	Continuation bool            `json:"continuation,omitempty"`
	Style        StyleID         `json:"style,omitempty"`
	Link         LinkID          `json:"link,omitempty"`
	NodeID       semantic.NodeID `json:"nodeId,omitempty"`
	Generation   uint32          `json:"generation,omitempty"`
}

type Region struct {
	X, Y, Width, Height int
}

type Run struct {
	Offset int    `json:"offset"`
	Cells  []Cell `json:"cells"`
}

type Patch struct {
	FromHash [32]byte        `json:"fromHash"`
	ToHash   [32]byte        `json:"toHash"`
	Resize   bool            `json:"resize,omitempty"`
	Width    int             `json:"width,omitempty"`
	Height   int             `json:"height,omitempty"`
	Runs     []Run           `json:"runs,omitempty"`
	Dirty    []Region        `json:"dirty,omitempty"`
	Styles   []ResolvedStyle `json:"styles,omitempty"`
	Links    []string        `json:"links,omitempty"`
}

type Surface struct {
	Width, Height int
	cells         []Cell
	styles        []ResolvedStyle
	links         []string
	hash          [32]byte
}

type Builder struct {
	surface    Surface
	styleIndex map[ResolvedStyle]StyleID
	linkIndex  map[string]LinkID
}

func New(w, h int) Surface {
	s, _ := NewBounded(w, h, max(0, w*h))
	return s
}

func NewBounded(w, h, maxCells int) (Surface, error) {
	if w < 0 || h < 0 {
		return Surface{}, fmt.Errorf("invalid surface size")
	}
	if w > 0 && h > 0 && w > maxInt/h {
		return Surface{}, fmt.Errorf("surface size overflow")
	}
	cellCount := w * h
	if maxCells > 0 && cellCount > maxCells {
		return Surface{}, fmt.Errorf("surface cell budget exceeded")
	}
	s := Surface{
		Width:  w,
		Height: h,
		cells:  make([]Cell, cellCount),
	}
	s = s.finalize()
	return s, nil
}

func NewBuilder(w, h, maxCells int) (*Builder, error) {
	s, err := NewBounded(w, h, maxCells)
	if err != nil {
		return nil, err
	}
	return &Builder{
		surface:    s,
		styleIndex: map[ResolvedStyle]StyleID{},
		linkIndex:  map[string]LinkID{},
	}, nil
}

func (s Surface) Cells() []Cell {
	return append([]Cell(nil), s.cells...)
}

func (s Surface) Styles() []ResolvedStyle {
	return append([]ResolvedStyle(nil), s.styles...)
}

func (s Surface) Links() []string {
	return append([]string(nil), s.links...)
}

func (s Surface) Hash() [32]byte {
	return s.hash
}

func (s Surface) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= s.Width || y >= s.Height {
		return Cell{}
	}
	return s.cells[y*s.Width+x]
}

func (s Surface) String() string {
	var b strings.Builder
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			cell := s.At(x, y)
			if cell.Continuation {
				continue
			}
			if cell.Grapheme == "" {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(cell.Grapheme)
		}
		if y+1 < s.Height {
			b.WriteByte('\n')
		}
	}
	return strings.TrimRightFunc(b.String(), func(r rune) bool { return r == ' ' || r == '\n' })
}

func (s Surface) WithCell(x, y int, cell Cell) Surface {
	if x < 0 || y < 0 || x >= s.Width || y >= s.Height {
		return s
	}
	span := max(1, int(cell.Width))
	if span > s.Width-x {
		return s
	}
	next := s.clone()
	if cell.Width == 0 {
		cell.Width = 1
	}
	clearOwning(next.cells, s.Width, x, y)
	for i := 1; i < span && x+i < s.Width; i++ {
		clearOwning(next.cells, s.Width, x+i, y)
	}
	idx := y*s.Width + x
	next.cells[idx] = cell
	for i := 1; i < span && x+i < s.Width; i++ {
		next.cells[y*s.Width+x+i] = Cell{
			Width:        1,
			Continuation: true,
			Style:        cell.Style,
			Link:         cell.Link,
			NodeID:       cell.NodeID,
			Generation:   cell.Generation,
		}
	}
	return next.finalize()
}

func (s Surface) WithStyledGrapheme(x, y int, grapheme string, style ResolvedStyle, link string, nodeID semantic.NodeID, generation uint32, clip layout.Rect) (Surface, error) {
	clusters := width.DefaultPolicy.Clusters(grapheme)
	if len(clusters) == 0 {
		return s, nil
	}
	if len(clusters) != 1 {
		return s, fmt.Errorf("expected single grapheme cluster")
	}
	cluster := clusters[0]
	if cluster.Text == "\n" || cluster.Text == "\r" {
		return s, nil
	}
	return s.placeCluster(x, y, cluster, style, link, nodeID, generation, clip)
}

func (s Surface) WithText(x, y int, text string, style ResolvedStyle, link string, nodeID semantic.NodeID, generation uint32, clip layout.Rect) (Surface, error) {
	next := s
	cursor := x
	for _, cluster := range width.DefaultPolicy.Clusters(text) {
		if cluster.Text == "\n" || cluster.Text == "\r" {
			break
		}
		if cluster.Text == "\t" {
			cluster.Text = strings.Repeat(" ", 4)
			cluster.Width = 4
		}
		var err error
		next, err = next.placeCluster(cursor, y, cluster, style, link, nodeID, generation, clip)
		if err != nil {
			return s, err
		}
		cursor += cluster.Width
	}
	return next, nil
}

func (b *Builder) WithText(x, y int, text string, style ResolvedStyle, link string, nodeID semantic.NodeID, generation uint32, clip layout.Rect) error {
	cursor := x
	for _, cluster := range width.DefaultPolicy.Clusters(text) {
		if cluster.Text == "\n" || cluster.Text == "\r" {
			break
		}
		if cluster.Text == "\t" {
			cluster.Text = strings.Repeat(" ", 4)
			cluster.Width = 4
		}
		if err := b.placeCluster(cursor, y, cluster, style, link, nodeID, generation, clip); err != nil {
			return err
		}
		cursor += cluster.Width
	}
	return nil
}

func (b *Builder) Surface() Surface {
	return b.surface.finalize()
}

func (s *Surface) Put(x, y int, c Cell) {
	*s = s.WithCell(x, y, c)
}

func (s *Surface) Finalize() {
	*s = s.finalize()
}

func (s Surface) ApplyPatch(p Patch) (Surface, error) {
	if s.hash != p.FromHash {
		return Surface{}, fmt.Errorf("surface hash mismatch")
	}
	next := s.clone()
	if p.Resize {
		resized, err := NewBounded(p.Width, p.Height, max(0, p.Width*p.Height))
		if err != nil {
			return Surface{}, err
		}
		next = resized
	}
	next.styles = append([]ResolvedStyle(nil), p.Styles...)
	next.links = append([]string(nil), p.Links...)
	for _, run := range p.Runs {
		offset := run.Offset
		for _, cell := range run.Cells {
			if offset < 0 || offset >= len(next.cells) {
				return Surface{}, fmt.Errorf("patch offset out of bounds")
			}
			next.cells[offset] = cell
			offset++
		}
	}
	next = next.finalize()
	if next.hash != p.ToHash {
		return Surface{}, fmt.Errorf("surface result mismatch")
	}
	return next, nil
}

func (s *Surface) Apply(p Patch) error {
	next, err := s.ApplyPatch(p)
	if err != nil {
		return err
	}
	*s = next
	return nil
}

func Diff(a, b Surface) Patch {
	p := Patch{FromHash: a.hash, ToHash: b.hash}
	if a.Width != b.Width || a.Height != b.Height {
		p.Resize = true
		p.Width = b.Width
		p.Height = b.Height
		p.Runs = []Run{{Offset: 0, Cells: b.Cells()}}
		p.Dirty = []Region{{X: 0, Y: 0, Width: b.Width, Height: b.Height}}
		p.Styles = b.Styles()
		p.Links = b.Links()
		return p
	}
	for y := 0; y < a.Height; y++ {
		run := Run{Offset: -1}
		for x := 0; x < a.Width; x++ {
			idx := y*a.Width + x
			if a.cells[idx] == b.cells[idx] {
				if run.Offset >= 0 {
					p.Runs = append(p.Runs, run)
					p.Dirty = append(p.Dirty, Region{X: run.Offset % a.Width, Y: y, Width: len(run.Cells), Height: 1})
					run = Run{Offset: -1}
				}
				continue
			}
			if run.Offset < 0 {
				run.Offset = idx
			}
			run.Cells = append(run.Cells, b.cells[idx])
		}
		if run.Offset >= 0 {
			p.Runs = append(p.Runs, run)
			p.Dirty = append(p.Dirty, Region{X: run.Offset % a.Width, Y: y, Width: len(run.Cells), Height: 1})
		}
	}
	p.Styles = b.Styles()
	p.Links = b.Links()
	return p
}

func (s Surface) placeCluster(x, y int, cluster width.Cluster, style ResolvedStyle, link string, nodeID semantic.NodeID, generation uint32, clip layout.Rect) (Surface, error) {
	if y < 0 || y >= s.Height || cluster.Width == 0 {
		return s, nil
	}
	if x < 0 || x+cluster.Width > s.Width {
		return s, nil
	}
	if clip.Width > 0 || clip.Height > 0 {
		if y < clip.Y || y >= clip.Y+clip.Height || x < clip.X || x+cluster.Width > clip.X+clip.Width {
			return s, nil
		}
	}
	next, styleID := s.withInternedStyle(style)
	next, linkID := next.withInternedLink(link)
	return next.WithCell(x, y, Cell{
		Grapheme:   cluster.Text,
		Width:      uint8(cluster.Width),
		Style:      styleID,
		Link:       linkID,
		NodeID:     nodeID,
		Generation: generation,
	}), nil
}

func (b *Builder) placeCluster(x, y int, cluster width.Cluster, style ResolvedStyle, link string, nodeID semantic.NodeID, generation uint32, clip layout.Rect) error {
	if y < 0 || y >= b.surface.Height || cluster.Width == 0 {
		return nil
	}
	if x < 0 || x+cluster.Width > b.surface.Width {
		return nil
	}
	if clip.Width > 0 || clip.Height > 0 {
		if y < clip.Y || y >= clip.Y+clip.Height || x < clip.X || x+cluster.Width > clip.X+clip.Width {
			return nil
		}
	}
	styleID := b.internStyle(style)
	linkID := b.internLink(link)
	cell := Cell{
		Grapheme:   cluster.Text,
		Width:      uint8(cluster.Width),
		Style:      styleID,
		Link:       linkID,
		NodeID:     nodeID,
		Generation: generation,
	}
	b.putCell(x, y, cell)
	return nil
}

func (s Surface) withInternedStyle(style ResolvedStyle) (Surface, StyleID) {
	for i, existing := range s.styles {
		if existing == style {
			return s, StyleID(i + 1)
		}
	}
	s = s.clone()
	s.styles = append(s.styles, style)
	return s, StyleID(len(s.styles))
}

func (s Surface) withInternedLink(link string) (Surface, LinkID) {
	if link == "" {
		return s, 0
	}
	for i, existing := range s.links {
		if existing == link {
			return s, LinkID(i + 1)
		}
	}
	s = s.clone()
	s.links = append(s.links, link)
	return s, LinkID(len(s.links))
}

func (s Surface) clone() Surface {
	return Surface{
		Width:  s.Width,
		Height: s.Height,
		cells:  append([]Cell(nil), s.cells...),
		styles: append([]ResolvedStyle(nil), s.styles...),
		links:  append([]string(nil), s.links...),
		hash:   s.hash,
	}
}

func (b *Builder) putCell(x, y int, cell Cell) {
	if x < 0 || y < 0 || x >= b.surface.Width || y >= b.surface.Height {
		return
	}
	span := max(1, int(cell.Width))
	if span > b.surface.Width-x {
		return
	}
	if cell.Width == 0 {
		cell.Width = 1
	}
	clearOwning(b.surface.cells, b.surface.Width, x, y)
	for i := 1; i < span && x+i < b.surface.Width; i++ {
		clearOwning(b.surface.cells, b.surface.Width, x+i, y)
	}
	idx := y*b.surface.Width + x
	b.surface.cells[idx] = cell
	for i := 1; i < span && x+i < b.surface.Width; i++ {
		b.surface.cells[y*b.surface.Width+x+i] = Cell{
			Width:        1,
			Continuation: true,
			Style:        cell.Style,
			Link:         cell.Link,
			NodeID:       cell.NodeID,
			Generation:   cell.Generation,
		}
	}
}

func (b *Builder) internStyle(style ResolvedStyle) StyleID {
	if id, ok := b.styleIndex[style]; ok {
		return id
	}
	b.surface.styles = append(b.surface.styles, style)
	id := StyleID(len(b.surface.styles))
	b.styleIndex[style] = id
	return id
}

func (b *Builder) internLink(link string) LinkID {
	if link == "" {
		return 0
	}
	if id, ok := b.linkIndex[link]; ok {
		return id
	}
	b.surface.links = append(b.surface.links, link)
	id := LinkID(len(b.surface.links))
	b.linkIndex[link] = id
	return id
}

func (s Surface) finalize() Surface {
	dto := struct {
		Width, Height int
		Cells         []Cell
		Styles        []ResolvedStyle
		Links         []string
	}{
		Width:  s.Width,
		Height: s.Height,
		Cells:  s.cells,
		Styles: s.styles,
		Links:  s.links,
	}
	payload, _ := json.Marshal(dto)
	s.hash = sha256.Sum256(payload)
	return s
}

func clearOwning(cells []Cell, widthValue, x, y int) {
	if x < 0 || y < 0 || widthValue <= 0 {
		return
	}
	idx := y*widthValue + x
	if idx < 0 || idx >= len(cells) {
		return
	}
	start := idx
	if cells[start].Continuation {
		for start > y*widthValue && cells[start].Continuation {
			start--
		}
	}
	span := max(1, int(cells[start].Width))
	for i := 0; i < span && start+i < len(cells) && start+i < (y+1)*widthValue; i++ {
		cells[start+i] = Cell{}
	}
}

const maxInt = int(^uint(0) >> 1)

func MergeDirty(regions []Region) []Region {
	if len(regions) == 0 {
		return nil
	}
	merged := append([]Region(nil), regions...)
	slices.SortStableFunc(merged, func(a, b Region) int {
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		return a.X - b.X
	})
	out := []Region{merged[0]}
	for _, region := range merged[1:] {
		last := &out[len(out)-1]
		if last.Y == region.Y && last.Height == region.Height && last.X+last.Width >= region.X {
			last.Width = max(last.Width, region.X+region.Width-last.X)
			continue
		}
		out = append(out, region)
	}
	return out
}
