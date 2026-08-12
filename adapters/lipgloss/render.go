package lipglossadapter

import (
	"fmt"
	"strings"

	lg "charm.land/lipgloss/v2"
	"github.com/ben-ranford/stave/surface"
)

type RenderRequest struct {
	Surface surface.Surface
	Profile Profile
}

type Segment struct {
	Row     int
	Column  int
	Width   int
	Text    string
	StyleID surface.StyleID
}

type Row struct {
	Index    int
	Width    int
	Plain    string
	Segments []Segment
}

type RenderResult struct {
	Output   string
	Rows     []Row
	Segments []Segment
	Styles   map[surface.StyleID]CompiledStyle
	Profile  Profile
}

func Render(req RenderRequest) (RenderResult, error) {
	if req.Surface.Width < 0 || req.Surface.Height < 0 {
		return RenderResult{}, fmt.Errorf("invalid surface bounds %dx%d", req.Surface.Width, req.Surface.Height)
	}
	cells := req.Surface.Cells()
	if len(cells) != req.Surface.Width*req.Surface.Height {
		return RenderResult{}, fmt.Errorf("surface cell count %d does not match bounds %dx%d", len(cells), req.Surface.Width, req.Surface.Height)
	}

	styles, err := CompileStyles(req.Surface.Styles(), req.Profile)
	if err != nil {
		return RenderResult{}, err
	}

	rows := make([]Row, 0, req.Surface.Height)
	segments := make([]Segment, 0, len(cells))
	var output strings.Builder

	for y := 0; y < req.Surface.Height; y++ {
		row, rendered, err := renderRow(req.Surface, y, styles, req.Profile)
		if err != nil {
			return RenderResult{}, err
		}
		rows = append(rows, row)
		segments = append(segments, row.Segments...)
		output.WriteString(rendered)
		if y+1 < req.Surface.Height {
			output.WriteByte('\n')
		}
	}

	return RenderResult{
		Output:   output.String(),
		Rows:     rows,
		Segments: segments,
		Styles:   styles,
		Profile:  req.Profile,
	}, nil
}

func renderRow(s surface.Surface, row int, styles map[surface.StyleID]CompiledStyle, profile Profile) (Row, string, error) {
	plainParts := make([]string, 0, s.Width)
	type run struct {
		styleID surface.StyleID
		text    strings.Builder
		width   int
		column  int
	}

	var runs []run
	var current *run
	column := 0

	for x := 0; x < s.Width; x++ {
		text, width, styleID, err := normalizeCell(s.At(x, row))
		if err != nil {
			return Row{}, "", err
		}
		if width == 0 {
			continue
		}
		plainParts = append(plainParts, text)
		if current == nil || current.styleID != styleID {
			runs = append(runs, run{styleID: styleID, column: column})
			current = &runs[len(runs)-1]
		}
		current.text.WriteString(text)
		current.width += width
		column += width
	}

	rowOut := Row{
		Index: row,
		Width: column,
		Plain: strings.Join(plainParts, ""),
	}
	var rendered strings.Builder

	for _, r := range runs {
		seg := Segment{
			Row:     row,
			Column:  r.column,
			Width:   r.width,
			Text:    r.text.String(),
			StyleID: r.styleID,
		}
		rowOut.Segments = append(rowOut.Segments, seg)
		compiled, ok := styles[r.styleID]
		if r.styleID != 0 && !ok {
			return Row{}, "", fmt.Errorf("missing compiled style %d", r.styleID)
		}
		if !profile.RenderANSI() || r.styleID == 0 {
			rendered.WriteString(seg.Text)
			continue
		}
		rendered.WriteString(compiled.Style.Render(seg.Text))
	}

	return rowOut, rendered.String(), nil
}

func normalizeCell(cell surface.Cell) (string, int, surface.StyleID, error) {
	if cell.Continuation {
		return "", 0, 0, nil
	}
	width := int(cell.Width)
	if width <= 0 {
		width = 1
	}
	text := cell.Grapheme
	if text == "" {
		return strings.Repeat(" ", width), width, cell.Style, nil
	}
	actual := lg.Width(text)
	if actual > width {
		return "", 0, 0, fmt.Errorf("cell %q width %d exceeds declared width %d", text, actual, width)
	}
	if actual < width {
		text += strings.Repeat(" ", width-actual)
	}
	return text, width, cell.Style, nil
}
