package semantic

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/internal/canonical"
)

func TestPatchDetailValidateValueEndpoints(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		valid bool
	}{
		{"empty", `{}`, true},
		{"ordinary", `{"hasValue":true,"text":"visible"}`, true},
		{"text only", `{"text":"visible"}`, true},
		{"redacted", `{"hasValue":true,"redacted":true}`, true},
		{"explicit false", `{"hasValue":false,"redacted":false,"text":""}`, true},
		{"allowed whitespace", `{"text":"line\nnext\tcell\r"}`, true},
		{"redacted text", `{"hasValue":true,"redacted":true,"text":"secret-endpoint-marker"}`, false},
		{"null", `null`, false},
		{"array", `[]`, false},
		{"string", `"visible"`, false},
		{"number", `1`, false},
		{"boolean", `true`, false},
		{"unknown field", `{"extra":true}`, false},
		{"case alias", `{"Redacted":true}`, false},
		{"conflicting alias", `{"Redacted":true,"redacted":false,"text":"secret-endpoint-marker"}`, false},
		{"numeric text", `{"text":1}`, false},
		{"null text", `{"text":null}`, false},
		{"string redacted", `{"redacted":"true"}`, false},
		{"null redacted", `{"redacted":null}`, false},
		{"numeric hasValue", `{"hasValue":1}`, false},
		{"null hasValue", `{"hasValue":null}`, false},
		{"unsafe text", `{"text":"\u0001"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, endpoint := range []string{"before", "after"} {
				t.Run(endpoint, func(t *testing.T) {
					checkExternalPatchDetailValue(t, tc.raw, endpoint, tc.valid)
				})
			}
		})
	}
}

func checkExternalPatchDetailValue(t *testing.T, raw, endpoint string, valid bool) {
	t.Helper()
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "value", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := canonical.JSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	companion := "{}"
	var endpointValue Value
	if err := json.Unmarshal(value, &endpointValue); err == nil && endpointValue.Redacted {
		companion = `{"redacted":true}`
	}
	before, after := companion, companion
	if endpoint == "before" {
		before = string(value)
	} else {
		after = string(value)
	}
	wire := fmt.Sprintf(`{"schemaVersion":"stave.semantic.patch-detail/v1","fromRevision":1,"toRevision":2,"changed":[{"nodeId":%q,"fields":[{"path":"/value","before":%s,"after":%s}]}]}`, id, before, after)
	var detail PatchDetail
	if err := json.Unmarshal([]byte(wire), &detail); err != nil {
		t.Fatal(err)
	}
	err = detail.Validate()
	if (err == nil) != valid {
		t.Fatalf("Validate() error = %v, valid = %t", err, valid)
	}
	if err != nil && strings.Contains(err.Error(), "secret-endpoint-marker") {
		t.Fatal("validation error echoed endpoint text")
	}
}

func TestPatchDetailChangedIDsExcludeAddedAndRemoved(t *testing.T) {
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "identity", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{"added", "removed", "generationChanged"} {
		t.Run(set, func(t *testing.T) {
			wire := fmt.Sprintf(`{"schemaVersion":"stave.semantic.patch-detail/v1","fromRevision":1,"toRevision":2,%q:[%q],"changed":[{"nodeId":%q,"fields":[{"path":"/name","before":"old","after":"new"}]}]}`, set, id, id)
			var detail PatchDetail
			if err := json.Unmarshal([]byte(wire), &detail); err != nil {
				t.Fatal(err)
			}
			if err := detail.Validate(); (err == nil) != (set == "generationChanged") {
				t.Fatalf("Validate() error = %v for %s overlap", err, set)
			}
		})
	}
}

func TestPatchDetailSensitiveFlagEndpoints(t *testing.T) {
	const redacted = `{"hasValue":true,"redacted":true}`
	const plaintext = `{"hasValue":true,"text":"secret-endpoint-marker"}`
	cases := []struct {
		name, flagsBefore, flagsAfter, valueBefore, valueAfter string
		valid                                                  bool
	}{
		{"ordinary flags only", `{"sensitive":false}`, `{"sensitive":false}`, "", "", true},
		{"stable sensitive flags only", `{"sensitive":true}`, `{"sensitive":true}`, "", "", true},
		{"enable missing value", `{"sensitive":false}`, `{"sensitive":true}`, "", "", false},
		{"disable missing value", `{"sensitive":true}`, `{"sensitive":false}`, "", "", false},
		{"enable redacted", `{"sensitive":false}`, `{"sensitive":true}`, redacted, redacted, true},
		{"disable redacted", `{"sensitive":true}`, `{"sensitive":false}`, redacted, redacted, true},
		{"enable plaintext before", `{"sensitive":false}`, `{"sensitive":true}`, plaintext, redacted, false},
		{"enable plaintext after", `{"sensitive":false}`, `{"sensitive":true}`, redacted, plaintext, false},
		{"disable plaintext before", `{"sensitive":true}`, `{"sensitive":false}`, plaintext, redacted, false},
		{"disable plaintext after", `{"sensitive":true}`, `{"sensitive":false}`, redacted, plaintext, false},
		{"stable sensitive plaintext", `{"sensitive":true}`, `{"sensitive":true}`, plaintext, plaintext, false},
		{"ordinary plaintext", `{"sensitive":false}`, `{"sensitive":false}`, plaintext, plaintext, true},
		{"case alias", `{"Sensitive":true}`, `{"sensitive":false}`, plaintext, plaintext, false},
		{"null sensitivity", `{"sensitive":null}`, `{"sensitive":false}`, "", "", false},
		{"string sensitivity", `{"sensitive":"true"}`, `{"sensitive":false}`, "", "", false},
		{"nonobject flags", `true`, `{"sensitive":false}`, "", "", false},
	}
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "sensitive", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		fields := []FieldChange{{Path: "/flags", Before: json.RawMessage(tc.flagsBefore), After: json.RawMessage(tc.flagsAfter)}}
		if tc.valueBefore != "" {
			fields = append(fields, FieldChange{Path: "/value", Before: json.RawMessage(tc.valueBefore), After: json.RawMessage(tc.valueAfter)})
		}
		detail := PatchDetail{SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2, Changed: []NodeChange{{NodeID: id, Fields: fields}}}
		t.Run(tc.name, func(t *testing.T) {
			checkDecodedPatchDetail(t, detail, tc.valid)
		})
	}
}

func checkDecodedPatchDetail(t *testing.T, detail PatchDetail, valid bool) {
	t.Helper()
	wire, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PatchDetail
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	err = decoded.Validate()
	if (err == nil) != valid {
		t.Fatalf("Validate() error = %v, valid = %t", err, valid)
	}
	if err != nil && strings.Contains(err.Error(), "secret-endpoint-marker") {
		t.Fatal("validation error echoed endpoint text")
	}
}

func TestDiffDetailSensitiveFlagsKeepValidGeneratedChanges(t *testing.T) {
	for _, remainsSensitive := range []bool{true, false} {
		t.Run(fmt.Sprint(remainsSensitive), func(t *testing.T) {
			checkGeneratedSensitiveFlagChange(t, remainsSensitive)
		})
	}
}

func checkGeneratedSensitiveFlagChange(t *testing.T, remainsSensitive bool) {
	t.Helper()
	id, err := NodeIDFor(NodeKey{"detail", "generated", "text", "sensitive", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	beforeNode, err := NewNode(NodeSpec{ID: id, Role: "text", Value: SecretValue(), Flags: Flags{Sensitive: true}})
	if err != nil {
		t.Fatal(err)
	}
	afterNode, err := NewNode(NodeSpec{ID: id, Role: "text", Value: SecretValue(), Flags: Flags{Sensitive: remainsSensitive, Visible: true}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewTree(1, beforeNode)
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewTree(2, afterNode)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := DiffDetail(before, after, PatchDetailV1)
	if err != nil {
		t.Fatal(err)
	}
	wantFields := 2
	if remainsSensitive {
		wantFields = 1
	}
	if len(detail.Changed) != 1 || len(detail.Changed[0].Fields) != wantFields {
		t.Fatalf("generated fields = %#v, want %d", detail.Changed, wantFields)
	}
}

func TestPatchDetailRedactionAppliesAcrossValueEndpoints(t *testing.T) {
	const secret = `{"hasValue":true,"redacted":true}`
	const plain = `{"hasValue":true,"text":"secret-endpoint-marker"}`
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "redaction-pair", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, before, after string
		valid               bool
	}{
		{"redact", plain, secret, false},
		{"unredact", secret, plain, false},
		{"empty then redacted", `{}`, secret, false},
		{"redacted then empty", secret, `{}`, false},
		{"both redacted", secret, secret, true},
		{"both ordinary", plain, plain, true},
	}
	for _, tc := range cases {
		for _, withFlags := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/flags=%t", tc.name, withFlags), func(t *testing.T) {
				fields := []FieldChange{}
				if withFlags {
					fields = append(fields, FieldChange{Path: "/flags", Before: json.RawMessage(`{"sensitive":false}`), After: json.RawMessage(`{"sensitive":false}`)})
				}
				fields = append(fields, FieldChange{Path: "/value", Before: json.RawMessage(tc.before), After: json.RawMessage(tc.after)})
				detail := PatchDetail{SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2, Changed: []NodeChange{{NodeID: id, Fields: fields}}}
				checkDecodedPatchDetail(t, detail, tc.valid)
			})
		}
	}
}
