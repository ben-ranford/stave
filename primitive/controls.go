package primitive

import (
	"fmt"
	"strconv"

	"github.com/ben-ranford/stave/secret"
	"github.com/ben-ranford/stave/semantic"
)

func Status(o Options, text string) (semantic.Node, error) {
	o.Kind = nonempty(o.Kind, "status")
	o.StyleRole = nonempty(o.StyleRole, "status-neutral")
	o.Metadata = toneMeta(o.Metadata, "neutral")
	o.Live = semantic.LivePolite
	return textNode(o, "status", text)
}
func Chip(o Options, text string) (semantic.Node, error) { o.Kind = "chip"; return Status(o, text) }
func Alert(o Options, text string) (semantic.Node, error) {
	o.Kind = "alert"
	o.StyleRole = nonempty(o.StyleRole, "status-warning")
	o.Metadata = toneMeta(o.Metadata, "warning")
	o.Live = semantic.LiveAssertive
	return textNode(o, "alert", text)
}
func FocusIndicator(o Options, child semantic.Node) (semantic.Node, error) {
	o.Kind = "focus"
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["indicator"] = "true"
	return node(o, "group", semantic.Value{}, []semantic.Node{child}, semantic.State("focused"))
}
func Disclosure(o Options, title string, expanded bool, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = "disclosure"
	o.Name = title
	o.Actions = ensureAction(o.Actions, Action("toggle_disclosure", "Toggle", true))
	st := semantic.State("collapsed")
	if expanded {
		st = "expanded"
	}
	return node(o, "group", semantic.Value{}, children, st)
}

type ViewportOptions struct {
	Options
	Width, Height int
	Narrow        bool
	Children      []semantic.Node
}

func Viewport(v ViewportOptions) (semantic.Node, error) {
	if v.Width < 0 || v.Height < 0 {
		return semantic.Node{}, fmt.Errorf("viewport dimensions must not be negative")
	}
	v.Metadata = cloneMeta(v.Metadata)
	v.Metadata["width"] = strconv.Itoa(v.Width)
	v.Metadata["height"] = strconv.Itoa(v.Height)
	v.Metadata["narrow"] = strconv.FormatBool(v.Narrow)
	return node(v.Options, "region", semantic.Value{}, v.Children)
}

type InputOptions struct {
	Options
	Value, Placeholder                             string
	Secret, Multiline, ReadOnly, Required, Invalid bool
	ErrorMessage                                   string
}

func Input(i InputOptions) (semantic.Node, error) {
	i.Focusable = !i.ReadOnly
	i.Sensitive = i.Secret
	i.Metadata = cloneMeta(i.Metadata)
	i.Metadata["placeholder"] = i.Placeholder
	i.Metadata["secret"] = strconv.FormatBool(i.Secret)
	i.Metadata["multiline"] = strconv.FormatBool(i.Multiline)
	i.Metadata["readonly"] = strconv.FormatBool(i.ReadOnly)
	i.Metadata["required"] = strconv.FormatBool(i.Required)
	i.Metadata["invalid"] = strconv.FormatBool(i.Invalid)
	if i.ErrorMessage != "" {
		i.Metadata["error"] = i.ErrorMessage
	}
	i.StyleRole = nonempty(i.StyleRole, fieldStyleRole(i.Invalid))
	i.Metadata = toneMeta(i.Metadata, fieldTone(i.Invalid))
	if i.Name == "" {
		i.Name = "Input"
	}
	if !i.ReadOnly {
		i.Actions = ensureAction(i.Actions, Action("edit", "Edit", true))
		i.Actions = ensureAction(i.Actions, Action("set-value", "Set value", false))
		if i.Multiline {
			i.Actions = ensureAction(i.Actions, Action("submit", "Submit", false))
		}
	}
	value := semantic.Value{Text: i.Value, HasValue: true}
	if i.Secret {
		value = semantic.SecretValue()
	}
	role := semantic.Role("textbox")
	if i.Multiline {
		i.Metadata["lines"] = "multiple"
	}
	children, relations, err := fieldErrorDecoration(i.Options, i.ErrorMessage)
	if err != nil {
		return semantic.Node{}, err
	}
	i.Relations = append(i.Relations, relations...)
	return node(i.Options, role, value, children)
}
func SecureInput(i InputOptions) (semantic.Node, error) { i.Secret = true; return Input(i) }

type NumberOptions struct {
	InputOptions
	Min  string
	Max  string
	Step string
}

func NumberInput(n NumberOptions) (semantic.Node, error) {
	n.Kind = nonempty(n.Kind, "number")
	n.Metadata = cloneMeta(n.Metadata)
	n.Metadata["inputMode"] = "numeric"
	if n.Min != "" {
		n.Metadata["min"] = n.Min
	}
	if n.Max != "" {
		n.Metadata["max"] = n.Max
	}
	if n.Step != "" {
		n.Metadata["step"] = n.Step
	}
	return Input(n.InputOptions)
}

type CheckboxOptions struct {
	Options
	Checked      bool
	Required     bool
	Invalid      bool
	ErrorMessage string
}

func Checkbox(c CheckboxOptions, label string) (semantic.Node, error) {
	c.Kind = nonempty(c.Kind, "checkbox")
	c.Focusable = !c.Disabled
	c.Name = nonempty(c.Name, label)
	c.Actions = ensureAction(c.Actions, Action("toggle", "Toggle", true))
	c.Actions = ensureAction(c.Actions, Action("set-value", "Set value", false))
	c.Metadata = cloneMeta(c.Metadata)
	c.Metadata["checked"] = strconv.FormatBool(c.Checked)
	c.Metadata["required"] = strconv.FormatBool(c.Required)
	c.Metadata["invalid"] = strconv.FormatBool(c.Invalid)
	if c.ErrorMessage != "" {
		c.Metadata["error"] = c.ErrorMessage
	}
	c.StyleRole = nonempty(c.StyleRole, fieldStyleRole(c.Invalid))
	c.Metadata = toneMeta(c.Metadata, fieldTone(c.Invalid))
	state := semantic.State("unchecked")
	if c.Checked {
		state = "checked"
	}
	children, relations, err := fieldErrorDecoration(c.Options, c.ErrorMessage)
	if err != nil {
		return semantic.Node{}, err
	}
	c.Relations = append(c.Relations, relations...)
	return node(c.Options, "checkbox", semantic.Value{Text: strconv.FormatBool(c.Checked), HasValue: true}, children, state)
}

type Choice struct {
	Key         string
	Name        string
	Description string
	Selected    bool
	Disabled    bool
}

type SelectOptions struct {
	Options
	Value        string
	Placeholder  string
	ReadOnly     bool
	Required     bool
	Invalid      bool
	Multi        bool
	ErrorMessage string
	Choices      []Choice
}

func Select(s SelectOptions) (semantic.Node, error) {
	s.Kind = nonempty(s.Kind, "select")
	s.Focusable = !s.ReadOnly && !s.Disabled
	s.Actions = ensureAction(s.Actions, Action("open", "Open", true))
	s.Actions = ensureAction(s.Actions, Action("select_option", "Select option", false))
	s.Actions = ensureAction(s.Actions, Action("set-value", "Set value", false))
	s.Metadata = cloneMeta(s.Metadata)
	s.Metadata["placeholder"] = s.Placeholder
	s.Metadata["readonly"] = strconv.FormatBool(s.ReadOnly)
	s.Metadata["required"] = strconv.FormatBool(s.Required)
	s.Metadata["invalid"] = strconv.FormatBool(s.Invalid)
	s.Metadata["multi"] = strconv.FormatBool(s.Multi)
	if s.ErrorMessage != "" {
		s.Metadata["error"] = s.ErrorMessage
	}
	s.StyleRole = nonempty(s.StyleRole, fieldStyleRole(s.Invalid))
	s.Metadata = toneMeta(s.Metadata, fieldTone(s.Invalid))
	children := make([]semantic.Node, 0, len(s.Choices)+1)
	for _, choice := range s.Choices {
		state := semantic.State("unselected")
		if choice.Selected {
			state = "selected"
		}
		option, err := node(Options{
			Namespace: s.Namespace,
			View:      s.View,
			Kind:      "option",
			Entity:    s.Entity + ":" + choice.Key,
			Slot:      "option",
			Name:      nonempty(choice.Name, choice.Key),
			Focusable: !choice.Disabled,
			Disabled:  choice.Disabled,
			Actions:   []semantic.ActionRef{Action("select_option", "Select option", true)},
			Metadata: map[string]string{
				"optionKey": choice.Key,
				"selected":  strconv.FormatBool(choice.Selected),
			},
		}, "option", semantic.Value{Text: nonempty(choice.Name, choice.Key), HasValue: true}, nil, state)
		if err != nil {
			return semantic.Node{}, err
		}
		children = append(children, option)
	}
	errorChildren, relations, err := fieldErrorDecoration(s.Options, s.ErrorMessage)
	if err != nil {
		return semantic.Node{}, err
	}
	s.Relations = append(s.Relations, relations...)
	children = append(children, errorChildren...)
	return node(s.Options, "combobox", semantic.Value{Text: s.Value, HasValue: s.Value != ""}, children)
}

type RadioGroupOptions struct {
	Options
	Required     bool
	Invalid      bool
	ErrorMessage string
	Choices      []Choice
}

func RadioGroup(r RadioGroupOptions) (semantic.Node, error) {
	r.Kind = nonempty(r.Kind, "radio-group")
	r.Metadata = cloneMeta(r.Metadata)
	r.Metadata["required"] = strconv.FormatBool(r.Required)
	r.Metadata["invalid"] = strconv.FormatBool(r.Invalid)
	if r.ErrorMessage != "" {
		r.Metadata["error"] = r.ErrorMessage
	}
	r.StyleRole = nonempty(r.StyleRole, fieldStyleRole(r.Invalid))
	r.Metadata = toneMeta(r.Metadata, fieldTone(r.Invalid))
	children := make([]semantic.Node, 0, len(r.Choices)+1)
	for _, choice := range r.Choices {
		state := semantic.State("unchecked")
		if choice.Selected {
			state = "checked"
		}
		radio, err := node(Options{
			Namespace: r.Namespace,
			View:      r.View,
			Kind:      "radio",
			Entity:    r.Entity + ":" + choice.Key,
			Slot:      "option",
			Name:      nonempty(choice.Name, choice.Key),
			Focusable: !choice.Disabled,
			Disabled:  choice.Disabled,
			Actions:   []semantic.ActionRef{Action("select_option", "Select option", true)},
			Metadata: map[string]string{
				"optionKey": choice.Key,
				"selected":  strconv.FormatBool(choice.Selected),
			},
		}, "radio", semantic.Value{Text: nonempty(choice.Name, choice.Key), HasValue: true}, nil, state)
		if err != nil {
			return semantic.Node{}, err
		}
		children = append(children, radio)
	}
	errorChildren, relations, err := fieldErrorDecoration(r.Options, r.ErrorMessage)
	if err != nil {
		return semantic.Node{}, err
	}
	r.Relations = append(r.Relations, relations...)
	children = append(children, errorChildren...)
	return node(r.Options, "radiogroup", semantic.Value{}, children)
}

// SecretHandle aliases the core opaque secret handle without duplicating its
// storage contract or exposing plaintext.
type SecretHandle = secret.Handle

func Empty(o Options, text string) (semantic.Node, error) {
	o.Kind = "empty"
	return textNode(o, "status", text)
}
func Loading(o Options, text string) (semantic.Node, error) {
	o.Kind = "loading"
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["indeterminate"] = "true"
	o.StyleRole = nonempty(o.StyleRole, "status-info")
	o.Metadata = toneMeta(o.Metadata, "info")
	o.Live = semantic.LivePolite
	return node(o, "status", semantic.Value{Text: text, HasValue: true}, nil, semantic.State("loading"))
}
func ErrorState(o Options, text string) (semantic.Node, error) {
	o.Kind = "error"
	o.StyleRole = nonempty(o.StyleRole, "status-danger")
	o.Metadata = toneMeta(o.Metadata, "danger")
	o.Live = semantic.LiveAssertive
	return node(o, "alert", semantic.Value{Text: text, HasValue: true}, nil, semantic.State("error"))
}
func Progress(o Options, current, total int, indeterminate bool) (semantic.Node, error) {
	if current < 0 || total < 0 || (!indeterminate && total == 0) || (!indeterminate && current > total) {
		return semantic.Node{}, fmt.Errorf("invalid progress")
	}
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["current"] = strconv.Itoa(current)
	o.Metadata["total"] = strconv.Itoa(total)
	o.Metadata["indeterminate"] = strconv.FormatBool(indeterminate)
	o.StyleRole = nonempty(o.StyleRole, "status-info")
	o.Metadata = toneMeta(o.Metadata, "info")
	st := semantic.State("determinate")
	if indeterminate {
		st = "indeterminate"
	}
	return node(o, "progressbar", semantic.Value{}, nil, st)
}

func CommandPalette(o Options, query string, commands ...semantic.Node) (semantic.Node, error) {
	o.Name = nonempty(o.Name, "Command palette")
	o.Focusable = true
	o.Actions = []semantic.ActionRef{Action("execute_command", "Execute", true), Action("close_palette", "Close", false)}
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["query"] = query
	return node(o, "dialog", semantic.Value{Text: query, HasValue: true}, commands)
}
func Dialog(o Options, child semantic.Node) (semantic.Node, error) {
	o.Kind = "dialog"
	o.Actions = ensureAction(ensureAction(o.Actions, Action("close_dialog", "Close", false)), Action("cancel", "Cancel", false))
	o.Relations = append(o.Relations, semantic.Relation{Kind: "owns", Target: child.ID()})
	return node(o, "dialog", semantic.Value{}, []semantic.Node{child})
}
func Confirmation(o Options, prompt string) (semantic.Node, error) {
	o.Kind = "confirmation"
	o.Name = prompt
	o.Actions = []semantic.ActionRef{Action("confirm", "Confirm", true), Action("cancel", "Cancel", false)}
	return node(o, "dialog", semantic.Value{Text: prompt, HasValue: true}, nil)
}
func Modal(o Options, child semantic.Node) (semantic.Node, error) {
	o.Kind = "modal"
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["modal"] = "true"
	o.Metadata["focusTrap"] = "true"
	o.Metadata["focusRestore"] = "opener"
	o.Actions = ensureAction(ensureAction(o.Actions, Action("close_modal", "Close", false)), Action("cancel", "Cancel", false))
	o.Relations = append(o.Relations, semantic.Relation{Kind: "owns", Target: child.ID()})
	return node(o, "dialog", semantic.Value{}, []semantic.Node{child})
}
func Overlay(o Options, child semantic.Node) (semantic.Node, error) {
	o.Kind = "overlay"
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["modal"] = "false"
	return node(o, "group", semantic.Value{}, []semantic.Node{child})
}
func Form(o Options, fields ...semantic.Node) (semantic.Node, error) {
	o.Actions = ensureAction(o.Actions, Action("submit_form", "Submit", true))
	o.Metadata = cloneMeta(o.Metadata)
	invalidCount := 0
	for _, field := range fields {
		if field.Metadata()["invalid"] == "true" {
			invalidCount++
		}
	}
	o.Metadata["invalidCount"] = strconv.Itoa(invalidCount)
	if invalidCount > 0 {
		o.StyleRole = nonempty(o.StyleRole, "status-warning")
		o.Metadata = toneMeta(o.Metadata, "warning")
	}
	return node(o, "form", semantic.Value{}, fields)
}
func Inspector(o Options, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = "inspector"
	return node(o, "region", semantic.Value{}, children)
}
func MasterDetail(o Options, master, detail semantic.Node) (semantic.Node, error) {
	o.Kind = "master-detail"
	o.Actions = []semantic.ActionRef{Action("open_detail", "Open detail", false), Action("back_to_master", "Back", false)}
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["recordFallback"] = "disclosure"
	o.Metadata["focusRestore"] = "master"
	o.Relations = append(o.Relations,
		semantic.Relation{Kind: "owns", Target: master.ID()},
		semantic.Relation{Kind: "controls", Target: detail.ID()},
	)
	return node(o, "group", semantic.Value{}, []semantic.Node{master, detail})
}

// Chart preserves textual meaning for ASCII, no-colour, screen-reader, and
// agent clients. Renderers may add bars or sparklines, but may not replace it.
func Chart(o Options, description string) (semantic.Node, error) {
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["chartDescription"] = description
	return node(o, "img", semantic.Value{Text: description, HasValue: true}, nil)
}
func BarChart(o Options, values []int, description string) (semantic.Node, error) {
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["chartType"] = "bar"
	o.Metadata["values"] = joinInts(values)
	return Chart(o, description)
}
func Sparkline(o Options, values []int, description string) (semantic.Node, error) {
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["chartType"] = "sparkline"
	o.Metadata["values"] = joinInts(values)
	return Chart(o, description)
}

func fieldErrorDecoration(base Options, message string) ([]semantic.Node, []semantic.Relation, error) {
	if message == "" {
		return nil, nil, nil
	}
	errNode, err := node(Options{
		Namespace: base.Namespace,
		View:      base.View,
		Kind:      "error-message",
		Entity:    base.Entity + ":error",
		Slot:      "error",
		Name:      nonempty(base.Name, base.Entity) + " error",
		StyleRole: "status-danger",
		Live:      semantic.LiveAssertive,
		Metadata:  toneMeta(nil, "danger"),
	}, "alert", semantic.Value{Text: message, HasValue: true}, nil)
	if err != nil {
		return nil, nil, err
	}
	return []semantic.Node{errNode}, []semantic.Relation{{Kind: "error-message", Target: errNode.ID()}}, nil
}

func toneMeta(metadata map[string]string, tone string) map[string]string {
	metadata = cloneMeta(metadata)
	metadata["tone"] = tone
	return metadata
}

func fieldStyleRole(invalid bool) string {
	if invalid {
		return "field-danger"
	}
	return "field-default"
}

func fieldTone(invalid bool) string {
	if invalid {
		return "danger"
	}
	return "neutral"
}
func joinInts(values []int) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += strconv.Itoa(v)
	}
	return out
}
