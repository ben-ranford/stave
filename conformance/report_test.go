package conformance

import (
	"strings"
	"testing"
)

func TestJSONReportCanonicalizesFailures(t *testing.T) {
	report, err := NewJSONReport([]Failure{
		{Path: "/b", Rule: "secret-redaction", Detail: "second"},
		{Path: "/a", Rule: "accessible-name", Detail: "first"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalJSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":"stave.conformance.report.v1","failures":[{"path":"/a","rule":"accessible-name","detail":"first","documentation":"docs/accessibility-agent-parity.md#parity-rules"},{"path":"/b","rule":"secret-redaction","detail":"second","documentation":"docs/accessibility-agent-parity.md#capability-policy"}]}`
	if string(data) != want {
		t.Fatalf("report = %s", data)
	}
	decoded, err := ParseJSONReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Failures[0].Path != "/a" || decoded.Failures[1].Path != "/b" {
		t.Fatalf("failures = %#v", decoded.Failures)
	}
}

func TestJSONReportRejectsUntrustedInput(t *testing.T) {
	valid := `{"schemaVersion":"stave.conformance.report.v1","failures":[]}`
	cases := map[string]string{
		"unknown field":           `{"schemaVersion":"stave.conformance.report.v1","failures":[],"extra":true}`,
		"duplicate field":         `{"schemaVersion":"stave.conformance.report.v1","schemaVersion":"stave.conformance.report.v1","failures":[]}`,
		"trailing value":          valid + ` {}`,
		"missing failures":        `{"schemaVersion":"stave.conformance.report.v1"}`,
		"missing detail":          `{"schemaVersion":"stave.conformance.report.v1","failures":[{"path":"p","rule":"unknown","documentation":""}]}`,
		"null failures":           `{"schemaVersion":"stave.conformance.report.v1","failures":null}`,
		"unsupported version":     `{"schemaVersion":"other","failures":[]}`,
		"wrong documentation":     `{"schemaVersion":"stave.conformance.report.v1","failures":[{"path":"p","rule":"accessible-name","detail":"d","documentation":"elsewhere"}]}`,
		"duplicate failure field": `{"schemaVersion":"stave.conformance.report.v1","failures":[{"path":"p","path":"q","rule":"unknown","detail":"d","documentation":""}]}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseJSONReport([]byte(data)); err == nil {
				t.Fatal("invalid report was accepted")
			}
		})
	}
	tooMany := `{"schemaVersion":"stave.conformance.report.v1","failures":[` + strings.Repeat(`{"path":"p","rule":"unknown","detail":"d","documentation":""},`, MaxReportFailures) + `{"path":"p","rule":"unknown","detail":"d","documentation":""}]}`
	if _, err := ParseJSONReport([]byte(tooMany)); err == nil {
		t.Fatal("oversized failure count was accepted")
	}
	if _, err := ParseJSONReport([]byte(strings.Repeat(" ", MaxReportBytes+1))); err == nil {
		t.Fatal("oversized byte input was accepted")
	}
}

func TestReadJSONReportBoundsInput(t *testing.T) {
	if _, err := ReadJSONReport(strings.NewReader(strings.Repeat(" ", MaxReportBytes+1))); err == nil {
		t.Fatal("oversized stream was accepted")
	}
}
