package keymap

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ben-ranford/stave/action"
	staveevent "github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

type CommandID string
type RouteKind string
type ResolutionStatus string

// BindingInventory is the immutable, conformance-facing view of a human
// binding. It intentionally contains no dispatcher state and can be compared
// with action.Registry.Manifest by adapters and tests.
type BindingInventory struct {
	Command   CommandID
	Sequence  []input.KeyChord
	Scope     semantic.NodeID
	Priority  int
	Hint      string
	RouteKind RouteKind
	ActionID  action.ID
	EventKind staveevent.Kind
}

const (
	CommandFocusNext     CommandID = "stave.focus.next.v1"
	CommandFocusPrevious CommandID = "stave.focus.previous.v1"
	CommandFocusFirst    CommandID = "stave.focus.first.v1"
	CommandFocusLast     CommandID = "stave.focus.last.v1"
	CommandActivate      CommandID = "stave.activate.v1"
	CommandCancel        CommandID = "stave.cancel.v1"
	CommandShutdown      CommandID = "stave.shutdown.v1"
	CommandPageUp        CommandID = "stave.page.up.v1"
	CommandPageDown      CommandID = "stave.page.down.v1"
)

const (
	RouteEvent  RouteKind = "event"
	RouteAction RouteKind = "action"
)

const (
	StatusNoMatch ResolutionStatus = "no_match"
	StatusPending ResolutionStatus = "pending"
	StatusMatch   ResolutionStatus = "match"
)

type Binding struct {
	Sequence []input.KeyChord `json:"sequence"`
	Command  CommandID        `json:"command"`
	Scope    semantic.NodeID  `json:"scope,omitempty"`
	Priority int              `json:"priority,omitempty"`
}

type Route struct {
	Kind      RouteKind         `json:"kind"`
	ActionID  action.ID         `json:"actionId,omitempty"`
	Arguments json.RawMessage   `json:"arguments,omitempty"`
	EventKind staveevent.Kind   `json:"eventKind,omitempty"`
	Payload   map[string]string `json:"payload,omitempty"`
}

type Mapping struct {
	Binding Binding `json:"binding"`
	Route   Route   `json:"route"`
	Hint    string  `json:"hint,omitempty"`
}

type Resolution struct {
	Status  ResolutionStatus  `json:"status"`
	Binding Binding           `json:"binding,omitempty"`
	Route   Route             `json:"route,omitempty"`
	Hint    string            `json:"hint,omitempty"`
	Call    *action.Call      `json:"-"`
	Event   *staveevent.Event `json:"-"`
}

type Map struct {
	profile  string
	mappings []Mapping
}

type Dispatcher struct {
	keymap          Map
	pending         []input.KeyChord
	pendingDeadline time.Time
}

func New(profile string, mappings []Mapping) (Map, error) {
	if profile == "" {
		profile = "default"
	}
	normalized := make([]Mapping, len(mappings))
	for i, mapping := range mappings {
		if len(mapping.Binding.Sequence) == 0 {
			return Map{}, errors.New("binding sequence cannot be empty")
		}
		if mapping.Binding.Command == "" {
			return Map{}, errors.New("binding command cannot be empty")
		}
		if mapping.Route.Kind != RouteAction && mapping.Route.Kind != RouteEvent {
			return Map{}, errors.New("binding route must be action or event")
		}
		normalized[i] = mapping
		normalized[i].Binding.Sequence = normalizeSequence(mapping.Binding.Sequence)
		normalized[i].Route = cloneRoute(mapping.Route)
		if err := validateRoute(normalized[i].Route); err != nil {
			return Map{}, fmt.Errorf("binding %s: %w", normalized[i].Binding.Command, err)
		}
		if normalized[i].Hint == "" {
			normalized[i].Hint = formatSequence(normalized[i].Binding.Sequence)
		}
	}
	if err := validate(normalized); err != nil {
		return Map{}, err
	}
	return Map{profile: profile, mappings: normalized}, nil
}

func Default() (Map, error) {
	return New("default", []Mapping{
		mapping("tab", CommandFocusNext, actionRoute("focus_next")),
		mapping("shift+tab", CommandFocusPrevious, actionRoute("focus_previous")),
		mapping("down", CommandFocusNext, actionRoute("focus_next")),
		mapping("up", CommandFocusPrevious, actionRoute("focus_previous")),
		mapping("home", CommandFocusFirst, actionRoute("focus_first")),
		mapping("end", CommandFocusLast, actionRoute("focus_last")),
		mapping("pageup", CommandPageUp, actionRoute("previous_page")),
		mapping("pagedown", CommandPageDown, actionRoute("next_page")),
		mapping("enter", CommandActivate, actionRoute("activate")),
		mapping("space", CommandActivate, actionRoute("activate")),
		mapping("esc", CommandCancel, actionRoute("cancel")),
		mapping("ctrl+c", CommandShutdown, eventRoute(staveevent.Shutdown, nil)),
	})
}

func (m Map) Profile() string {
	return m.profile
}

func (m Map) Bindings() []Mapping {
	out := make([]Mapping, len(m.mappings))
	for i, mapping := range m.mappings {
		out[i] = mapping
		out[i].Binding.Sequence = append([]input.KeyChord(nil), mapping.Binding.Sequence...)
		out[i].Route.Arguments = append(json.RawMessage(nil), mapping.Route.Arguments...)
		out[i].Route.Payload = copyPayload(mapping.Route.Payload)
	}
	return out
}

func (m Map) Hints(command CommandID) []string {
	var hints []string
	for _, binding := range m.mappings {
		if binding.Binding.Command == command {
			hints = append(hints, binding.Hint)
		}
	}
	return hints
}

func (m Map) Inventory() []BindingInventory {
	out := make([]BindingInventory, 0, len(m.mappings))
	for _, mapping := range m.mappings {
		out = append(out, BindingInventory{
			Command:   mapping.Binding.Command,
			Sequence:  append([]input.KeyChord(nil), mapping.Binding.Sequence...),
			Scope:     mapping.Binding.Scope,
			Priority:  mapping.Binding.Priority,
			Hint:      mapping.Hint,
			RouteKind: mapping.Route.Kind,
			ActionID:  mapping.Route.ActionID,
			EventKind: mapping.Route.EventKind,
		})
	}
	return out
}

// ActionIDs returns all typed action routes in stable keymap order.
func (m Map) ActionIDs() []action.ID {
	ids := make([]action.ID, 0, len(m.mappings))
	seen := map[action.ID]bool{}
	for _, item := range m.mappings {
		if item.Route.Kind == RouteAction && item.Route.ActionID != "" && !seen[item.Route.ActionID] {
			seen[item.Route.ActionID] = true
			ids = append(ids, item.Route.ActionID)
		}
	}
	return ids
}

func NewDispatcher(keymap Map) *Dispatcher {
	return &Dispatcher{keymap: keymap}
}

func (d *Dispatcher) Reset() {
	d.pending = nil
	d.pendingDeadline = time.Time{}
}

// Pending reports whether a multi-key prefix is awaiting completion.
func (d *Dispatcher) Pending() bool { return len(d.pending) > 0 }

// Expire clears an incomplete key sequence after its configured timeout.
func (d *Dispatcher) Expire(now time.Time) bool {
	if len(d.pending) == 0 || d.pendingDeadline.IsZero() || now.Before(d.pendingDeadline) {
		return false
	}
	d.Reset()
	return true
}

func (d *Dispatcher) Feed(chord input.KeyChord, scope semantic.NodeID, target semantic.Target, deadline time.Time) (Resolution, error) {
	if !d.pendingDeadline.IsZero() && !deadline.IsZero() && deadline.After(d.pendingDeadline) {
		d.Expire(deadline)
	}
	d.pending = append(d.pending, chord.Normalize())
	exact, partials := d.matches(scope)
	if len(exact) == 0 && len(partials) == 0 {
		d.pending = nil
		return Resolution{Status: StatusNoMatch}, nil
	}
	if len(exact) == 0 {
		d.pendingDeadline = deadline
		return Resolution{Status: StatusPending}, nil
	}
	selected, err := selectMapping(exact)
	if err != nil {
		return Resolution{}, err
	}
	d.pending = nil
	d.pendingDeadline = time.Time{}
	resolution := Resolution{
		Status:  StatusMatch,
		Binding: cloneBinding(selected.Binding),
		Route:   cloneRoute(selected.Route),
		Hint:    selected.Hint,
	}
	switch selected.Route.Kind {
	case RouteAction:
		call := action.Call{
			ActionID:  selected.Route.ActionID,
			Target:    target,
			Arguments: append(json.RawMessage(nil), selected.Route.Arguments...),
			Deadline:  deadline,
		}
		resolution.Call = &call
	case RouteEvent:
		ev, err := routeEvent(selected.Route)
		if err != nil {
			return Resolution{}, err
		}
		resolution.Event = &ev
	}
	return resolution, nil
}

func (d *Dispatcher) matches(scope semantic.NodeID) ([]Mapping, []Mapping) {
	var exact []Mapping
	var partials []Mapping
	for _, mapping := range d.keymap.mappings {
		if mapping.Binding.Scope != "" && mapping.Binding.Scope != scope {
			continue
		}
		switch compareSequence(d.pending, mapping.Binding.Sequence) {
		case 0:
			exact = append(exact, mapping)
		case -1:
			partials = append(partials, mapping)
		}
	}
	return exact, partials
}

func validate(mappings []Mapping) error {
	for i := range mappings {
		for j := i + 1; j < len(mappings); j++ {
			left, right := mappings[i], mappings[j]
			if left.Binding.Scope != right.Binding.Scope || left.Binding.Priority != right.Binding.Priority {
				continue
			}
			cmp := compareSequence(left.Binding.Sequence, right.Binding.Sequence)
			if cmp == 0 || isPrefix(left.Binding.Sequence, right.Binding.Sequence) || isPrefix(right.Binding.Sequence, left.Binding.Sequence) {
				return fmt.Errorf("ambiguous bindings for %s and %s", left.Binding.Command, right.Binding.Command)
			}
		}
	}
	return nil
}

func selectMapping(matches []Mapping) (Mapping, error) {
	if len(matches) == 1 {
		return matches[0], nil
	}
	best := matches[0]
	tied := false
	for _, mapping := range matches[1:] {
		switch {
		case mapping.Binding.Scope != "" && best.Binding.Scope == "":
			best = mapping
			tied = false
		case mapping.Binding.Scope == best.Binding.Scope && mapping.Binding.Priority > best.Binding.Priority:
			best = mapping
			tied = false
		case mapping.Binding.Scope == best.Binding.Scope && mapping.Binding.Priority == best.Binding.Priority:
			tied = true
		}
	}
	if tied {
		return Mapping{}, errors.New("ambiguous binding resolution")
	}
	return best, nil
}

func mapping(key string, command CommandID, route Route) Mapping {
	chord, _ := input.ParseKey(key)
	return Mapping{
		Binding: Binding{
			Sequence: []input.KeyChord{chord},
			Command:  command,
		},
		Route: route,
	}
}

func eventRoute(kind staveevent.Kind, payload map[string]string) Route {
	return Route{Kind: RouteEvent, EventKind: kind, Payload: payload}
}

func actionRoute(name string) Route {
	return Route{Kind: RouteAction, ActionID: action.ID(primitive.CanonicalActionID(name))}
}

func normalizeSequence(sequence []input.KeyChord) []input.KeyChord {
	out := make([]input.KeyChord, len(sequence))
	for i, chord := range sequence {
		out[i] = chord.Normalize()
	}
	return out
}

func compareSequence(pending []input.KeyChord, binding []input.KeyChord) int {
	if len(pending) > len(binding) {
		return 1
	}
	for i := range pending {
		if pending[i] != binding[i] {
			return 1
		}
	}
	if len(pending) == len(binding) {
		return 0
	}
	return -1
}

func isPrefix(left, right []input.KeyChord) bool {
	if len(left) > len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func formatSequence(sequence []input.KeyChord) string {
	parts := make([]string, 0, len(sequence))
	for _, chord := range sequence {
		var name string
		switch chord.Code {
		case input.KeyRune:
			name = string(chord.Rune)
		default:
			name = string(chord.Code)
		}
		if chord.Mods&input.ModCtrl != 0 {
			name = "Ctrl+" + name
		}
		if chord.Mods&input.ModShift != 0 {
			name = "Shift+" + name
		}
		if chord.Mods&input.ModAlt != 0 {
			name = "Alt+" + name
		}
		if chord.Mods&input.ModMeta != 0 {
			name = "Meta+" + name
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, " ")
}

func copyPayload(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneBinding(in Binding) Binding {
	in.Sequence = append([]input.KeyChord(nil), in.Sequence...)
	return in
}

func cloneRoute(in Route) Route {
	in.Arguments = append(json.RawMessage(nil), in.Arguments...)
	in.Payload = copyPayload(in.Payload)
	return in
}

func validateRoute(route Route) error {
	switch route.Kind {
	case RouteAction:
		if !validActionID(route.ActionID) {
			return fmt.Errorf("invalid action id %q", route.ActionID)
		}
		if len(route.Arguments) > 0 && !json.Valid(route.Arguments) {
			return errors.New("action arguments must be valid JSON")
		}
	case RouteEvent:
		if _, err := routeEvent(route); err != nil {
			return err
		}
	default:
		return errors.New("unsupported route kind")
	}
	return nil
}

func validActionID(id action.ID) bool {
	raw := string(id)
	if !strings.HasPrefix(raw, "stave.") || !strings.Contains(raw, ".v") {
		return false
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 3 {
		return false
	}
	last := parts[len(parts)-1]
	if len(last) < 2 || last[0] != 'v' {
		return false
	}
	if _, err := strconv.Atoi(last[1:]); err != nil {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func routeEvent(route Route) (staveevent.Event, error) {
	payload := copyPayload(route.Payload)
	switch route.EventKind {
	case staveevent.Focus, staveevent.Blur, staveevent.Tick, staveevent.Cancel, staveevent.Shutdown:
		if len(payload) != 0 {
			return staveevent.Event{}, fmt.Errorf("%s events cannot carry a payload", route.EventKind)
		}
		return staveevent.New(route.EventKind, nil)
	case staveevent.Key:
		key := strings.TrimSpace(payload["key"])
		if key == "" {
			return staveevent.Event{}, errors.New("key event route requires key payload")
		}
		modifiers := splitCSV(payload["modifiers"])
		return staveevent.New(route.EventKind, staveevent.KeyPayload{Key: key, Modifiers: modifiers})
	case staveevent.Text:
		text := payload["text"]
		if strings.TrimSpace(text) == "" {
			return staveevent.Event{}, errors.New("text event route requires text payload")
		}
		return staveevent.New(route.EventKind, staveevent.TextPayload{Text: text, Committed: payload["committed"] != "false"})
	case staveevent.Resize:
		width, err := strconv.Atoi(payload["width"])
		if err != nil {
			return staveevent.Event{}, errors.New("resize event route requires integer width")
		}
		height, err := strconv.Atoi(payload["height"])
		if err != nil {
			return staveevent.Event{}, errors.New("resize event route requires integer height")
		}
		return staveevent.New(route.EventKind, staveevent.ResizePayload{Width: width, Height: height})
	case staveevent.Pointer:
		sourceID := strings.TrimSpace(payload["sourceId"])
		if sourceID == "" {
			return staveevent.Event{}, errors.New("pointer event route requires sourceId")
		}
		x, err := strconv.Atoi(payload["x"])
		if err != nil {
			return staveevent.Event{}, errors.New("pointer event route requires integer x")
		}
		y, err := strconv.Atoi(payload["y"])
		if err != nil {
			return staveevent.Event{}, errors.New("pointer event route requires integer y")
		}
		return staveevent.New(route.EventKind, staveevent.PointerPayload{
			SourceID: sourceID,
			X:        x,
			Y:        y,
			Phase:    payload["phase"],
			Buttons:  splitCSV(payload["buttons"]),
		})
	default:
		return staveevent.Event{}, fmt.Errorf("unsupported event kind %q", route.EventKind)
	}
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
