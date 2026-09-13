package focus

import "github.com/ben-ranford/stave/semantic"

// ModalLifecycle composes Graph scope operations for application-owned dialogs.
type ModalLifecycle struct {
	State  State
	scopes []modalScope
}

type modalScope struct {
	scope       semantic.NodeID
	previous    semantic.NodeID
	initiator   semantic.Target
	returnFrame bool
}

func NewModalLifecycle(state State) ModalLifecycle { return ModalLifecycle{State: state} }
func (m *ModalLifecycle) Open(g Graph, scope semantic.NodeID) bool {
	if m == nil {
		return false
	}
	next, ok := g.PushScope(m.State, scope)
	if !ok {
		return false
	}
	m.scopes = append(m.scopes, modalScope{scope: scope, previous: m.State.Scope, initiator: m.State.Active, returnFrame: len(next.Stack) > len(m.State.Stack)})
	m.State = next
	return true
}

// Close restores the initiating target when it survives, otherwise uses Graph's
// deterministic revision repair. Closing an already closed scope is a no-op.
func (m *ModalLifecycle) Close(previous, current Graph, scope semantic.NodeID) bool {
	if m == nil || len(m.scopes) == 0 || m.scopes[len(m.scopes)-1].scope != scope {
		return false
	}
	frame := m.scopes[len(m.scopes)-1]
	next := State{
		Active: m.State.Active,
		Stack:  append([]semantic.Target(nil), m.State.Stack...),
	}
	if frame.returnFrame && len(next.Stack) > 0 {
		next.Stack = next.Stack[:len(next.Stack)-1]
	} else if !current.Contains(next.Active) {
		if _, ok := current.First(""); !ok {
			return false
		}
	}
	next.Active = frame.initiator
	m.scopes = m.scopes[:len(m.scopes)-1]
	next.Scope = frame.previous
	m.State = current.RepairFrom(previous, next)
	return true
}
