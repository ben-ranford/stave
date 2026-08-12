package testfixture

import (
	"fmt"

	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

// Primitive constructs the canonical fixture named by
// testdata/primitive-manifest.json. Every catalog entry exercises its real
// public constructor; unknown names fail closed instead of silently becoming a
// generic text node.
func Primitive(name, namespace string) (semantic.Node, error) {
	base := primitive.Options{Namespace: namespace, View: "catalog", Entity: name, Name: name}
	child := func(entity string) (semantic.Node, error) {
		return primitive.Text(primitive.Options{Namespace: namespace, View: "catalog", Entity: entity, Name: entity}, entity)
	}

	switch name {
	case "brand-a", "brand-b":
		return BrandTree(namespace+"-"+name, "ready")
	case "text":
		return primitive.Text(base, "text")
	case "label":
		return primitive.Label(base, "label")
	case "stack", "row", "grid", "surface", "frame", "list":
		content, err := child(name + "-child")
		if err != nil {
			return semantic.Node{}, err
		}
		switch name {
		case "stack":
			return primitive.Stack(base, content)
		case "row":
			return primitive.Row(base, content)
		case "grid":
			return primitive.GridLayout(base, content)
		case "surface":
			return primitive.Surface(base, content)
		case "frame":
			return primitive.Frame(base, content)
		default:
			return primitive.List(base, content)
		}
	case "table":
		return TableTree(namespace)
	case "status":
		return primitive.Status(base, "ready")
	case "chip":
		return primitive.Chip(base, "stable")
	case "focus":
		content, err := primitive.Button(primitive.Options{Namespace: namespace, View: "catalog", Entity: "focus-button", Name: "Focus"}, "Focus")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.FocusIndicator(base, content)
	case "disclosure":
		content, err := child("disclosure-content")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Disclosure(base, "Details", true, content)
	case "viewport":
		content, err := child("viewport-content")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Viewport(primitive.ViewportOptions{Options: base, Width: 80, Height: 24, Children: []semantic.Node{content}})
	case "input":
		return primitive.Input(primitive.InputOptions{Options: base, Value: "value", Required: true})
	case "secure-input":
		return primitive.SecureInput(primitive.InputOptions{Options: base, Required: true})
	case "empty":
		return primitive.Empty(base, "No results")
	case "loading":
		return primitive.Loading(base, "Loading")
	case "error":
		return primitive.ErrorState(base, "Unable to load")
	case "terminal-writer":
		return primitive.CodeBlock(base, "plain terminal output")
	case "semantic-snapshot":
		content, err := child("snapshot-content")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Section(base, content)
	case "action-descriptor":
		return primitive.Button(base, "Run")
	case "diagnostics":
		return primitive.Alert(base, "Diagnostic")
	case "tabs":
		return primitive.Tabs(base, primitive.Tab{Key: "one", Name: "One", Selected: true}, primitive.Tab{Key: "two", Name: "Two"})
	case "pagination":
		return primitive.Pagination(base, 2, 5)
	case "command-palette":
		command, err := primitive.Button(primitive.Options{Namespace: namespace, View: "catalog", Entity: "palette-command", Name: "Run"}, "Run")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.CommandPalette(base, "run", command)
	case "bar-chart":
		return primitive.BarChart(base, []int{1, 3, 2}, "Requests: one, three, two")
	case "sparkline":
		return primitive.Sparkline(base, []int{1, 2, 3}, "Trend rising from one to three")
	case "inspector":
		content, err := child("inspector-content")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Inspector(base, content)
	case "master-detail":
		master, err := primitive.List(primitive.Options{Namespace: namespace, View: "catalog", Entity: "master", Name: "Master"})
		if err != nil {
			return semantic.Node{}, err
		}
		detail, err := child("detail")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.MasterDetail(base, master, detail)
	case "modal":
		content, err := child("modal-content")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Modal(base, content)
	case "dialog":
		content, err := child("dialog-content")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Dialog(base, content)
	case "confirmation":
		return primitive.Confirmation(base, "Continue?")
	case "form":
		field, err := primitive.Input(primitive.InputOptions{Options: primitive.Options{Namespace: namespace, View: "catalog", Entity: "form-field", Name: "Name"}, Required: true})
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Form(base, field)
	case "progress":
		return primitive.Progress(base, 1, 3, false)
	case "overlay":
		content, err := child("overlay-content")
		if err != nil {
			return semantic.Node{}, err
		}
		return primitive.Overlay(base, content)
	default:
		return semantic.Node{}, fmt.Errorf("unknown primitive fixture %q", name)
	}
}
