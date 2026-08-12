// Package primitive contains renderer-independent, brand-neutral UI primitives.
// Every constructor produces the same semantic tree used by human, accessible,
// and agent clients. Primitives intentionally carry style intent, never colours.
package primitive

import (
	"errors"
	"strings"

	"github.com/ben-ranford/stave/semantic"
)

type Options struct {
	Namespace, View, Kind, Entity, Slot, Name, Description string
	Generation                                             uint32
	Actions                                                []semantic.ActionRef
	Relations                                              []semantic.Relation
	Metadata                                               map[string]string
	StyleRole                                              string
	Live                                                   semantic.LiveMode
	Focusable, Disabled, Visible, Sensitive, Hidden        bool
}

func (o Options) key(kind string) *semantic.NodeKey {
	if o.Kind != "" {
		kind = o.Kind
	}
	return &semantic.NodeKey{AppNamespace: nonempty(o.Namespace, "app"), View: nonempty(o.View, "view"), Kind: kind, Entity: o.Entity, Slot: nonempty(o.Slot, "main")}
}
func nonempty(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}
func cloneMeta(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
func node(o Options, role semantic.Role, value semantic.Value, children []semantic.Node, states ...semantic.State) (semantic.Node, error) {
	if strings.TrimSpace(o.Entity) == "" {
		return semantic.Node{}, errors.New("primitive entity is required for stable identity")
	}
	if o.Generation == 0 {
		o.Generation = 1
	}
	if o.Name == "" {
		o.Name = nonempty(o.Entity, string(role))
	}
	// Disabled controls remain visible; applications can explicitly hide through
	// the semantic tree rather than overloading the zero value of Options.
	o.Visible = !o.Hidden
	if o.Hidden || o.Disabled {
		o.Focusable = false
	}
	return semantic.NewNode(semantic.NodeSpec{
		Key:         o.key(string(role)),
		Generation:  o.Generation,
		Role:        role,
		Name:        o.Name,
		Description: o.Description,
		Value:       value,
		States:      states,
		Relations:   o.Relations,
		Actions:     o.Actions,
		Style:       semantic.StyleIntent{Role: o.StyleRole},
		Children:    children,
		Flags: semantic.Flags{
			Visible:   o.Visible,
			Focusable: o.Focusable,
			Disabled:  o.Disabled,
			Sensitive: o.Sensitive,
			Live:      o.Live,
			Stability: semantic.Stable,
		},
		Metadata: o.Metadata,
	})
}
func CanonicalActionID(id string) semantic.ActionID {
	if !strings.HasPrefix(id, "stave.") {
		id = "stave.primitive.core." + id + ".v1"
	}
	return semantic.ActionID(id)
}
func Action(id, label string, def bool) semantic.ActionRef {
	return semantic.ActionRef{ID: CanonicalActionID(id), Label: label, Default: def}
}
func textNode(o Options, role semantic.Role, text string) (semantic.Node, error) {
	return node(o, role, semantic.Value{Text: text, HasValue: true}, nil)
}

func Text(o Options, text string) (semantic.Node, error) { return textNode(o, "text", text) }
func Label(o Options, text string) (semantic.Node, error) {
	o.Kind = "label"
	return textNode(o, "text", text)
}
func Heading(o Options, text string) (semantic.Node, error)   { return textNode(o, "heading", text) }
func CodeBlock(o Options, text string) (semantic.Node, error) { return textNode(o, "code", text) }
func Spacer(o Options) (semantic.Node, error)                 { return node(o, "separator", semantic.Value{}, nil) }
func Divider(o Options) (semantic.Node, error)                { return node(o, "separator", semantic.Value{}, nil) }
func Button(o Options, label string) (semantic.Node, error) {
	o.Focusable = !o.Disabled
	o.Actions = ensureAction(o.Actions, Action("activate", "Activate", true))
	return textNode(o, "button", label)
}
func Link(o Options, label string) (semantic.Node, error) {
	o.Focusable = !o.Disabled
	o.Actions = ensureAction(o.Actions, Action("open", "Open", true))
	return textNode(o, "link", label)
}
func Section(o Options, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = nonempty(o.Kind, "section")
	return node(o, "region", semantic.Value{}, children)
}
func ensureAction(in []semantic.ActionRef, a semantic.ActionRef) []semantic.ActionRef {
	for _, x := range in {
		if x.ID == a.ID {
			return in
		}
	}
	return append(append([]semantic.ActionRef(nil), in...), a)
}

type Direction string

const (
	Vertical   Direction = "vertical"
	Horizontal Direction = "horizontal"
	Grid       Direction = "grid"
)

func group(o Options, direction Direction, children ...semantic.Node) (semantic.Node, error) {
	o.Metadata = cloneMeta(o.Metadata)
	o.Metadata["direction"] = string(direction)
	return node(o, "group", semantic.Value{}, children)
}
func Stack(o Options, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = nonempty(o.Kind, "stack")
	return group(o, Vertical, children...)
}
func Row(o Options, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = nonempty(o.Kind, "row")
	return group(o, Horizontal, children...)
}
func GridLayout(o Options, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = nonempty(o.Kind, "grid")
	return group(o, Grid, children...)
}
func Surface(o Options, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = nonempty(o.Kind, "surface")
	return node(o, "region", semantic.Value{}, children)
}
func Frame(o Options, children ...semantic.Node) (semantic.Node, error) {
	o.Kind = nonempty(o.Kind, "frame")
	return node(o, "region", semantic.Value{}, children)
}

func requirePositive(name string, n int) error {
	if n < 0 {
		return errors.New(name + " must not be negative")
	}
	return nil
}
