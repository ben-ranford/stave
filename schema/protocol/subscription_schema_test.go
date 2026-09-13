package protocol

import (
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
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
