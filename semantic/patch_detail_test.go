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
	before, after := "{}", "{}"
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
