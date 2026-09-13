package human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/semantic"
)

type confirmationPresenterFunc func(context.Context, ConfirmationView) (ConfirmationDecision, error)

func (f confirmationPresenterFunc) PresentConfirmation(ctx context.Context, view ConfirmationView) (ConfirmationDecision, error) {
	return f(ctx, view)
}

func TestConfirmationFlowConfirmsThroughRegistryWithRedactedView(t *testing.T) {
	registry, definition, invoked := confirmationRegistry(t)
	now := time.Now().Add(time.Minute)
	grant := confirmationGrant(t, definition, now)
	var got ConfirmationView
	flow := ConfirmationFlow{
		Registry: registry,
		Presenter: confirmationPresenterFunc(func(_ context.Context, view ConfirmationView) (ConfirmationDecision, error) {
			got = view
			return ConfirmationConfirmed, nil
		}),
	}
	call := confirmationCall(definition)
	result := flow.Resolve(context.Background(), grant, call)
	if result.Status != action.ResultOK || result.Error != nil || *invoked != 1 {
		t.Fatalf("confirmation result = %+v, invoked=%d", result, *invoked)
	}
	if got.ActionID != definition.ID || got.ActionVersion != definition.Version || got.Safety != definition.Safety || !got.ExpiresAt.Equal(now) {
		t.Fatalf("view = %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{grant.Token, grant.SessionID, string(grant.Target.NodeID), grant.ArgHash, grant.PolicyID} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("redacted view leaked %q: %s", secret, encoded)
		}
	}
}

func TestConfirmationFlowCancelRevokesIssuedGrant(t *testing.T) {
	registry, definition, invoked := confirmationRegistry(t)
	grant := confirmationGrant(t, definition, time.Now().Add(time.Minute))
	flow := ConfirmationFlow{Registry: registry, Presenter: confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		return ConfirmationCancelled, nil
	})}
	result := flow.Resolve(context.Background(), grant, confirmationCall(definition))
	if result.Error == nil || result.Error.Code != action.ConfirmationInvalid || *invoked != 0 {
		t.Fatalf("cancel result = %+v, invoked=%d", result, *invoked)
	}
	call := confirmationCall(definition)
	call.Confirmation = &action.Confirmation{Token: grant.Token, SessionID: grant.SessionID}
	if replay := registry.Invoke(context.Background(), call); replay.Error == nil || replay.Error.Code != action.ConfirmationInvalid {
		t.Fatalf("cancelled grant was accepted: %+v", replay)
	}
	if err := registry.IssueConfirmation(grant); err == nil {
		t.Fatal("cancelled token was reissued")
	}
}

func TestConfirmationFlowKeepsStagedGrantInactiveUntilConfirmed(t *testing.T) {
	registry, definition, invoked := confirmationRegistry(t)
	grant := confirmationGrant(t, definition, time.Now().Add(time.Minute))
	started := make(chan struct{})
	release := make(chan struct{})
	flow := ConfirmationFlow{Registry: registry, Presenter: confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		close(started)
		<-release
		return ConfirmationConfirmed, nil
	})}
	resolved := make(chan action.Result, 1)
	go func() {
		resolved <- flow.Resolve(context.Background(), grant, confirmationCall(definition))
	}()
	<-started
	if err := registry.IssueConfirmation(grant); err == nil {
		t.Fatal("staged grant was issued before confirmation")
	}
	concurrent := confirmationCall(definition)
	concurrent.Confirmation = &action.Confirmation{Token: grant.Token, SessionID: grant.SessionID}
	if result := registry.Invoke(context.Background(), concurrent); result.Error == nil || result.Error.Code != action.ConfirmationInvalid || *invoked != 0 {
		t.Fatalf("staged grant invoked before confirmation: %+v, invoked=%d", result, *invoked)
	}
	close(release)
	if result := <-resolved; result.Status != action.ResultOK || *invoked != 1 {
		t.Fatalf("confirmed result = %+v, invoked=%d", result, *invoked)
	}
}

func TestConfirmationFlowRejectsActionMismatchAndPresenterFailure(t *testing.T) {
	registry, definition, invoked := confirmationRegistry(t)
	grant := confirmationGrant(t, definition, time.Now().Add(time.Minute))
	presented := false
	flow := ConfirmationFlow{Registry: registry, Presenter: confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		presented = true
		return ConfirmationConfirmed, nil
	})}
	call := confirmationCall(definition)
	call.ActionID = "other.v1"
	if result := flow.Resolve(context.Background(), grant, call); result.Error == nil || result.Error.Code != action.ConfirmationInvalid || presented || *invoked != 0 {
		t.Fatalf("mismatch result = %+v presented=%t invoked=%d", result, presented, *invoked)
	}
	secret := "presenter-secret"
	flow.Presenter = confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		return ConfirmationCancelled, errors.New(secret)
	})
	result := flow.Resolve(context.Background(), grant, confirmationCall(definition))
	if result.Error == nil || result.Error.Code != action.Internal || strings.Contains(result.Error.Message, secret) {
		t.Fatalf("presenter error result = %+v", result)
	}
	if err := registry.IssueConfirmation(grant); err == nil {
		t.Fatal("presenter failure token was reissued")
	}
}

func TestConfirmationFlowRevokesUnconsumedGrantAfterInvoke(t *testing.T) {
	registry, definition, _ := confirmationRegistry(t)
	grant := confirmationGrant(t, definition, time.Now().Add(time.Minute))
	flow := ConfirmationFlow{Registry: registry, Presenter: confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		return ConfirmationConfirmed, nil
	})}
	call := confirmationCall(definition)
	call.Arguments = json.RawMessage(`[]`)
	if result := flow.Resolve(context.Background(), grant, call); result.Error == nil || result.Error.Code != action.InvalidArgument {
		t.Fatalf("invalid invocation result = %+v", result)
	}
	if err := registry.IssueConfirmation(grant); err == nil {
		t.Fatal("unconsumed grant was reissued")
	}
}

func TestConfirmationFlowCannotBypassGrantBinding(t *testing.T) {
	registry, definition, invoked := confirmationRegistry(t)
	grant := confirmationGrant(t, definition, time.Now().Add(time.Minute))
	flow := ConfirmationFlow{Registry: registry, Presenter: confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		return ConfirmationConfirmed, nil
	})}
	call := confirmationCall(definition)
	call.Arguments = json.RawMessage(`{"changed":true}`)
	result := flow.Resolve(context.Background(), grant, call)
	if result.Error == nil || result.Error.Code != action.ConfirmationInvalid || *invoked != 0 {
		t.Fatalf("mismatched call bypassed confirmation: %+v, invoked=%d", result, *invoked)
	}
}

func TestConfirmationFlowRejectsExpiredGrantBeforePresentation(t *testing.T) {
	registry, definition, _ := confirmationRegistry(t)
	presented := false
	flow := ConfirmationFlow{Registry: registry, Presenter: confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		presented = true
		return ConfirmationConfirmed, nil
	})}
	result := flow.Resolve(context.Background(), confirmationGrant(t, definition, time.Now().Add(-time.Minute)), confirmationCall(definition))
	if result.Error == nil || result.Error.Code != action.ConfirmationInvalid || presented {
		t.Fatalf("expired confirmation result = %+v, presented=%t", result, presented)
	}
}

func TestConfirmationFlowReportsRegistryCapacity(t *testing.T) {
	registry, definition, _ := confirmationRegistry(t)
	expires := time.Now().Add(time.Hour)
	for i := 0; i < 1024; i++ {
		if err := registry.IssueConfirmation(action.Confirmation{Token: fmt.Sprintf("grant-%d", i), SessionID: "session", ExpiresAt: expires}); err != nil {
			t.Fatalf("issue confirmation %d: %v", i, err)
		}
	}
	presented := false
	flow := ConfirmationFlow{Registry: registry, Presenter: confirmationPresenterFunc(func(context.Context, ConfirmationView) (ConfirmationDecision, error) {
		presented = true
		return ConfirmationConfirmed, nil
	})}
	result := flow.Resolve(context.Background(), confirmationGrant(t, definition, expires), confirmationCall(definition))
	if result.Error == nil || result.Error.Code != action.ResourceLimit || presented {
		t.Fatalf("capacity result = %+v, presented=%t", result, presented)
	}
}

func confirmationRegistry(t *testing.T) (*action.Registry, action.Definition, *int) {
	t.Helper()
	registry := action.NewRegistry()
	definition := action.Definition{
		ID:           "danger.v1",
		Version:      "1",
		InputSchema:  action.Schema{ID: "in", JSON: json.RawMessage(`{"type":"object"}`)},
		OutputSchema: action.Schema{ID: "out", JSON: json.RawMessage(`{"type":"object"}`)},
		Safety:       action.Consequential,
		Confirmation: action.ConfirmationPolicy{Required: true},
	}
	invoked := 0
	if err := registry.Register(definition, func(context.Context, action.Call, any) (any, error) {
		invoked++
		return json.RawMessage(`{}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	return registry, definition, &invoked
}

func confirmationGrant(t *testing.T, definition action.Definition, expires time.Time) action.Confirmation {
	t.Helper()
	grant, err := action.NewConfirmation("secret-session", definition, semantic.Target{NodeID: "secret-target", Generation: 1, ObservedRevision: 2}, json.RawMessage(`{}`), expires)
	if err != nil {
		t.Fatal(err)
	}
	grant.PolicyID = "secret-policy"
	grant.PolicyEpoch = 7
	return grant
}

func confirmationCall(definition action.Definition) action.Call {
	return action.Call{
		CallID:      "call",
		ActionID:    definition.ID,
		Target:      semantic.Target{NodeID: "secret-target", Generation: 1, ObservedRevision: 2},
		Arguments:   json.RawMessage(`{}`),
		SessionID:   "secret-session",
		PolicyID:    "secret-policy",
		PolicyEpoch: 7,
	}
}
