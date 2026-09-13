package focus

import "github.com/ben-ranford/stave/semantic"

// ModalLifecycle composes Graph scope operations for application-owned dialogs.
type ModalLifecycle struct {
	State  State
	scopes []semantic.NodeID
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
	m.State = next
	m.scopes = append(m.scopes, scope)
	return true
}

// Close restores the initiating target when it survives, otherwise uses Graph's
// deterministic revision repair. Closing an already closed scope is a no-op.
func (m *ModalLifecycle) Close(previous, current Graph, scope semantic.NodeID) bool {
	if m == nil || len(m.scopes) == 0 || m.scopes[len(m.scopes)-1] != scope {
		return false
	}
	next, ok := current.PopScope(m.State)
	if !ok {
		return false
	}
	m.scopes = m.scopes[:len(m.scopes)-1]
	m.State = current.RepairFrom(previous, next)
	return true
}
