package human

import (
	"context"
	"errors"
	"time"

	"github.com/ben-ranford/stave/action"
)

// ConfirmationView is the redacted, renderer-neutral data needed to ask a
// person to confirm an action. It deliberately excludes the confirmation
// token, session, arguments, argument hash, target, and policy bindings.
type ConfirmationView struct {
	ActionID      action.ID     `json:"actionId"`
	ActionVersion string        `json:"actionVersion"`
	Safety        action.Safety `json:"safety"`
	ExpiresAt     time.Time     `json:"expiresAt"`
}

// ConfirmationDecision is an explicit human decision. The zero value never
// approves an action.
type ConfirmationDecision string

const (
	ConfirmationCancelled ConfirmationDecision = "cancel"
	ConfirmationConfirmed ConfirmationDecision = "confirm"
)

// ConfirmationPresenter presents a redacted confirmation view through a host
// renderer. It must return ConfirmationConfirmed explicitly before a call can
// be sent to the action registry.
type ConfirmationPresenter interface {
	PresentConfirmation(context.Context, ConfirmationView) (ConfirmationDecision, error)
}

var (
	ErrConfirmationRegistryRequired  = errors.New("confirmation registry is required")
	ErrConfirmationPresenterRequired = errors.New("confirmation presenter is required")
)

// ConfirmationFlow connects a host-owned presenter to the existing action
// registry. It is opt-in and does not issue automatic approvals.
type ConfirmationFlow struct {
	Registry  *action.Registry
	Presenter ConfirmationPresenter
}

// Resolve issues a grant, presents only its redacted view, then routes an
// explicit confirmation through Registry.Invoke. Cancellation removes the
// issued grant and never invokes the action handler.
func (f ConfirmationFlow) Resolve(ctx context.Context, grant action.Confirmation, call action.Call) action.Result {
	if f.Registry == nil {
		return confirmationRejected(call, action.ConfirmationInvalid, ErrConfirmationRegistryRequired.Error())
	}
	if f.Presenter == nil {
		return confirmationRejected(call, action.ConfirmationInvalid, ErrConfirmationPresenterRequired.Error())
	}
	if err := f.Registry.IssueConfirmation(grant); err != nil {
		if errors.Is(err, action.ErrConfirmationLimit) {
			return confirmationRejected(call, action.ResourceLimit, "confirmation capacity reached")
		}
		return confirmationRejected(call, action.ConfirmationInvalid, "confirmation could not be issued")
	}

	decision, err := f.Presenter.PresentConfirmation(ctx, ConfirmationView{
		ActionID:      grant.ActionID,
		ActionVersion: grant.ActionVersion,
		Safety:        grant.Safety,
		ExpiresAt:     grant.ExpiresAt,
	})
	if err != nil || decision != ConfirmationConfirmed {
		f.Registry.CancelConfirmation(grant)
		return confirmationRejected(call, action.ConfirmationInvalid, "confirmation cancelled")
	}

	call.Confirmation = &action.Confirmation{Token: grant.Token, SessionID: grant.SessionID}
	return f.Registry.Invoke(ctx, call)
}

func confirmationRejected(call action.Call, code action.Code, message string) action.Result {
	return action.Result{
		CallID:   call.CallID,
		ActionID: call.ActionID,
		Target:   call.Target,
		Status:   action.ResultRejected,
		Error:    &action.Error{Code: code, Message: message},
	}
}
