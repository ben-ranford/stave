package action

import (
	"context"
	"encoding/json"
	"github.com/ben-ranford/stave/semantic"
	"sync"
	"testing"
	"time"
)

func TestRegistryRejectsAndConfirmation(t *testing.T) {
	r := NewRegistry()
	d := Definition{ID: "x.v1", Version: "v1", InputSchema: Schema{ID: "in", JSON: json.RawMessage(`{"type":"object"}`)}, OutputSchema: Schema{ID: "out", JSON: json.RawMessage(`{"type":"object"}`)}, Safety: ReadOnly}
	if err := r.Register(d, func(_ context.Context, _ Call, v any) (any, error) { return v, nil }); err != nil {
		t.Fatal(err)
	}
	res := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`{}`)})
	if res.Status != ResultOK {
		t.Fatal(res.Error)
	}
	d.Confirmation.Required = true
	r2 := NewRegistry()
	_ = r2.Register(d, func(_ context.Context, _ Call, v any) (any, error) { return v, nil })
	res = r2.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`{}`)})
	if res.Error == nil || res.Error.Code != ConfirmationRequired {
		t.Fatal(res)
	}
	c, _ := NewConfirmation("s", d, semantic.Target{}, json.RawMessage(`{}`), time.Now().Add(time.Minute))
	_ = r2.IssueConfirmation(c)
	res = r2.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`{}`), Confirmation: &c, SessionID: "s"})
	if res.Status != ResultOK {
		t.Fatal(res.Error)
	}
}
func TestConfirmationWhitespaceAndReplay(t *testing.T) {
	r := NewRegistry()
	d := Definition{ID: "c.v1", Version: "1", InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{}`)}, Safety: Consequential, Confirmation: ConfirmationPolicy{Required: true}}
	calls := 0
	_ = r.Register(d, func(context.Context, Call, any) (any, error) { calls++; return map[string]any{"ok": true}, nil })
	args := json.RawMessage(` { "x" : 1 } `)
	c, _ := NewConfirmation("s", d, semantic.Target{NodeID: "", Generation: 2, ObservedRevision: 3}, args, time.Now().Add(time.Minute))
	c.RevisionMin = 3
	c.RevisionMax = 3
	c.PolicyID = "p"
	c.PolicyEpoch = 1
	_ = r.IssueConfirmation(c)
	call := Call{ActionID: d.ID, Arguments: json.RawMessage(`{"x":1}`), Target: c.Target, Confirmation: &c, PolicyID: "p", PolicyEpoch: 1, SessionID: "s"}
	if r.Invoke(context.Background(), call).Status != ResultOK {
		t.Fatal("whitespace should match")
	}
	if r.Invoke(context.Background(), call).Error == nil || calls != 1 {
		t.Fatal("replay reached handler")
	}
	c2, _ := NewConfirmation("s", d, c.Target, args, time.Now().Add(time.Minute))
	_ = r.IssueConfirmation(c2)
	c2.Target.Generation = 9
	if r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: args, Target: c.Target, Confirmation: &c2, SessionID: "s"}).Error == nil {
		t.Fatal("target mutation accepted")
	}
}
func TestOutputSchemaViolation(t *testing.T) {
	r := NewRegistry()
	d := Definition{ID: "o.v1", Version: "1", InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{"type":"object"}`)}, Safety: ReadOnly}
	called := false
	_ = r.Register(d, func(context.Context, Call, any) (any, error) { called = true; return "wrong", nil })
	x := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`{}`)})
	if x.Error == nil || x.Error.Code != OutputSchemaViolation || !called {
		t.Fatal(x, called)
	}
}
func TestConcurrentConfirmationSingleUse(t *testing.T) {
	r := NewRegistry()
	d := Definition{ID: "q.v1", Version: "1", InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{"type":"object"}`)}, Safety: Consequential, Confirmation: ConfirmationPolicy{Required: true}}
	n := 0
	_ = r.Register(d, func(context.Context, Call, any) (any, error) { n++; return map[string]any{}, nil })
	c, _ := NewConfirmation("s", d, semantic.Target{}, json.RawMessage(`{}`), time.Now().Add(time.Minute))
	_ = r.IssueConfirmation(c)
	var wg sync.WaitGroup
	ok := 0
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x := r.Invoke(context.Background(), Call{ActionID: d.ID, SessionID: "s", Arguments: json.RawMessage(`{}`), Confirmation: &c})
			if x.Status == ResultOK {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 || n != 1 {
		t.Fatalf("ok=%d handlers=%d", ok, n)
	}
}
func TestTokenOnlyConfirmation(t *testing.T) {
	r := NewRegistry()
	d := Definition{ID: "t.v1", Version: "1", InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{}`)}, Safety: ReadOnly, Confirmation: ConfirmationPolicy{Required: true}}
	_ = r.Register(d, func(context.Context, Call, any) (any, error) { return map[string]any{}, nil })
	g, _ := NewConfirmation("s", d, semantic.Target{}, json.RawMessage(`{}`), time.Now().Add(time.Minute))
	_ = r.IssueConfirmation(g)
	p := Confirmation{Token: g.Token, SessionID: "s"}
	if r.Invoke(context.Background(), Call{ActionID: d.ID, SessionID: "s", Arguments: json.RawMessage(`{}`), Confirmation: &p}).Status != ResultOK {
		t.Fatal("token-only rejected")
	}
}
func TestSchemaKeywordNegatives(t *testing.T) {
	cases := []struct{ name, schema, input string }{{"required", `{"type":"object","required":["x"]}`, `{}`}, {"additional", `{"type":"object","additionalProperties":false,"properties":{}}`, `{"x":1}`}, {"enum", `{"enum":["a"]}`, `"b"`}, {"minimum", `{"minimum":2}`, `1`}, {"maximum", `{"maximum":2}`, `3`}, {"minLength", `{"minLength":2}`, `"a"`}, {"maxLength", `{"maxLength":2}`, `"abc"`}, {"minItems", `{"minItems":2}`, `[1]`}, {"maxItems", `{"maxItems":1}`, `[1,2]`}, {"items", `{"type":"array","items":{"type":"string"}}`, `[1]`}}
	for _, tc := range cases {
		d := Definition{ID: ID("k." + tc.name), Version: "1", InputSchema: Schema{ID: "i", JSON: json.RawMessage(tc.schema)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{}`)}, Safety: ReadOnly}
		r := NewRegistry()
		if r.Register(d, func(context.Context, Call, any) (any, error) { return map[string]any{}, nil }) != nil {
			t.Fatal(tc.name, "register")
		}
		if r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(tc.input)}).Error == nil {
			t.Fatal(tc.name, "accepted")
		}
	}
}
