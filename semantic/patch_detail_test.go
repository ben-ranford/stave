package semantic

import (
	"encoding/json"
	"fmt"
	"strconv"
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
	companion := `{"hasValue":true}`
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

func TestPatchDetailValidateNonValueEndpoints(t *testing.T) {
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "field", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	maxInt := "9223372036854775807e0"
	if strconv.IntSize == 32 {
		maxInt = "2147483647e0"
	}
	child, err := NodeIDFor(NodeKey{"detail", "external", "text", "child", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	children := fmt.Sprintf("[%q]", child)
	relations := fmt.Sprintf(`[{"kind":"any-kind","target":%q}]`, id)
	cases := []struct {
		path           string
		valid, invalid []string
	}{
		{"/actions", []string{`null`, `[{"default":true,"id":"any-action","label":"label"}]`}, []string{`true`, `[{"ID":"any-action"}]`, `[{"extra":true,"id":"any-action"}]`}},
		{"/children", []string{`[]`, children}, []string{`true`, `null`}},
		{"/description", []string{`"description"`}, []string{`17`, `"\u0001"`}},
		{"/flags", []string{`{"disabled":false,"focusable":false,"live":"any-live","offscreen":false,"sensitive":false,"stability":"any-stability","visible":true}`}, []string{`true`, `{"Visible":true}`, `{"unknown":true}`}},
		{"/layout", []string{`{"width":1e0}`, `{"height":` + maxInt + `,"width":1e0}`}, []string{`1`, `null`, `{"width":1.5}`, `{"width":9223372036854775808e0}`, `{"Width":1e0}`, `{"unknown":1e0}`}},
		{"/metadata", []string{`{}`, `{"key":"value"}`}, []string{`null`, `{"key":true}`}},
		{"/name", []string{`"name"`}, []string{`17`, `"\u0001"`}},
		{"/relations", []string{`null`, relations}, []string{`true`, `[{"kind":"any-kind","target":"bad"}]`, `[{"Kind":"any-kind","target":"` + string(id) + `"}]`, `[{"kind":"any-kind","target":"` + string(id) + `","unknown":true}]`}},
		{"/role", []string{`"text"`}, []string{`17`, `"bogus"`}},
		{"/states", []string{`null`, `["any-state"]`}, []string{`true`}},
		{"/style", []string{`{"role":"accent"}`}, []string{`true`, `{"Role":"accent"}`, `{"role":true}`, `{"unknown":"accent"}`}},
	}
	for _, tc := range cases {
		for _, raw := range tc.valid {
			for _, endpoint := range []string{"before", "after"} {
				t.Run(tc.path+"/valid/"+endpoint, func(t *testing.T) {
					checkExternalPatchDetailEndpoint(t, id, tc.path, raw, validPatchDetailCompanion(tc.path, raw, child), endpoint, true)
				})
			}
		}
		for _, raw := range tc.invalid {
			for _, endpoint := range []string{"before", "after"} {
				t.Run(tc.path+"/invalid/"+endpoint, func(t *testing.T) {
					checkExternalPatchDetailEndpoint(t, id, tc.path, raw, tc.valid[0], endpoint, false)
				})
			}
		}
	}
}

func validPatchDetailCompanion(path, raw string, child NodeID) string {
	switch path {
	case "/actions":
		if raw == `null` {
			return `[{"id":"other-action"}]`
		}
		return `null`
	case "/children":
		if raw == `[]` {
			return fmt.Sprintf("[%q]", child)
		}
		return `[]`
	case "/description":
		return `"changed description"`
	case "/flags":
		return `{"visible":true}`
	case "/layout":
		return `{"height":1e0}`
	case "/metadata":
		if raw == `{}` {
			return `{"key":"changed"}`
		}
		return `{}`
	case "/name":
		return `"changed name"`
	case "/relations":
		if raw == `null` {
			return fmt.Sprintf(`[{"kind":"other-kind","target":%q}]`, child)
		}
		return `null`
	case "/role":
		return `"region"`
	case "/states":
		if raw == `null` {
			return `["other-state"]`
		}
		return `null`
	case "/style":
		return `{"role":"changed"}`
	}
	return raw
}

func checkExternalPatchDetailEndpoint(t *testing.T, id NodeID, path, raw, companion, endpoint string, valid bool) {
	t.Helper()
	value, err := canonical.JSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	other, err := canonical.JSON([]byte(companion))
	if err != nil {
		t.Fatal(err)
	}
	before, after := other, other
	if endpoint == "before" {
		before = value
	} else {
		after = value
	}
	wire := fmt.Sprintf(`{"schemaVersion":"stave.semantic.patch-detail/v1","fromRevision":1,"toRevision":2,"changed":[{"nodeId":%q,"fields":[{"path":%q,"before":%s,"after":%s}]}]}`, id, path, before, after)
	var detail PatchDetail
	if err := json.Unmarshal([]byte(wire), &detail); err != nil {
		t.Fatal(err)
	}
	if err := detail.Validate(); (err == nil) != valid {
		t.Fatalf("Validate() error = %v, valid = %t", err, valid)
	}
}

func TestDiffDetailValidatesNonzeroLayoutEndpoints(t *testing.T) {
	id, err := NodeIDFor(NodeKey{"detail", "generated", "text", "layout", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	beforeNode, err := NewNode(NodeSpec{ID: id, Role: "text", Name: "layout", Layout: LayoutSpec{Width: 1}})
	if err != nil {
		t.Fatal(err)
	}
	afterNode, err := NewNode(NodeSpec{ID: id, Role: "text", Name: "layout", Layout: LayoutSpec{Height: 2, Width: 3}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := NewTree(1, beforeNode)
	after, _ := NewTree(2, afterNode)
	detail, err := DiffDetail(before, after, PatchDetailV1)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range detail.Changed[0].Fields {
		if field.Path == "/layout" && string(field.Before) == `{"width":1e0}` && string(field.After) == `{"height":2e0,"width":3e0}` {
			return
		}
	}
	t.Fatalf("generated layout endpoint = %#v", detail.Changed[0].Fields)
}

func TestPatchDetailRejectsUnchangedOrdinaryFields(t *testing.T) {
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "unchanged", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	plain := json.RawMessage(`{"hasValue":true,"text":"ordinary"}`)
	redacted := json.RawMessage(`{"hasValue":true,"redacted":true}`)
	cases := []struct {
		name   string
		fields []FieldChange
		valid  bool
	}{
		{"name", []FieldChange{{Path: "/name", Before: json.RawMessage(`"same"`), After: json.RawMessage(`"same"`)}}, false},
		{"flags", []FieldChange{{Path: "/flags", Before: json.RawMessage(`{"sensitive":false}`), After: json.RawMessage(`{"sensitive":false}`)}}, false},
		{"ordinary value", []FieldChange{{Path: "/value", Before: plain, After: plain}}, false},
		{"redacted value during hidden transition", []FieldChange{{Path: "/flags", Before: json.RawMessage(`{"sensitive":false}`), After: json.RawMessage(`{"sensitive":true}`)}, {Path: "/value", Before: redacted, After: redacted}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail := PatchDetail{SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2, Changed: []NodeChange{{NodeID: id, Fields: tc.fields}}}
			checkDecodedPatchDetail(t, detail, tc.valid)
		})
	}
}

func TestPatchDetailRejectsDuplicateOrEnclosingChildren(t *testing.T) {
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "parent", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := NodeIDFor(NodeKey{"detail", "external", "text", "child", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"duplicate": fmt.Sprintf("[%q,%q]", child, child),
		"enclosing": fmt.Sprintf("[%q]", id),
	} {
		for _, endpoint := range []string{"before", "after"} {
			t.Run(name+"/"+endpoint, func(t *testing.T) {
				checkExternalPatchDetailEndpoint(t, id, "/children", raw, `[]`, endpoint, false)
			})
		}
	}
}

func TestPatchDetailRejectsSemanticNoops(t *testing.T) {
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "semantic-noop", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	redacted := json.RawMessage(`{"hasValue":true,"redacted":true}`)
	cases := []struct {
		name   string
		fields []FieldChange
		valid  bool
	}{
		{"layout default", []FieldChange{{Path: "/layout", Before: json.RawMessage(`{}`), After: json.RawMessage(`{"width":0}`)}}, false},
		{"states default", []FieldChange{{Path: "/states", Before: json.RawMessage(`null`), After: json.RawMessage(`[]`)}}, false},
		{"flags default", []FieldChange{{Path: "/flags", Before: json.RawMessage(`{}`), After: json.RawMessage(`{"visible":false}`)}}, false},
		{"style default", []FieldChange{{Path: "/style", Before: json.RawMessage(`{}`), After: json.RawMessage(`{"role":""}`)}}, false},
		{"value default", []FieldChange{{Path: "/value", Before: json.RawMessage(`{}`), After: json.RawMessage(`{"hasValue":false}`)}}, false},
		{"action member default", []FieldChange{{Path: "/actions", Before: json.RawMessage(`[{"id":"action"}]`), After: json.RawMessage(`[{"default":false,"id":"action"}]`)}}, false},
		{"layout integer transition", []FieldChange{{Path: "/layout", Before: json.RawMessage(`{"width":1e0}`), After: json.RawMessage(`{"width":2e0}`)}}, true},
		{"redacted hidden transition", []FieldChange{{Path: "/flags", Before: json.RawMessage(`{"sensitive":false}`), After: json.RawMessage(`{"sensitive":true}`)}, {Path: "/value", Before: redacted, After: redacted}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkDecodedPatchDetail(t, PatchDetail{SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2, Changed: []NodeChange{{NodeID: id, Fields: tc.fields}}}, tc.valid)
		})
	}
}

func TestPatchDetailInteractiveNamesRequireContext(t *testing.T) {
	id, err := NodeIDFor(NodeKey{"detail", "external", "text", "interactive-name", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, before, after, roleBefore, roleAfter string
		valid                                      bool
	}{
		{"blank before", `""`, `"name"`, `"button"`, `"text"`, false},
		{"blank after", `"name"`, `""`, `"text"`, `"button"`, false},
		{"whitespace before", `" \t"`, `"name"`, `"button"`, `"text"`, false},
		{"whitespace after", `"name"`, `" \t"`, `"text"`, `"button"`, false},
		{"name alone", `""`, `"name"`, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := []FieldChange{{Path: "/name", Before: json.RawMessage(tc.before), After: json.RawMessage(tc.after)}}
			if tc.roleBefore != "" {
				fields = append(fields, FieldChange{Path: "/role", Before: json.RawMessage(tc.roleBefore), After: json.RawMessage(tc.roleAfter)})
			}
			checkDecodedPatchDetail(t, PatchDetail{SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2, Changed: []NodeChange{{NodeID: id, Fields: fields}}}, tc.valid)
		})
	}
}

func TestPatchDetailChildTargetsRespectPatchMembership(t *testing.T) {
	parent, err := NodeIDFor(NodeKey{"detail", "external", "text", "parent-target", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := NodeIDFor(NodeKey{"detail", "external", "text", "target", "slot"})
	if err != nil {
		t.Fatal(err)
	}
	children := fmt.Sprintf("[%q]", target)
	relations := fmt.Sprintf(`[{"kind":"any-kind","target":%q}]`, target)
	cases := []struct {
		name, path, before, after string
		added, removed            []NodeID
		valid                     bool
	}{
		{"added child before", "/children", children, `[]`, []NodeID{target}, nil, false},
		{"added child after", "/children", `[]`, children, []NodeID{target}, nil, true},
		{"removed child before", "/children", children, `[]`, nil, []NodeID{target}, true},
		{"removed child after", "/children", `[]`, children, nil, []NodeID{target}, false},
		{"added relation before", "/relations", relations, `null`, []NodeID{target}, nil, false},
		{"removed relation after", "/relations", `null`, relations, nil, []NodeID{target}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field := FieldChange{Path: tc.path, Before: json.RawMessage(tc.before), After: json.RawMessage(tc.after)}
			checkDecodedPatchDetail(t, PatchDetail{SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2, Added: tc.added, Removed: tc.removed, Changed: []NodeChange{{NodeID: parent, Fields: []FieldChange{field}}}}, tc.valid)
		})
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
		{"ordinary flags only", `{"sensitive":false}`, `{"sensitive":false,"visible":true}`, "", "", true},
		{"stable sensitive flags only", `{"sensitive":true}`, `{"sensitive":true,"visible":true}`, "", "", true},
		{"enable missing value", `{"sensitive":false}`, `{"sensitive":true}`, "", "", false},
		{"disable missing value", `{"sensitive":true}`, `{"sensitive":false}`, "", "", false},
		{"enable redacted", `{"sensitive":false}`, `{"sensitive":true}`, redacted, redacted, true},
		{"disable redacted", `{"sensitive":true}`, `{"sensitive":false}`, redacted, redacted, true},
		{"enable plaintext before", `{"sensitive":false}`, `{"sensitive":true}`, plaintext, redacted, false},
		{"enable plaintext after", `{"sensitive":false}`, `{"sensitive":true}`, redacted, plaintext, false},
		{"disable plaintext before", `{"sensitive":true}`, `{"sensitive":false}`, plaintext, redacted, false},
		{"disable plaintext after", `{"sensitive":true}`, `{"sensitive":false}`, redacted, plaintext, false},
		{"stable sensitive plaintext", `{"sensitive":true}`, `{"sensitive":true}`, plaintext, plaintext, false},
		{"ordinary plaintext", `{"sensitive":false}`, `{"sensitive":false,"visible":true}`, plaintext, `{"hasValue":true,"text":"other-endpoint-marker"}`, true},
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
		{"both ordinary", plain, `{"hasValue":true,"text":"other-endpoint-marker"}`, true},
	}
	for _, tc := range cases {
		for _, withFlags := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/flags=%t", tc.name, withFlags), func(t *testing.T) {
				fields := []FieldChange{}
				if withFlags {
					fields = append(fields, FieldChange{Path: "/flags", Before: json.RawMessage(`{"sensitive":false}`), After: json.RawMessage(`{"sensitive":false,"visible":true}`)})
				}
				fields = append(fields, FieldChange{Path: "/value", Before: json.RawMessage(tc.before), After: json.RawMessage(tc.after)})
				detail := PatchDetail{SchemaVersion: PatchDetailV1, FromRevision: 1, ToRevision: 2, Changed: []NodeChange{{NodeID: id, Fields: fields}}}
				checkDecodedPatchDetail(t, detail, tc.valid)
			})
		}
	}
}
