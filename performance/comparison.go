package performance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	ComparisonSchemaVersion = "stave.performance.comparison/v1"
	MaxReportBytes          = 16 << 20
)

// ComparisonPolicy makes the same-host noise allowance explicit. All values
// are increases: relative fractions for p95/allocation and percentage points
// for idle CPU.
type ComparisonPolicy struct {
	MeasurementTolerance float64
	AllocationTolerance  float64
	IdleCPUTolerance     float64
}

var DefaultComparisonPolicy = ComparisonPolicy{MeasurementTolerance: 0.10, AllocationTolerance: 0.10, IdleCPUTolerance: 0.10}

// Comparison is an opt-in envelope for comparing two existing performance
// report artifacts. Report itself intentionally remains unchanged.
type Comparison struct {
	SchemaVersion     string        `json:"schemaVersion"`
	Policy            string        `json:"policy"`
	BaselineRevision  string        `json:"baselineRevision,omitempty"`
	CandidateRevision string        `json:"candidateRevision,omitempty"`
	Deltas            []MetricDelta `json:"deltas"`
	Passed            bool          `json:"passed"`
}

// MetricDelta describes one deterministic comparison result. Values are raw
// report units: nanoseconds for p95, bytes for allocation, and percentage
// points for idle CPU.
type MetricDelta struct {
	Metric          string  `json:"metric"`
	Unit            string  `json:"unit"`
	Baseline        float64 `json:"baseline"`
	Candidate       float64 `json:"candidate"`
	Delta           float64 `json:"delta"`
	PercentDelta    float64 `json:"percentDelta,omitempty"`
	Tolerance       float64 `json:"tolerance"`
	ToleranceUnit   string  `json:"toleranceUnit"`
	WithinTolerance bool    `json:"withinTolerance"`
}

// MarshalJSON preserves the public float64 API while making percentDelta
// optional only for relative metrics with a zero baseline. A defined 0% delta
// remains visible, while undefined relative and absolute deltas omit the field.
func (delta MetricDelta) MarshalJSON() ([]byte, error) {
	type metricDeltaWire MetricDelta
	var percent *float64
	if delta.ToleranceUnit == "relative" && delta.Baseline != 0 {
		value := delta.PercentDelta
		percent = &value
	}
	return json.Marshal(struct {
		metricDeltaWire
		PercentDelta *float64 `json:"percentDelta,omitempty"`
	}{metricDeltaWire: metricDeltaWire(delta), PercentDelta: percent})
}

// DecodeReport strictly decodes an existing performance report without
// changing its wire schema. It rejects duplicate and unknown fields so a
// baseline cannot silently reinterpret saved measurements.
func DecodeReport(data []byte) (Report, error) {
	if len(data) > MaxReportBytes {
		return Report{}, fmt.Errorf("performance report exceeds %d-byte limit", MaxReportBytes)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || !utf8.Valid(data) || data[0] != '{' {
		return Report{}, errors.New("performance report must be a UTF-8 JSON object")
	}
	if err := validateReportJSONKeys(data); err != nil {
		return Report{}, fmt.Errorf("decode performance report: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("decode performance report: %w", err)
	}
	if err := ensureReportEOF(decoder); err != nil {
		return Report{}, err
	}
	if err := ValidateReport(report); err != nil {
		return Report{}, err
	}
	return report, nil
}

// Compare requires reports from the same declared environment and run policy.
// Source revisions may differ because that is the point of a baseline check.
// It never updates either input or creates a baseline.
func Compare(baseline, candidate Report) (Comparison, error) {
	return CompareWithPolicy(baseline, candidate, DefaultComparisonPolicy)
}

// CompareWithPolicy compares compatible reports with an explicit validated
// same-host noise policy. It never updates either input or creates a baseline.
func CompareWithPolicy(baseline, candidate Report, policy ComparisonPolicy) (Comparison, error) {
	if err := validatePolicy(policy); err != nil {
		return Comparison{}, err
	}
	if err := ValidateReport(baseline); err != nil {
		return Comparison{}, fmt.Errorf("invalid baseline report: %w", err)
	}
	if err := ValidateReport(candidate); err != nil {
		return Comparison{}, fmt.Errorf("invalid candidate report: %w", err)
	}
	candidateMeasurements := measurementIndex(candidate.Measurements)
	if err := compatibleEnvironment(baseline, candidate, candidateMeasurements); err != nil {
		return Comparison{}, err
	}
	deltas := make([]MetricDelta, 0, len(baseline.Measurements)+2)
	for _, metric := range baseline.Measurements {
		candidateMetric := candidateMeasurements[metric.Name]
		deltas = append(deltas, relativeDelta(metric.Name, "nanoseconds", float64(metric.P95), float64(candidateMetric.P95), policy.MeasurementTolerance))
	}
	deltas = append(deltas,
		relativeDelta("alloc_bytes", "bytes", float64(baseline.AllocBytes), float64(candidate.AllocBytes), policy.AllocationTolerance),
		absoluteDelta("idle_cpu.percent_one_core", "percentage_points", baseline.IdleCPU.Value, candidate.IdleCPU.Value, policy.IdleCPUTolerance),
	)
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Metric < deltas[j].Metric })
	passed := true
	for _, delta := range deltas {
		passed = passed && delta.WithinTolerance
	}
	return Comparison{SchemaVersion: ComparisonSchemaVersion, Policy: fmt.Sprintf("same host, Go version, OS/architecture, CPU count, fixture, capabilities, and run parameters; source revisions may differ; %.2f%% p95, %.2f%% allocation, and %.2f percentage-point idle-CPU tolerance", policy.MeasurementTolerance*100, policy.AllocationTolerance*100, policy.IdleCPUTolerance), BaselineRevision: baseline.Reproducibility.VCSRevision, CandidateRevision: candidate.Reproducibility.VCSRevision, Deltas: deltas, Passed: passed}, nil
}

func validatePolicy(policy ComparisonPolicy) error {
	for _, value := range []float64{policy.MeasurementTolerance, policy.AllocationTolerance, policy.IdleCPUTolerance} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return errors.New("comparison tolerances must be finite and non-negative")
		}
	}
	return nil
}

func ValidateReport(report Report) error {
	if report.Host == "" || report.Host == "unknown" || report.GoVersion == "" || report.GOOS == "" || report.GOARCH == "" || report.CPUs < 1 || report.Nodes < 1 || report.NodeShape == "" || report.Renderer == "" || report.Viewport.Width < 1 || report.Viewport.Height < 1 {
		return errors.New("performance report has incomplete environment metadata")
	}
	if report.Reproducibility.SampleCount < 1 || report.Reproducibility.GOMAXPROCS < 1 || len(report.Reproducibility.Invocation) == 0 {
		return errors.New("performance report has incomplete reproducibility metadata")
	}
	if report.AllocBytes > report.AllocLimit || !report.AllocWithin {
		return errors.New("performance report fails its allocation budget")
	}
	if err := validateIdleCPUReport(report.IdleCPU); err != nil {
		return err
	}
	if len(report.Measurements) == 0 {
		return errors.New("performance report has no measurements")
	}
	seen := map[string]struct{}{}
	for _, measurement := range report.Measurements {
		if measurement.Name == "" || measurement.Samples < 1 || measurement.P50 < 0 || measurement.P50 > measurement.P95 || measurement.P95 > measurement.P99 || measurement.P95 > measurement.Limit || measurement.Limit <= 0 || !measurement.AllWithinBudget {
			return fmt.Errorf("performance report fails absolute budget for %q", measurement.Name)
		}
		if _, duplicate := seen[measurement.Name]; duplicate {
			return fmt.Errorf("performance report has duplicate metric %q", measurement.Name)
		}
		seen[measurement.Name] = struct{}{}
	}
	return nil
}

func validateIdleCPUReport(metric RatioMeasurement) error {
	if metric.Name != "idle_cpu.percent_one_core" || metric.Window <= 0 || math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) || math.IsNaN(metric.Limit) || math.IsInf(metric.Limit, 0) || metric.Value < 0 || metric.Limit <= 0 || metric.Value >= metric.Limit || !metric.AllWithinBudget {
		return errors.New("performance report fails its idle CPU budget")
	}
	for _, attempt := range metric.Attempts {
		if math.IsNaN(attempt) || math.IsInf(attempt, 0) || attempt < 0 {
			return errors.New("performance report has invalid idle CPU attempts")
		}
	}
	return nil
}

func compatibleEnvironment(baseline, candidate Report, candidateMeasurements map[string]Measurement) error {
	if baseline.Host != candidate.Host || baseline.GoVersion != candidate.GoVersion || baseline.GOOS != candidate.GOOS || baseline.GOARCH != candidate.GOARCH || baseline.CPUs != candidate.CPUs || baseline.Nodes != candidate.Nodes || baseline.NodeShape != candidate.NodeShape || baseline.Renderer != candidate.Renderer || baseline.Viewport != candidate.Viewport || !reflect.DeepEqual(baseline.Capabilities, candidate.Capabilities) {
		return errors.New("performance reports use incompatible environment or fixture schema")
	}
	if baseline.Reproducibility.SampleCount != candidate.Reproducibility.SampleCount || baseline.Reproducibility.Strict != candidate.Reproducibility.Strict || baseline.Reproducibility.GOMAXPROCS != candidate.Reproducibility.GOMAXPROCS || !reflect.DeepEqual(comparableInvocation(baseline.Reproducibility.Invocation), comparableInvocation(candidate.Reproducibility.Invocation)) {
		return errors.New("performance reports use incompatible reproducibility parameters")
	}
	if baseline.AllocLimit != candidate.AllocLimit || baseline.IdleCPU.Limit != candidate.IdleCPU.Limit || baseline.IdleCPU.Name != candidate.IdleCPU.Name || len(baseline.IdleCPU.Attempts) != len(candidate.IdleCPU.Attempts) || len(baseline.Measurements) != len(candidate.Measurements) {
		return errors.New("performance reports use incompatible absolute budget schema")
	}
	for _, measurement := range baseline.Measurements {
		candidateMetric := candidateMeasurements[measurement.Name]
		if candidateMetric.Name == "" || candidateMetric.Limit != measurement.Limit || candidateMetric.Samples != measurement.Samples || len(candidateMetric.Attempts) != len(measurement.Attempts) {
			return fmt.Errorf("performance reports use incompatible metric schema for %q", measurement.Name)
		}
	}
	return nil
}

// comparableInvocation preserves measured run parameters while excluding
// launch and artifact locations. os.Args[0] can be an ephemeral go-build path,
// while -out names an artifact; neither changes a measurement run.
func comparableInvocation(invocation []string) []string {
	result := make([]string, 0, len(invocation))
	for i := 1; i < len(invocation); i++ {
		if invocation[i] == "-out" || invocation[i] == "--out" {
			i++
			continue
		}
		if strings.HasPrefix(invocation[i], "-out=") || strings.HasPrefix(invocation[i], "--out=") {
			continue
		}
		result = append(result, invocation[i])
	}
	return result
}

func measurementIndex(measurements []Measurement) map[string]Measurement {
	indexed := make(map[string]Measurement, len(measurements))
	for _, measurement := range measurements {
		indexed[measurement.Name] = measurement
	}
	return indexed
}

func relativeDelta(metric, unit string, baseline, candidate, tolerance float64) MetricDelta {
	delta := candidate - baseline
	percent := 0.0
	withinTolerance := candidate == 0
	if baseline > 0 {
		percent = delta / baseline
		withinTolerance = percent <= tolerance
	}
	return MetricDelta{Metric: metric, Unit: unit, Baseline: baseline, Candidate: candidate, Delta: delta, PercentDelta: percent, Tolerance: tolerance, ToleranceUnit: "relative", WithinTolerance: withinTolerance}
}

func absoluteDelta(metric, unit string, baseline, candidate, tolerance float64) MetricDelta {
	delta := candidate - baseline
	return MetricDelta{Metric: metric, Unit: unit, Baseline: baseline, Candidate: candidate, Delta: delta, Tolerance: tolerance, ToleranceUnit: "absolute", WithinTolerance: delta <= tolerance && !math.IsNaN(candidate) && !math.IsInf(candidate, 0)}
}

func validateReportJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateTypedJSONValue(decoder, 0, reflect.TypeOf(Report{})); err != nil {
		return err
	}
	return ensureReportEOF(decoder)
}

func validateJSONKeys(data []byte, depth int) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateTypedJSONValue(decoder, depth, nil); err != nil {
		return err
	}
	return ensureReportEOF(decoder)
}

func validateTypedJSONValue(decoder *json.Decoder, depth int, typ reflect.Type) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	if depth > 64 {
		return errors.New("performance report JSON nesting exceeds limit")
	}
	typ = indirectJSONType(typ)
	switch delim {
	case '{':
		return validateTypedJSONObject(decoder, depth, typ)
	case '[':
		return validateTypedJSONArray(decoder, depth, typ)
	default:
		return errors.New("invalid performance report JSON")
	}
}

func validateTypedJSONObject(decoder *json.Decoder, depth int, typ reflect.Type) error {
	fields := jsonStructFields(typ)
	seen := map[string]struct{}{}
	for decoder.More() {
		key, err := validateJSONObjectKey(decoder, seen)
		if err != nil {
			return err
		}
		fieldType, err := typedJSONField(fields, key)
		if err != nil {
			return err
		}
		if err := validateTypedJSONValue(decoder, depth+1, fieldType); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func validateTypedJSONArray(decoder *json.Decoder, depth int, typ reflect.Type) error {
	var element reflect.Type
	if typ != nil && (typ.Kind() == reflect.Array || typ.Kind() == reflect.Slice) {
		element = typ.Elem()
	}
	for decoder.More() {
		if err := validateTypedJSONValue(decoder, depth+1, element); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func typedJSONField(fields map[string]reflect.Type, key string) (reflect.Type, error) {
	if fields == nil {
		return nil, nil
	}
	if fieldType, exists := fields[key]; exists {
		return fieldType, nil
	}
	for name := range fields {
		if strings.EqualFold(key, name) {
			return nil, fmt.Errorf("noncanonical performance report key %q, want %q", key, name)
		}
	}
	return nil, nil
}

func indirectJSONType(typ reflect.Type) reflect.Type {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

// jsonStructFieldCache stores immutable field maps keyed by the framework type.
// Typed array entries reuse their schema without allocating a map per object.
var jsonStructFieldCache sync.Map // map[reflect.Type]map[string]reflect.Type

func jsonStructFields(typ reflect.Type) map[string]reflect.Type {
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil
	}
	if cached, ok := jsonStructFieldCache.Load(typ); ok {
		return cached.(map[string]reflect.Type)
	}
	fields := make(map[string]reflect.Type)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	actual, _ := jsonStructFieldCache.LoadOrStore(typ, fields)
	return actual.(map[string]reflect.Type)
}

func validateJSONObjectKey(decoder *json.Decoder, seen map[string]struct{}) (string, error) {
	keyToken, err := decoder.Token()
	if err != nil {
		return "", err
	}
	key, ok := keyToken.(string)
	if !ok {
		return "", errors.New("invalid performance report object key")
	}
	if _, exists := seen[key]; exists {
		return "", fmt.Errorf("duplicate performance report key %q", key)
	}
	seen[key] = struct{}{}
	return key, nil
}

func ensureReportEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}
