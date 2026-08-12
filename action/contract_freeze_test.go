package action

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/ben-ranford/stave/semantic"
)

func TestNumericEnumExactEqualityInputAndOutput(t *testing.T) {
	r := NewRegistry()
	d := Definition{
		ID: "numeric.v1", Version: "1", Safety: ReadOnly,
		InputSchema:  Schema{ID: "in", JSON: json.RawMessage(`{"enum":[9007199254740993,1e0]}`)},
		OutputSchema: Schema{ID: "out", JSON: json.RawMessage(`{"enum":[9007199254740993,1e0]}`)},
	}
	if err := r.Register(d, func(_ context.Context, c Call, _ any) (any, error) {
		return c.Arguments, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		arg  string
		ok   bool
	}{
		{"equivalent integer", "1", true},
		{"equivalent decimal", "1.0", true},
		{"equivalent exponent", "1e0", true},
		{"large exact value", "9007199254740993", true},
		{"adjacent large value", "9007199254740992", false},
		{"cross type string", `"1"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(tc.arg)})
			if (got.Error == nil) != tc.ok {
				t.Fatalf("arg %s: ok=%v want=%v error=%v", tc.arg, got.Error == nil, tc.ok, got.Error)
			}
		})
	}
	for _, tc := range []struct {
		name string
		out  string
		ok   bool
	}{
		{"equivalent output", "1.0", true},
		{"exact large output", "9007199254740993", true},
		{"adjacent output", "9007199254740992", false},
		{"cross type output", `"1"`, false},
	} {
		t.Run("output "+tc.name, func(t *testing.T) {
			out := Definition{ID: ID("numeric-output." + tc.name), Version: "1", Safety: ReadOnly,
				InputSchema:  Schema{ID: "in", JSON: json.RawMessage(`{}`)},
				OutputSchema: Schema{ID: "out", JSON: json.RawMessage(`{"enum":[9007199254740993,1e0]}`)},
			}
			r2 := NewRegistry()
			if err := r2.Register(out, func(_ context.Context, _ Call, _ any) (any, error) { return json.RawMessage(tc.out), nil }); err != nil {
				t.Fatal(err)
			}
			result := r2.Invoke(context.Background(), Call{ActionID: out.ID, Arguments: json.RawMessage(`{}`)})
			if (result.Error == nil) != tc.ok {
				t.Fatalf("output %s: ok=%v want=%v error=%v", tc.out, result.Error == nil, tc.ok, result.Error)
			}
		})
	}
}

func TestNestedEnumJSONEqualityInputAndOutput(t *testing.T) {
	schema := json.RawMessage(`{"enum":[{"values":[1,1e0,-9007199254740993],"ok":true},[1.0,{"n":-9007199254740993}]]}`)
	r := NewRegistry()
	d := Definition{ID: "nested-enum.v1", Version: "1", Safety: ReadOnly, InputSchema: Schema{ID: "i", JSON: schema}, OutputSchema: Schema{ID: "o", JSON: schema}}
	if err := r.Register(d, func(_ context.Context, c Call, _ any) (any, error) { return c.Arguments, nil }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value string
		ok          bool
	}{
		{"object numeric equivalents", `{"values":[1.0,1e0,-9007199254740993],"ok":true}`, true},
		{"array nested equivalents", `[1e0,{"n":-9007199254740993}]`, true},
		{"adjacent negative", `{"values":[1,1,-9007199254740992],"ok":true}`, false},
		{"cross type nested", `{"values":["1",1,-9007199254740993],"ok":true}`, false},
	} {
		t.Run("input "+tc.name, func(t *testing.T) {
			result := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(tc.value)})
			if (result.Error == nil) != tc.ok {
				t.Fatalf("ok=%v want=%v err=%v", result.Error == nil, tc.ok, result.Error)
			}
		})
	}
	for _, tc := range []struct {
		name, value string
		ok          bool
	}{
		{"object output equivalents", `{"values":[1e0,1.0,-9007199254740993],"ok":true}`, true},
		{"array output equivalents", `[1,{"n":-9007199254740993}]`, true},
		{"adjacent negative output", `{"values":[1,1,-9007199254740992],"ok":true}`, false},
		{"cross type output", `{"values":["1",1,-9007199254740993],"ok":true}`, false},
	} {
		t.Run("output "+tc.name, func(t *testing.T) {
			out := Definition{ID: ID("nested-output." + tc.name), Version: "1", Safety: ReadOnly, InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{}`)}, OutputSchema: Schema{ID: "o", JSON: schema}}
			r2 := NewRegistry()
			if err := r2.Register(out, func(_ context.Context, _ Call, _ any) (any, error) { return json.RawMessage(tc.value), nil }); err != nil {
				t.Fatal(err)
			}
			result := r2.Invoke(context.Background(), Call{ActionID: out.ID, Arguments: json.RawMessage(`{}`)})
			if (result.Error == nil) != tc.ok {
				t.Fatalf("ok=%v want=%v err=%v", result.Error == nil, tc.ok, result.Error)
			}
		})
	}
}

func TestSchemaValidateFailsClosedAndNumericPolicy(t *testing.T) {
	valid := json.RawMessage(`{"type":"object","properties":{"n":{"type":"number"}}}`)
	for _, tc := range []struct{ name, schema, input string }{
		{"unsupported keyword", `{"type":"number","unknown":true}`, `1`},
		{"malformed properties", `{"properties":true}`, `{}`},
		{"impossible bounds", `{"minimum":2,"maximum":1}`, `1`},
		{"trailing schema", `{"type":"number"}{}`, `1`},
		{"hostile exponent", `{"type":"number"}`, `1e10001`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (Schema{ID: "x", JSON: json.RawMessage(tc.schema)}).Validate(json.RawMessage(tc.input))
			if err == nil {
				t.Fatal("accepted invalid schema or numeric literal")
			}
		})
	}
	if _, err := (Schema{ID: "x", JSON: valid}).Validate(json.RawMessage(`{"n":1e10000}`)); err != nil {
		t.Fatalf("numeric boundary rejected: %v", err)
	}
}

func TestInvokeNumericResourceLimit(t *testing.T) {
	t.Run("input does not invoke handler", func(t *testing.T) {
		r := NewRegistry()
		calls := 0
		d := Definition{ID: "resource-input.v1", Version: "1", Safety: ReadOnly, InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{"type":"number"}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{}`)}}
		if err := r.Register(d, func(context.Context, Call, any) (any, error) { calls++; return map[string]any{}, nil }); err != nil {
			t.Fatal(err)
		}
		result := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`1e10001`)})
		if result.Error == nil || result.Error.Code != ResourceLimit {
			t.Fatalf("error=%v, want RESOURCE_LIMIT", result.Error)
		}
		if calls != 0 {
			t.Fatalf("handler calls=%d, want 0", calls)
		}
	})
	t.Run("output maps resource limit", func(t *testing.T) {
		r := NewRegistry()
		d := Definition{ID: "resource-output.v1", Version: "1", Safety: ReadOnly, InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{"type":"number"}`)}}
		if err := r.Register(d, func(context.Context, Call, any) (any, error) { return json.RawMessage(`1e10001`), nil }); err != nil {
			t.Fatal(err)
		}
		result := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`{}`)})
		if result.Error == nil || result.Error.Code != ResourceLimit {
			t.Fatalf("error=%v, want RESOURCE_LIMIT", result.Error)
		}
	})
}

func TestDefinitionValidationEnumsAndDefault(t *testing.T) {
	d := Definition{ID: "enum.v1", Version: "1", InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{}`)}, Safety: ReadOnly}
	if err := d.Validate(); err != nil {
		t.Fatalf("empty idempotency is a supported legacy default: %v", err)
	}
	if got := d.EffectiveIdempotency(); got != NonIdempotent {
		t.Fatalf("default idempotency=%q, want %q", got, NonIdempotent)
	}
	for _, bad := range []Definition{
		{ID: d.ID, Version: d.Version, InputSchema: d.InputSchema, OutputSchema: d.OutputSchema, Safety: Safety("unknown")},
		{ID: d.ID, Version: d.Version, InputSchema: d.InputSchema, OutputSchema: d.OutputSchema, Safety: d.Safety, Idempotency: Idempotency("unknown")},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatal("unknown enum accepted")
		}
	}
}

func TestRegistryDefinitionDeepImmutability(t *testing.T) {
	r := NewRegistry()
	input := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	roles := []semantic.Role{"operator"}
	caps := []string{"terminal.write"}
	d := Definition{ID: "immutable.v1", Version: "1", Safety: ReadOnly, InputSchema: Schema{ID: "i", JSON: input}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{"type":"object"}`)}, TargetRoles: roles, RequiredCaps: caps}
	if err := r.Register(d, func(context.Context, Call, any) (any, error) { return map[string]any{}, nil }); err != nil {
		t.Fatal(err)
	}
	input[0] = '{'
	roles[0] = "mutated"
	caps[0] = "mutated"
	got, ok := r.Definition(d.ID)
	if !ok {
		t.Fatal("definition missing")
	}
	got.InputSchema.JSON[0] = '{'
	got.TargetRoles[0] = "returned-mutation"
	got.RequiredCaps[0] = "returned-mutation"
	manifest := r.Manifest()
	manifest[0].InputSchema.JSON[0] = '{'
	manifest[0].TargetRoles[0] = "manifest-mutation"
	if result := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`{"name":"ok"}`)}); result.Error != nil {
		t.Fatalf("registry changed by caller mutation: %v", result.Error)
	}
}

func TestRegistryDefinitionAccessorClonesConcurrently(t *testing.T) {
	r := NewRegistry()
	d := Definition{ID: "immutable-race.v1", Version: "1", Safety: ReadOnly, InputSchema: Schema{ID: "i", JSON: json.RawMessage(`{"type":"object"}`)}, OutputSchema: Schema{ID: "o", JSON: json.RawMessage(`{"type":"object"}`)}, TargetRoles: []semantic.Role{"operator"}, RequiredCaps: []string{"terminal.write"}}
	if err := r.Register(d, func(context.Context, Call, any) (any, error) { return map[string]any{}, nil }); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				copy, ok := r.Definition(d.ID)
				if !ok {
					t.Error("definition missing")
					return
				}
				copy.InputSchema.JSON[0] = '{'
				copy.TargetRoles[0] = "mutated"
				copy.RequiredCaps[0] = "mutated"
			}
		}()
	}
	wg.Wait()
	if result := r.Invoke(context.Background(), Call{ActionID: d.ID, Arguments: json.RawMessage(`{}`)}); result.Error != nil {
		t.Fatal(result.Error)
	}
}
