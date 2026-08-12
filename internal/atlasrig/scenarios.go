package atlasrig

import (
	"fmt"
	"strconv"

	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func buildScenario(name string, events int) (semantic.Node, error) {
	title, err := primitive.Heading(options(name, "title", "Atlas"), "Atlas control deck")
	if err != nil {
		return semantic.Node{}, err
	}
	eventStatus, err := primitive.Status(primitive.Options{
		Namespace: "atlas", View: name, Entity: "event-status", Name: "Processed events",
		StyleRole: "status-advisory",
	}, fmt.Sprintf("Processed events: %d", events))
	if err != nil {
		return semantic.Node{}, err
	}
	body, err := buildScenarioBody(name)
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Frame(primitive.Options{
		Namespace: "atlas", View: name, Entity: "root", Name: "Atlas proof fixture",
		Metadata: map[string]string{"scenario": name, "layout.gap": "1"},
	}, title, eventStatus, body)
}

func buildScenarioBody(name string) (semantic.Node, error) {
	switch name {
	case "ready":
		return readyScenario(name)
	case "empty":
		return emptyScenario(name)
	case "loading":
		return loadingScenario(name)
	case "error":
		return errorScenario(name)
	case "invalid-form":
		return invalidFormScenario(name)
	case "modal-confirmation":
		return modalScenario(name)
	case "dense-table":
		return denseTableScenario(name)
	default:
		return semantic.Node{}, fmt.Errorf("unsupported atlas scenario %q", name)
	}
}

func readyScenario(view string) (semantic.Node, error) {
	summary, err := primitive.Text(options(view, "summary", "Operations summary"), "5 corridors · 18 signals · 2 alerts")
	if err != nil {
		return semantic.Node{}, err
	}
	inspect, err := primitive.Button(primitive.Options{
		Namespace: "atlas", View: view, Entity: "inspect", Name: "Inspect route",
		Actions: []semantic.ActionRef{{ID: semantic.ActionID(InspectAction), Label: "Inspect route", Default: true}},
	}, "Inspect route")
	if err != nil {
		return semantic.Node{}, err
	}
	retire, err := primitive.Button(primitive.Options{
		Namespace: "atlas", View: view, Entity: "retire", Name: "Retire route", StyleRole: "status-failure",
		Actions: []semantic.ActionRef{{ID: semantic.ActionID(RetireAction), Label: "Retire route", Default: true}},
	}, "Retire route")
	if err != nil {
		return semantic.Node{}, err
	}
	actions, err := primitive.Row(primitive.Options{Namespace: "atlas", View: view, Entity: "actions", Name: "Route actions"}, inspect, retire)
	if err != nil {
		return semantic.Node{}, err
	}
	table, err := routeTable(view, "routes", routeRows(3), 0, 3, 3, "ready")
	if err != nil {
		return semantic.Node{}, err
	}
	detail, err := primitive.Stack(primitive.Options{Namespace: "atlas", View: view, Entity: "detail", Name: "Route detail"}, summary, actions)
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.GridLayout(primitive.Options{
		Namespace: "atlas", View: view, Entity: "deck", Name: "Command deck",
		Metadata: map[string]string{"layout.columns": "1fr,2fr", "layout.gap": "2"},
	}, detail, table)
}

func emptyScenario(view string) (semantic.Node, error) {
	empty, err := primitive.Empty(options(view, "empty", "No routes"), "No routes match the current filter")
	if err != nil {
		return semantic.Node{}, err
	}
	table, err := routeTable(view, "routes", nil, 0, 0, 0, "empty")
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Stack(options(view, "empty-stack", "Empty routes"), empty, table)
}

func loadingScenario(view string) (semantic.Node, error) {
	loading, err := primitive.Loading(options(view, "loading", "Loading routes"), "Loading routes…")
	if err != nil {
		return semantic.Node{}, err
	}
	progress, err := primitive.Progress(options(view, "progress", "Route load progress"), 0, 0, true)
	if err != nil {
		return semantic.Node{}, err
	}
	table, err := routeTable(view, "routes", nil, 0, 0, 0, "loading")
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Stack(options(view, "loading-stack", "Loading state"), loading, progress, table)
}

func errorScenario(view string) (semantic.Node, error) {
	failure, err := primitive.ErrorState(options(view, "failure", "Route error"), "Unable to load routes; retry is safe")
	if err != nil {
		return semantic.Node{}, err
	}
	table, err := primitive.Table(primitive.TableOptions{
		Options: primitive.Options{Namespace: "atlas", View: view, Entity: "routes", Name: "Route matrix"},
		Columns: routeColumns(), State: "error", ErrorMessage: "route source unavailable",
	})
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Stack(options(view, "error-stack", "Failure state"), failure, table)
}

func invalidFormScenario(view string) (semantic.Node, error) {
	route, err := primitive.Input(primitive.InputOptions{
		Options:  primitive.Options{Namespace: "atlas", View: view, Entity: "route-id", Name: "Route ID"},
		Required: true, Invalid: true, ErrorMessage: "Route ID is required",
	})
	if err != nil {
		return semantic.Node{}, err
	}
	capacity, err := primitive.NumberInput(primitive.NumberOptions{
		InputOptions: primitive.InputOptions{Options: primitive.Options{Namespace: "atlas", View: view, Entity: "capacity", Name: "Capacity"}, Value: "-1", Required: true, Invalid: true, ErrorMessage: "Capacity must be positive"},
		Min:          "1", Max: "999", Step: "1",
	})
	if err != nil {
		return semantic.Node{}, err
	}
	confirm, err := primitive.Checkbox(primitive.CheckboxOptions{
		Options:  primitive.Options{Namespace: "atlas", View: view, Entity: "acknowledge", Name: "Acknowledge risk"},
		Required: true, Invalid: true, ErrorMessage: "Acknowledgement is required",
	}, "Acknowledge risk")
	if err != nil {
		return semantic.Node{}, err
	}
	owner, err := primitive.Select(primitive.SelectOptions{
		Options:  primitive.Options{Namespace: "atlas", View: view, Entity: "owner", Name: "Owner"},
		Required: true, Invalid: true, ErrorMessage: "Select an owner",
		Choices: []primitive.Choice{{Key: "ops", Name: "Operations"}, {Key: "relay", Name: "Relay"}},
	})
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Form(options(view, "route-form", "Route form"), route, capacity, confirm, owner)
}

func modalScenario(view string) (semantic.Node, error) {
	prompt, err := primitive.Confirmation(options(view, "confirmation", "Confirm retirement"), "Retire north gate?")
	if err != nil {
		return semantic.Node{}, err
	}
	modal, err := primitive.Modal(primitive.Options{
		Namespace: "atlas", View: view, Entity: "modal", Name: "Retire route",
		Actions: []semantic.ActionRef{{ID: semantic.ActionID(RetireAction), Label: "Retire route", Default: true}},
	}, prompt)
	if err != nil {
		return semantic.Node{}, err
	}
	background, err := primitive.Text(primitive.Options{Namespace: "atlas", View: view, Entity: "background", Name: "Background", Hidden: true}, "Background content is inert")
	if err != nil {
		return semantic.Node{}, err
	}
	stack, err := primitive.Stack(options(view, "modal-stack", "Modal stack"), background, modal)
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Overlay(options(view, "overlay", "Confirmation overlay"), stack)
}

func denseTableScenario(view string) (semantic.Node, error) {
	table, err := routeTable(view, "dense-routes", routeRows(20), 40, 20, 200, "ready")
	if err != nil {
		return semantic.Node{}, err
	}
	page, err := primitive.Pagination(options(view, "pagination", "Route pages"), 3, 10)
	if err != nil {
		return semantic.Node{}, err
	}
	tabs, err := primitive.Tabs(options(view, "tabs", "Route views"),
		primitive.Tab{Key: "routes", Name: "Routes", Selected: true},
		primitive.Tab{Key: "signals", Name: "Signals"},
		primitive.Tab{Key: "alerts", Name: "Alerts"},
	)
	if err != nil {
		return semantic.Node{}, err
	}
	chart, err := primitive.BarChart(options(view, "chart", "Queue depth chart"), []int{4, 7, 2, 9, 5}, "Queue depth by route: 4, 7, 2, 9, 5")
	if err != nil {
		return semantic.Node{}, err
	}
	detail, err := primitive.Inspector(options(view, "inspector", "Route inspector"), chart, page)
	if err != nil {
		return semantic.Node{}, err
	}
	master, err := primitive.MasterDetail(options(view, "master-detail", "Route master detail"), table, detail)
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Stack(options(view, "dense-stack", "Dense route fixture"), tabs, master)
}

func routeTable(view, entity string, rows []primitive.TableRow, offset, limit, total int, state string) (semantic.Node, error) {
	selected := ""
	if len(rows) > 0 {
		selected = rows[0].Key
	}
	return primitive.Table(primitive.TableOptions{
		Options: primitive.Options{Namespace: "atlas", View: view, Entity: entity, Name: "Route matrix", StyleRole: "table"},
		Columns: routeColumns(), Rows: rows, SelectedKey: selected,
		Offset: offset, Limit: limit, Total: total,
		SortKey: "queue", SortDescending: true, SelectionMode: "single", State: state,
	})
}

func routeColumns() []primitive.Column {
	return []primitive.Column{
		{Key: "route", Name: "Route", WidthPolicy: "min", Width: 12, Sticky: true},
		{Key: "state", Name: "State", WidthPolicy: "auto"},
		{Key: "owner", Name: "Owner", WidthPolicy: "auto"},
		{Key: "queue", Name: "Queue", WidthPolicy: "fixed", Width: 6, Numeric: true},
	}
}

func routeRows(count int) []primitive.TableRow {
	rows := make([]primitive.TableRow, 0, count)
	for index := 0; index < count; index++ {
		key := fmt.Sprintf("route-%03d", index+1)
		if index == 0 {
			key = "north-gate"
		}
		rows = append(rows, primitive.TableRow{
			Key: key, Name: fmt.Sprintf("Route %03d", index+1), Selected: index == 0,
			Cells: []string{key, []string{"steady", "watch", "hold"}[index%3], []string{"ops", "relay", "field"}[index%3], strconv.Itoa((index*3 + 4) % 17)},
		})
	}
	return rows
}

func options(view, entity, name string) primitive.Options {
	return primitive.Options{Namespace: "atlas", View: view, Entity: entity, Name: name}
}
