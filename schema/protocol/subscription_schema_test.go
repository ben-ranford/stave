package protocol

import (
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/diag"
)

func TestSnapshotSubscriptionSchemaIsSeparateAndValid(t *testing.T) {
	data := readSubscriptionSchema(t)
	var schema struct {
		ID    string                     `json:"$id"`
		OneOf []map[string]string        `json:"oneOf"`
		Defs  map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.ID != "stave.snapshot.subscribe/v1" {
		t.Fatalf("subscription schema id=%q", schema.ID)
	}
	if got, want := refs(schema.OneOf), []string{
		"#/$defs/subscribeRequest",
		"#/$defs/unsubscribeRequest",
		"#/$defs/subscribeSuccessResponse",
		"#/$defs/unsubscribeSuccessResponse",
		"#/$defs/identifiedErrorResponse",
		"#/$defs/subscriptionNotification",
	}; !slices.Equal(got, want) {
		t.Fatalf("subscription schema variants=%v, want %v", got, want)
	}
	requireFragments(t, string(schema.Defs["fullSnapshot"]),
		`"$ref": "../protocol.json#/$defs/snapshotResult"`,
		`"required": ["snapshot"]`,
		`"not": {"anyOf": [{"required": ["actions"]}, {"required": ["patch"]}]}`, `"minimum": 1`)
	assertSubscriptionFullSnapshotObjectContract(t, schema.Defs["fullSnapshot"])
	assertSubscriptionDiagnosticContract(t, schema.Defs["fullSnapshot"])
	for name, method := range map[string]string{
		"subscribeRequest":   "stave.snapshot.subscribe",
		"unsubscribeRequest": "stave.snapshot.unsubscribe",
	} {
		requireFragments(t, string(schema.Defs[name]), `"const": "`+method+`"`, `"$ref": "#/$defs/optionalEmptyParams"`)
	}
	for _, name := range []string{"subscribeSuccessResponse", "unsubscribeSuccessResponse"} {
		requireFragments(t, string(schema.Defs[name]), `"required": ["jsonrpc", "id", "result"]`, `"not": {"required": ["error"]}`)
	}
	requireFragments(t, string(schema.Defs["identifiedErrorResponse"]), `"required": ["jsonrpc", "id", "error"]`, `"not": {"required": ["result"]}`)
	requireFragments(t, string(schema.Defs["subscriptionNotification"]), `"$ref": "#/$defs/fullSnapshot"`, `"provider_failed"`, `"session_closed"`, `"output_limit"`)

	base, err := os.ReadFile("protocol.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(base), "stave.snapshot.subscribe") || strings.Contains(string(base), "stave.snapshot.subscription") {
		t.Fatal("base protocol schema must not absorb the separately negotiated extension")
	}
}

type subscriptionDiagnosticSchema struct {
	Items subscriptionDiagnosticItems `json:"items"`
}

type subscriptionDiagnosticItems struct {
	Required   []string                         `json:"required"`
	Properties subscriptionDiagnosticProperties `json:"properties"`
}

type subscriptionDiagnosticProperties struct {
	Redacted   subscriptionDiagnosticBool   `json:"redacted"`
	Message    subscriptionDiagnosticString `json:"message"`
	Attributes *bool                        `json:"attributes"`
}

type subscriptionDiagnosticBool struct {
	Const bool `json:"const"`
}

type subscriptionDiagnosticString struct {
	Const string `json:"const"`
}

type subscriptionFullSnapshotDefinition struct {
	AllOf []subscriptionFullSnapshotClause `json:"allOf"`
}

type subscriptionFullSnapshotClause struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

func assertSubscriptionFullSnapshotObjectContract(t *testing.T, fullSnapshot json.RawMessage) {
	t.Helper()
	var definition subscriptionFullSnapshotDefinition
	if err := json.Unmarshal(fullSnapshot, &definition); err != nil {
		t.Fatal(err)
	}
	snapshotRaw, ok := definition.AllOf[1].Properties["snapshot"]
	if !ok {
		t.Fatal("full snapshot does not constrain snapshot")
	}
	var snapshot struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "object" {
		t.Fatalf("full snapshot type = %q, want object", snapshot.Type)
	}
}

func assertSubscriptionDiagnosticContract(t *testing.T, fullSnapshot json.RawMessage) {
	t.Helper()
	var definition subscriptionFullSnapshotDefinition
	if err := json.Unmarshal(fullSnapshot, &definition); err != nil {
		t.Fatal(err)
	}
	var diagnostics subscriptionDiagnosticSchema
	if err := json.Unmarshal(definition.AllOf[1].Properties["diagnostics"], &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(diagnostics.Items.Required, []string{"redacted"}) {
		t.Fatalf("diagnostic required fields = %v, want [redacted]", diagnostics.Items.Required)
	}
	if !diagnostics.Items.Properties.Redacted.Const || diagnostics.Items.Properties.Message.Const != "[REDACTED]" || diagnostics.Items.Properties.Attributes == nil || *diagnostics.Items.Properties.Attributes {
		t.Fatalf("diagnostic contract = %+v", diagnostics.Items.Properties)
	}

	runtimeDiagnostic, err := json.Marshal(diag.Diagnostic{
		Redacted:   true,
		Message:    "secret",
		Attributes: map[string]string{"secret": "value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var representation struct {
		Redacted   bool               `json:"redacted"`
		Message    string             `json:"message"`
		Attributes *map[string]string `json:"attributes"`
	}
	if err := json.Unmarshal(runtimeDiagnostic, &representation); err != nil {
		t.Fatal(err)
	}
	if !representation.Redacted || representation.Message != "[REDACTED]" || representation.Attributes != nil {
		t.Fatalf("redacted runtime representation leaked attributes: %s", runtimeDiagnostic)
	}
}

func TestSnapshotSubscriptionSchemaExternalRefsResolveToSiblingProtocol(t *testing.T) {
	retrieval, err := url.Parse("https://schemas.invalid/protocol/snapshot-subscriptions.json")
	if err != nil {
		t.Fatal(err)
	}
	schemaID, err := url.Parse("stave.snapshot.subscribe/v1")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := url.Parse("../protocol.json#/$defs/snapshotResult")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := retrieval.ResolveReference(schemaID).ResolveReference(reference).String(), "https://schemas.invalid/protocol/protocol.json#/$defs/snapshotResult"; got != want {
		t.Fatalf("subscription schema reference resolves to %q, want %q", got, want)
	}
}

func readSubscriptionSchema(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("snapshot-subscriptions.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func refs(variants []map[string]string) []string {
	result := make([]string, len(variants))
	for index, variant := range variants {
		result[index] = variant["$ref"]
	}
	return result
}

func requireFragments(t *testing.T, value string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			t.Fatalf("schema definition must contain %s", fragment)
		}
	}
}
