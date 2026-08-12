package primitive

import (
	"fmt"
	"strconv"

	"github.com/ben-ranford/stave/semantic"
)

type Column struct {
	Key, Name       string
	Width           int
	WidthPolicy     string // auto, fixed, min, max
	Numeric, Sticky bool
}
type TableRow struct {
	Key, Name          string
	Cells              []string
	Selected, Expanded bool
}
type TableOptions struct {
	Options
	Columns              []Column
	Rows                 []TableRow
	SelectedKey          string
	Offset, Limit, Total int
	SortKey              string
	SortDescending       bool
	SelectionMode        string // none, single, multiple
	State                string // ready, empty, loading, error
	ErrorMessage         string
}

// Table emits a bounded semantic window. Row identity is derived solely from
// the application row key, so sorting, filtering, and pagination do not change
// the target of an action.
func Table(t TableOptions) (semantic.Node, error) {
	if t.SelectionMode == "" {
		t.SelectionMode = "single"
	}
	if t.SelectionMode != "none" && t.SelectionMode != "single" && t.SelectionMode != "multiple" {
		return semantic.Node{}, fmt.Errorf("invalid selection mode")
	}
	if t.State == "" {
		t.State = "ready"
	}
	if t.State != "ready" && t.State != "empty" && t.State != "loading" && t.State != "error" {
		return semantic.Node{}, fmt.Errorf("invalid table state")
	}
	if t.State == "error" && t.ErrorMessage == "" {
		return semantic.Node{}, fmt.Errorf("error table requires error message")
	}
	if err := requirePositive("offset", t.Offset); err != nil {
		return semantic.Node{}, err
	}
	if err := requirePositive("limit", t.Limit); err != nil {
		return semantic.Node{}, err
	}
	if t.Total == 0 {
		t.Total = len(t.Rows)
	}
	if t.Limit == 0 {
		t.Limit = len(t.Rows)
	}
	if len(t.Columns) == 0 && len(t.Rows) > 0 {
		return semantic.Node{}, fmt.Errorf("table requires columns")
	}
	seenColumns := map[string]bool{}
	for _, c := range t.Columns {
		if c.Key == "" || seenColumns[c.Key] {
			return semantic.Node{}, fmt.Errorf("table column keys must be unique and non-empty")
		}
		seenColumns[c.Key] = true
		if c.Width < 0 {
			return semantic.Node{}, fmt.Errorf("column %q width must not be negative", c.Key)
		}
		if c.WidthPolicy == "" {
			continue
		}
		if c.WidthPolicy != "auto" && c.WidthPolicy != "fixed" && c.WidthPolicy != "min" && c.WidthPolicy != "max" {
			return semantic.Node{}, fmt.Errorf("column %q has invalid width policy", c.Key)
		}
	}
	seenRows := map[string]bool{}
	selectedCount := 0
	for _, r := range t.Rows {
		if r.Key == "" || seenRows[r.Key] {
			return semantic.Node{}, fmt.Errorf("table row keys must be unique and non-empty")
		}
		seenRows[r.Key] = true
		if r.Selected || r.Key == t.SelectedKey {
			selectedCount++
		}
	}
	if t.SelectedKey != "" && !seenRows[t.SelectedKey] {
		return semantic.Node{}, fmt.Errorf("selected key is not in table window")
	}
	if t.SortKey != "" && !seenColumns[t.SortKey] {
		return semantic.Node{}, fmt.Errorf("sort key is not a table column")
	}
	if t.SelectionMode == "single" && selectedCount > 1 {
		return semantic.Node{}, fmt.Errorf("single-selection table has multiple selected rows")
	}
	if t.State == "empty" && len(t.Rows) != 0 {
		return semantic.Node{}, fmt.Errorf("empty table cannot contain rows")
	}
	if t.Limit < len(t.Rows) {
		return semantic.Node{}, fmt.Errorf("table window contains more rows than limit")
	}
	if t.Total < t.Offset+len(t.Rows) {
		return semantic.Node{}, fmt.Errorf("table total is smaller than window")
	}
	headers := make([]semantic.Node, 0, len(t.Columns))
	for _, c := range t.Columns {
		m := map[string]string{"columnKey": c.Key, "alignment": "leading"}
		if c.Width > 0 {
			m["width"] = strconv.Itoa(c.Width)
		}
		if c.WidthPolicy != "" {
			m["widthPolicy"] = c.WidthPolicy
		}
		if c.Numeric {
			m["alignment"] = "numeric"
		}
		if c.Sticky {
			m["sticky"] = "true"
		}
		headerOptions := Options{
			Namespace: t.Namespace,
			View:      t.View,
			Kind:      "columnheader",
			Entity:    t.Entity + ":" + c.Key,
			Slot:      "header",
			Name:      c.Name,
			Metadata:  m,
			Actions:   []semantic.ActionRef{Action("sort_column", "Sort", false)},
			StyleRole: "table-header",
		}
		if t.SortKey == c.Key {
			if t.SortDescending {
				m["sort"] = "descending"
				h, err := node(headerOptions, "columnheader", semantic.Value{Text: c.Name, HasValue: true}, nil, semantic.State("sorted-descending"))
				if err != nil {
					return semantic.Node{}, err
				}
				headers = append(headers, h)
				continue
			}
			m["sort"] = "ascending"
			h, err := node(headerOptions, "columnheader", semantic.Value{Text: c.Name, HasValue: true}, nil, semantic.State("sorted-ascending"))
			if err != nil {
				return semantic.Node{}, err
			}
			headers = append(headers, h)
			continue
		}
		m["sort"] = "none"
		h, err := node(headerOptions, "columnheader", semantic.Value{Text: c.Name, HasValue: true}, nil)
		if err != nil {
			return semantic.Node{}, err
		}
		headers = append(headers, h)
	}
	hg, err := node(Options{Namespace: t.Namespace, View: t.View, Kind: "rowgroup", Entity: t.Entity, Slot: "header", Name: "Header"}, "rowgroup", semantic.Value{}, headers)
	if err != nil {
		return semantic.Node{}, err
	}
	rows := make([]semantic.Node, 0, len(t.Rows))
	for _, r := range t.Rows {
		if r.Key == "" {
			return semantic.Node{}, fmt.Errorf("table row key is required")
		}
		if len(r.Cells) != len(t.Columns) {
			return semantic.Node{}, fmt.Errorf("row %q cell count does not match columns", r.Key)
		}
		cells := make([]semantic.Node, 0, len(r.Cells))
		for i, v := range r.Cells {
			if i >= len(t.Columns) {
				return semantic.Node{}, fmt.Errorf("row %q has more cells than columns", r.Key)
			}
			c, e := node(Options{
				Namespace: t.Namespace,
				View:      t.View,
				Kind:      "cell",
				Entity:    t.Entity + ":" + r.Key,
				Slot:      "cell-" + t.Columns[i].Key,
				Name:      t.Columns[i].Name,
				Relations: []semantic.Relation{{Kind: "labelled-by", Target: headers[i].ID()}},
				Metadata: map[string]string{
					"columnKey":      t.Columns[i].Key,
					"rowKey":         r.Key,
					"headerNodeId":   headers[i].ID().String(),
					"recordFallback": "disclosure",
				},
			}, "cell", semantic.Value{Text: v, HasValue: true}, nil)
			if e != nil {
				return semantic.Node{}, e
			}
			cells = append(cells, c)
		}
		actions := []semantic.ActionRef{Action("select_row", "Select", false), Action("open_row", "Open", false)}
		if r.Expanded {
			actions = append(actions, Action("close_row", "Close", false))
		} else {
			actions = append(actions, Action("expand_row", "Expand", false))
		}
		meta := map[string]string{
			"rowKey":         r.Key,
			"selected":       strconv.FormatBool(r.Selected || r.Key == t.SelectedKey),
			"expanded":       strconv.FormatBool(r.Expanded),
			"position":       strconv.Itoa(t.Offset + len(rows) + 1),
			"setSize":        strconv.Itoa(t.Total),
			"recordFallback": "disclosure",
		}
		ro, e := node(Options{
			Namespace: t.Namespace,
			View:      t.View,
			Kind:      "row",
			Entity:    t.Entity + ":" + r.Key,
			Slot:      "row",
			Name:      nonempty(r.Name, r.Key),
			Actions:   actions,
			Focusable: true,
			Metadata:  meta,
			StyleRole: "table-row",
		}, "row", semantic.Value{}, cells)
		if e != nil {
			return semantic.Node{}, e
		}
		rows = append(rows, ro)
	}
	body, err := node(Options{Namespace: t.Namespace, View: t.View, Kind: "rowgroup", Entity: t.Entity, Slot: "body", Name: "Rows", Metadata: map[string]string{"offset": strconv.Itoa(t.Offset), "limit": strconv.Itoa(t.Limit), "total": strconv.Itoa(t.Total), "sortKey": t.SortKey, "sortDescending": strconv.FormatBool(t.SortDescending), "recordFallback": "disclosure"}}, "rowgroup", semantic.Value{}, rows)
	if err != nil {
		return semantic.Node{}, err
	}
	o := t.Options
	o.Kind = nonempty(o.Kind, "table")
	o.Actions = ensureAction(o.Actions, Action("previous_page", "Previous page", false))
	o.Actions = ensureAction(o.Actions, Action("next_page", "Next page", false))
	o.Actions = ensureAction(o.Actions, Action("sort_column", "Sort", false))
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["windowed"] = "true"
	o.Metadata["selectionMode"] = t.SelectionMode
	o.Metadata["state"] = t.State
	if t.ErrorMessage != "" {
		o.Metadata["errorMessage"] = t.ErrorMessage
	}
	o.Metadata["recordFallback"] = "disclosure"
	o.StyleRole = nonempty(o.StyleRole, "table")
	return node(o, "table", semantic.Value{}, []semantic.Node{hg, body}, semantic.State("windowed"))
}

func List(o Options, items ...semantic.Node) (semantic.Node, error) {
	return node(o, "list", semantic.Value{}, items)
}

type Tab struct {
	Key, Name string
	Selected  bool
}

func Tabs(o Options, tabs ...Tab) (semantic.Node, error) {
	children := make([]semantic.Node, 0, len(tabs))
	selected := 0
	for _, t := range tabs {
		if t.Selected {
			selected++
		}
		st := semantic.State("unselected")
		if t.Selected {
			st = "selected"
		}
		n, e := node(Options{Namespace: o.Namespace, View: o.View, Kind: "tab", Entity: o.Entity + ":" + t.Key, Slot: "tab", Name: t.Name, Focusable: true, Actions: []semantic.ActionRef{Action("select_tab", "Select", true)}, Metadata: map[string]string{"selected": strconv.FormatBool(t.Selected)}}, "tab", semantic.Value{Text: t.Name, HasValue: true}, nil, st)
		if e != nil {
			return semantic.Node{}, e
		}
		children = append(children, n)
	}
	if selected > 1 {
		return semantic.Node{}, fmt.Errorf("tabs must have at most one selected tab")
	}
	o.Kind = nonempty(o.Kind, "tabs")
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["count"] = strconv.Itoa(len(tabs))
	return node(o, "tablist", semantic.Value{}, children)
}

func Pagination(o Options, page, pages int) (semantic.Node, error) {
	if page < 1 || pages < 0 || (pages > 0 && page > pages) {
		return semantic.Node{}, fmt.Errorf("invalid pagination")
	}
	o.Actions = []semantic.ActionRef{Action("previous_page", "Previous", false), Action("next_page", "Next", false), Action("goto_page", "Go to page", true)}
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["page"] = strconv.Itoa(page)
	o.Metadata["pages"] = strconv.Itoa(pages)
	return node(o, "group", semantic.Value{}, nil)
}
