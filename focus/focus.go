package focus

import (
	"errors"

	"github.com/ben-ranford/stave/semantic"
)

type State struct {
	Active semantic.Target
	Scope  semantic.NodeID
	Stack  []semantic.Target
}

// FocusState is the contract name used by the system design; State remains a
// concise compatibility alias for applications that adopted the early API.
type FocusState = State

type node struct {
	target    semantic.Target
	parent    semantic.NodeID
	focusable bool
}

type Graph struct {
	revision  uint64
	root      semantic.NodeID
	nodes     map[semantic.NodeID]node
	order     []semantic.Target
	orderByID map[semantic.NodeID]int
}

func NewGraph(tree semantic.Tree) Graph {
	g := Graph{
		revision:  tree.Revision(),
		nodes:     make(map[semantic.NodeID]node),
		orderByID: make(map[semantic.NodeID]int),
	}
	root := tree.Root()
	g.root = root.ID()
	var walk func(parent semantic.NodeID, current semantic.Node)
	walk = func(parent semantic.NodeID, current semantic.Node) {
		flags := current.Flags()
		target := semantic.Target{
			NodeID:           current.ID(),
			Generation:       current.Generation(),
			ObservedRevision: tree.Revision(),
		}
		focusable := flags.Focusable && flags.Visible && !flags.Disabled && !flags.Offscreen
		g.nodes[current.ID()] = node{
			target:    target,
			parent:    parent,
			focusable: focusable,
		}
		if focusable {
			g.orderByID[current.ID()] = len(g.order)
			g.order = append(g.order, target)
		}
		for _, child := range current.Children() {
			walk(current.ID(), child)
		}
	}
	walk("", root)
	return g
}

func (g Graph) Initial(scope semantic.NodeID) (State, bool) {
	target, ok := g.First(scope)
	if !ok {
		return State{}, false
	}
	return State{Active: target, Scope: scope}, true
}

func (g Graph) Revision() uint64 {
	return g.revision
}

func (g Graph) Root() semantic.NodeID {
	return g.root
}

func (g Graph) Focusable() []semantic.Target {
	return append([]semantic.Target(nil), g.order...)
}

func (g Graph) First(scope semantic.NodeID) (semantic.Target, bool) {
	items := g.targetsInScope(scope)
	if len(items) == 0 {
		return semantic.Target{}, false
	}
	return items[0], true
}

// Last returns the last focusable target in a scope.
func (g Graph) Last(scope semantic.NodeID) (semantic.Target, bool) {
	items := g.targetsInScope(scope)
	if len(items) == 0 {
		return semantic.Target{}, false
	}
	return items[len(items)-1], true
}

func (g Graph) Next(state State) (State, bool) {
	items := g.targetsInScope(state.Scope)
	if len(items) == 0 {
		return State{}, false
	}
	index := g.indexOf(items, state.Active)
	index = (index + 1) % len(items)
	next := state
	next.Active = items[index]
	next.Active.ObservedRevision = g.revision
	return next, true
}

func (g Graph) Previous(state State) (State, bool) {
	items := g.targetsInScope(state.Scope)
	if len(items) == 0 {
		return State{}, false
	}
	index := g.indexOf(items, state.Active)
	index = (index - 1 + len(items)) % len(items)
	prev := state
	prev.Active = items[index]
	prev.Active.ObservedRevision = g.revision
	return prev, true
}

func (g Graph) PushScope(state State, scope semantic.NodeID) (State, bool) {
	if scope == "" {
		return state, false
	}
	first, ok := g.First(scope)
	if !ok {
		return state, false
	}
	next := State{
		Active: first,
		Scope:  scope,
		Stack:  append([]semantic.Target(nil), state.Stack...),
	}
	if g.Contains(state.Active) {
		next.Stack = append(next.Stack, state.Active)
	}
	return next, true
}

func (g Graph) PopScope(state State) (State, bool) {
	if len(state.Stack) == 0 {
		if g.Contains(state.Active) {
			return State{
				Active: semantic.Target{
					NodeID:           state.Active.NodeID,
					Generation:       state.Active.Generation,
					ObservedRevision: g.revision,
				},
				Stack: nil,
			}, true
		}
		root, ok := g.First("")
		if !ok {
			return State{}, false
		}
		return State{Active: root}, true
	}
	stack := append([]semantic.Target(nil), state.Stack...)
	restore := stack[len(stack)-1]
	stack = stack[:len(stack)-1]
	next := State{Active: restore, Stack: stack}
	return g.Repair(next), true
}

func (g Graph) Repair(state State) State {
	if g.Contains(state.Active) && g.withinScope(state.Active.NodeID, state.Scope) {
		state.Active.ObservedRevision = g.revision
		return state
	}
	if restored, ok := g.repairFromStack(state); ok {
		return restored
	}
	if first, ok := g.First(state.Scope); ok {
		state.Active = first
		return state
	}
	if state.Scope != "" {
		state.Scope = ""
		if first, ok := g.First(""); ok {
			state.Active = first
		}
	}
	return state
}

func (g Graph) RepairFrom(previous Graph, state State) State {
	if g.Contains(state.Active) && g.withinScope(state.Active.NodeID, state.Scope) {
		state.Active.ObservedRevision = g.revision
		return state
	}
	if index, ok := previous.orderByID[state.Active.NodeID]; ok {
		scopeItems := g.targetsInScope(state.Scope)
		if candidate, ok := g.nearest(scopeItems, previous.order, index); ok {
			state.Active = candidate
			return state
		}
	}
	return g.Repair(state)
}

func (g Graph) Contains(target semantic.Target) bool {
	entry, ok := g.nodes[target.NodeID]
	if !ok {
		return false
	}
	return entry.focusable && entry.target.Generation == target.Generation
}

func (g Graph) Validate(state State) error {
	if len(g.order) == 0 {
		return nil
	}
	if !g.Contains(state.Active) {
		return errors.New("active focus target is not focusable")
	}
	if state.Scope != "" {
		if _, ok := g.nodes[state.Scope]; !ok {
			return errors.New("scope does not exist")
		}
		if !g.withinScope(state.Active.NodeID, state.Scope) {
			return errors.New("active target is outside scope")
		}
	}
	return nil
}

func (g Graph) repairFromStack(state State) (State, bool) {
	for i := len(state.Stack) - 1; i >= 0; i-- {
		candidate := state.Stack[i]
		if g.Contains(candidate) && g.withinScope(candidate.NodeID, state.Scope) {
			state.Active = candidate
			state.Active.ObservedRevision = g.revision
			state.Stack = append([]semantic.Target(nil), state.Stack[:i]...)
			return state, true
		}
	}
	return State{}, false
}

func (g Graph) nearest(current []semantic.Target, previous []semantic.Target, start int) (semantic.Target, bool) {
	if len(current) == 0 {
		return semantic.Target{}, false
	}
	currentByID := make(map[semantic.NodeID]semantic.Target, len(current))
	for _, target := range current {
		currentByID[target.NodeID] = target
	}
	for delta := 1; delta <= len(previous); delta++ {
		right := start + delta
		if right >= 0 && right < len(previous) {
			if target, ok := currentByID[previous[right].NodeID]; ok {
				return target, true
			}
		}
		left := start - delta
		if left >= 0 && left < len(previous) {
			if target, ok := currentByID[previous[left].NodeID]; ok {
				return target, true
			}
		}
	}
	return semantic.Target{}, false
}

func (g Graph) targetsInScope(scope semantic.NodeID) []semantic.Target {
	if scope == "" {
		return append([]semantic.Target(nil), g.order...)
	}
	filtered := make([]semantic.Target, 0, len(g.order))
	for _, target := range g.order {
		if g.withinScope(target.NodeID, scope) {
			filtered = append(filtered, target)
		}
	}
	return filtered
}

func (g Graph) withinScope(id, scope semantic.NodeID) bool {
	if scope == "" {
		return true
	}
	current := id
	for current != "" {
		if current == scope {
			return true
		}
		entry, ok := g.nodes[current]
		if !ok {
			return false
		}
		current = entry.parent
	}
	return false
}

func (g Graph) indexOf(items []semantic.Target, active semantic.Target) int {
	for index, target := range items {
		if target.NodeID == active.NodeID && target.Generation == active.Generation {
			return index
		}
	}
	return -1
}
