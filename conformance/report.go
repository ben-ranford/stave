package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// ReportSchemaVersion identifies the JSON report contract.
const ReportSchemaVersion = "stave.conformance.report.v1"

// Report limits bound untrusted report input before it is formatted by a CLI.
const (
	MaxReportBytes      = 64 * 1024
	MaxReportFailures   = 256
	maxReportFieldBytes = 4 * 1024
)

// JSONFailure is one stable, machine-readable conformance failure.
// Documentation is empty only when Stave has no matching rule guide.
type JSONFailure struct {
	Path          string `json:"path"`
	Rule          string `json:"rule"`
	Detail        string `json:"detail"`
	Documentation string `json:"documentation"`
}

// JSONReport is the versioned report format for CI and other report consumers.
type JSONReport struct {
	SchemaVersion string        `json:"schemaVersion"`
	Failures      []JSONFailure `json:"failures"`
}

// NewJSONReport converts conformance failures into the canonical report format.
func NewJSONReport(failures []Failure) (JSONReport, error) {
	report := JSONReport{SchemaVersion: ReportSchemaVersion, Failures: make([]JSONFailure, 0, len(failures))}
	for _, failure := range failures {
		report.Failures = append(report.Failures, JSONFailure{
			Path:          failure.Path,
			Rule:          failure.Rule,
			Detail:        failure.Detail,
			Documentation: RuleDocumentation(failure.Rule),
		})
	}
	if err := validateJSONReport(report); err != nil {
		return JSONReport{}, err
	}
	sortJSONFailures(report.Failures)
	return report, nil
}

// MarshalJSONReport returns the canonical JSON encoding of a report.
func MarshalJSONReport(report JSONReport) ([]byte, error) {
	if err := validateJSONReport(report); err != nil {
		return nil, err
	}
	report.Failures = append([]JSONFailure(nil), report.Failures...)
	sortJSONFailures(report.Failures)
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxReportBytes {
		return nil, fmt.Errorf("conformance report exceeds %d byte limit", MaxReportBytes)
	}
	return data, nil
}

// ParseJSONReport validates and decodes a report received from an untrusted
// caller. It rejects unknown or duplicate fields, trailing values, unsupported
// versions and reports beyond the documented byte and failure limits.
func ParseJSONReport(data []byte) (JSONReport, error) {
	if len(data) > MaxReportBytes {
		return JSONReport{}, fmt.Errorf("conformance report exceeds %d byte limit", MaxReportBytes)
	}
	if err := rejectDuplicateJSONFields(data); err != nil {
		return JSONReport{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire struct {
		SchemaVersion *string          `json:"schemaVersion"`
		Failures      *json.RawMessage `json:"failures"`
	}
	if err := decoder.Decode(&wire); err != nil {
		return JSONReport{}, fmt.Errorf("decode conformance report: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return JSONReport{}, fmt.Errorf("trailing conformance report JSON")
	}
	if wire.SchemaVersion == nil || wire.Failures == nil {
		return JSONReport{}, fmt.Errorf("conformance report requires schemaVersion and failures")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(*wire.Failures), []byte("[")) {
		return JSONReport{}, fmt.Errorf("conformance report failures must be an array")
	}
	var failures []json.RawMessage
	if err := json.Unmarshal(*wire.Failures, &failures); err != nil {
		return JSONReport{}, fmt.Errorf("decode conformance report failures: %w", err)
	}
	report := JSONReport{SchemaVersion: *wire.SchemaVersion, Failures: make([]JSONFailure, 0, len(failures))}
	for i, rawFailure := range failures {
		failure, err := decodeJSONFailure(rawFailure)
		if err != nil {
			return JSONReport{}, fmt.Errorf("decode conformance report failure %d: %w", i, err)
		}
		report.Failures = append(report.Failures, failure)
	}
	if err := validateJSONReport(report); err != nil {
		return JSONReport{}, err
	}
	return report, nil
}

func rejectDuplicateJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := readJSONValue(decoder); err != nil {
		return fmt.Errorf("decode conformance report: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing conformance report JSON")
	}
	return nil
}

func readJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = true
			if err := readJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := readJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func decodeJSONFailure(data json.RawMessage) (JSONFailure, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire struct {
		Path          *string `json:"path"`
		Rule          *string `json:"rule"`
		Detail        *string `json:"detail"`
		Documentation *string `json:"documentation"`
	}
	if err := decoder.Decode(&wire); err != nil {
		return JSONFailure{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return JSONFailure{}, fmt.Errorf("trailing failure JSON")
	}
	if wire.Path == nil || wire.Rule == nil || wire.Detail == nil || wire.Documentation == nil {
		return JSONFailure{}, fmt.Errorf("path, rule, detail, and documentation are required")
	}
	return JSONFailure{Path: *wire.Path, Rule: *wire.Rule, Detail: *wire.Detail, Documentation: *wire.Documentation}, nil
}

// ReadJSONReport reads at most one bounded report from r before decoding it.
func ReadJSONReport(r io.Reader) (JSONReport, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxReportBytes+1))
	if err != nil {
		return JSONReport{}, fmt.Errorf("read conformance report: %w", err)
	}
	return ParseJSONReport(data)
}

// RuleDocumentation returns the stable guide anchor for a built-in rule.
func RuleDocumentation(rule string) string {
	switch rule {
	case "unique-node-id":
		return "docs/accessibility-agent-parity.md#shared-node-contract"
	case "action-binding", "action-authority", "action-registry", "action-registry-authority", "keymap-action-authority", "keymap-binding-authority":
		return "docs/accessibility-agent-parity.md#action-policy"
	case "secret-redaction":
		return "docs/accessibility-agent-parity.md#capability-policy"
	case "render":
		return "docs/client-adoption.md#validate-an-integration"
	case "client-fixtures", "fixture-name", "fixture-modes":
		return "docs/client-adoption.md#validate-an-integration"
	case "accessible-name", "focus-state", "action-parity", "table-shape", "table-header", "table-sort", "table-row", "stable-row-key", "table-cell", "table-cell-header", "field-error-relation", "form-invalid-count", "progress-value", "dialog-close", "modal-dismiss", "modal-focus-restore", "master-detail-actions", "master-detail-focus-restore", "record-fallback":
		return "docs/accessibility-agent-parity.md#parity-rules"
	default:
		return ""
	}
}

func validateJSONReport(report JSONReport) error {
	if report.SchemaVersion != ReportSchemaVersion {
		return fmt.Errorf("unexpected conformance report schema version %q", report.SchemaVersion)
	}
	if len(report.Failures) > MaxReportFailures {
		return fmt.Errorf("conformance report exceeds %d failure limit", MaxReportFailures)
	}
	for i, failure := range report.Failures {
		if err := validateJSONFailure(failure); err != nil {
			return fmt.Errorf("conformance report failure %d: %w", i, err)
		}
	}
	return nil
}

func validateJSONFailure(failure JSONFailure) error {
	for name, value := range map[string]string{"path": failure.Path, "rule": failure.Rule, "detail": failure.Detail} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
		if !utf8.ValidString(value) || len(value) > maxReportFieldBytes {
			return fmt.Errorf("%s exceeds %d byte limit or is not valid UTF-8", name, maxReportFieldBytes)
		}
	}
	if failure.Documentation != RuleDocumentation(failure.Rule) {
		return fmt.Errorf("documentation does not match rule %q", failure.Rule)
	}
	if len(failure.Documentation) > maxReportFieldBytes {
		return fmt.Errorf("documentation exceeds %d byte limit", maxReportFieldBytes)
	}
	return nil
}

func sortJSONFailures(failures []JSONFailure) {
	sort.Slice(failures, func(i, j int) bool {
		left, right := failures[i], failures[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Detail < right.Detail
	})
}
