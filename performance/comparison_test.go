package performance

import (
	"bytes"
	"encoding/json"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/layout"
)

func TestCompareProducesDeterministicDeltasAndAllowsSourceRevisionChange(t *testing.T) {
	baseline := comparisonFixture()
	candidate := comparisonFixture()
	candidate.Reproducibility.VCSRevision = "candidate"
	baseline.Reproducibility.Invocation = append(baseline.Reproducibility.Invocation, "-out", "baseline.json")
	candidate.Reproducibility.Invocation = append(candidate.Reproducibility.Invocation, "-out", "candidate.json")
	candidate.Measurements[0].P95 = 105 * time.Nanosecond
	candidate.Measurements[0].P99 = 105 * time.Nanosecond
	comparison, err := Compare(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.Passed || comparison.SchemaVersion != ComparisonSchemaVersion || len(comparison.Deltas) != 3 {
		t.Fatalf("comparison=%+v", comparison)
	}
	if comparison.BaselineRevision != "baseline" || comparison.CandidateRevision != "candidate" {
		t.Fatalf("revisions=%q/%q", comparison.BaselineRevision, comparison.CandidateRevision)
	}
}

func TestCompareIgnoresNonMeasurementInvocationPaths(t *testing.T) {
	baseline := comparisonFixture()
	candidate := comparisonFixture()
	baseline.Reproducibility.Invocation = []string{"/private/var/folders/a/go-build123/b001/exe/stave-performance", "-strict", "--out=baseline.json"}
	candidate.Reproducibility.Invocation = []string{"/private/var/folders/b/go-build456/b001/exe/stave-performance", "-strict", "--out", "candidate.json"}
	if _, err := Compare(baseline, candidate); err != nil {
		t.Fatalf("Compare() rejected equivalent invocation paths: %v", err)
	}
}

func TestCompareRejectsMeasuredInvocationArgumentChange(t *testing.T) {
	baseline := comparisonFixture()
	candidate := comparisonFixture()
	candidate.Reproducibility.Invocation = []string{"/tmp/go-build/exe/stave-performance", "-strict=false"}
	if _, err := Compare(baseline, candidate); err == nil {
		t.Fatal("Compare() accepted different invocation arguments")
	}
}

func TestCompareWithPolicyRejectsInvalidTolerancesAndReports(t *testing.T) {
	baseline := comparisonFixture()
	for _, policy := range []ComparisonPolicy{{MeasurementTolerance: -1}, {MeasurementTolerance: math.NaN()}, {MeasurementTolerance: math.Inf(1)}} {
		if _, err := CompareWithPolicy(baseline, baseline, policy); err == nil {
			t.Fatal("CompareWithPolicy() accepted invalid tolerance")
		}
	}
	for _, mutate := range []func(*Report){
		func(r *Report) { r.IdleCPU.Value = math.NaN() },
		func(r *Report) { r.IdleCPU.Value = -1 },
		func(r *Report) { r.IdleCPU.Window = 0 },
		func(r *Report) { r.Host = "unknown" },
		func(r *Report) { r.IdleCPU.Attempts = []float64{math.NaN()} },
		func(r *Report) { r.IdleCPU.Attempts = []float64{math.Inf(1)} },
		func(r *Report) { r.IdleCPU.Attempts = []float64{-1} },
		func(r *Report) { r.Measurements[0].P50 = r.Measurements[0].P95 + time.Nanosecond },
		func(r *Report) { r.Measurements[0].P95 = -time.Nanosecond },
		func(r *Report) { r.Measurements[0].P99 = r.Measurements[0].P95 - time.Nanosecond },
	} {
		invalid := comparisonFixture()
		mutate(&invalid)
		if _, err := Compare(baseline, invalid); err == nil {
			t.Fatal("Compare() accepted invalid direct API report")
		}
	}
}

func TestCompareRejectsRegressionEnvironmentAndBudgetChanges(t *testing.T) {
	baseline := comparisonFixture()
	cases := []struct {
		name   string
		mutate func(*Report)
		wantOK bool
	}{
		{"regression", func(r *Report) {
			r.Measurements[0].P95, r.Measurements[0].P99 = 120*time.Nanosecond, 120*time.Nanosecond
		}, false},
		{"host", func(r *Report) { r.Host = "other" }, true},
		{"run parameters", func(r *Report) { r.Reproducibility.SampleCount++ }, true},
		{"absolute budget", func(r *Report) { r.Measurements[0].Limit++ }, true},
		{"failed absolute budget", func(r *Report) { r.Measurements[0].AllWithinBudget = false }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := comparisonFixture()
			tc.mutate(&candidate)
			comparison, err := Compare(baseline, candidate)
			if tc.wantOK {
				if err == nil {
					t.Fatal("Compare() accepted incompatible reports")
				}
				return
			}
			if err != nil || comparison.Passed {
				t.Fatalf("Compare() comparison=%+v err=%v", comparison, err)
			}
		})
	}
}

func TestCompareRequiresEqualAttemptCounts(t *testing.T) {
	baseline, candidate := comparisonFixture(), comparisonFixture()
	baseline.Measurements[0].Attempts = []Attempt{
		{P50: time.Nanosecond, P95: 2 * time.Nanosecond, P99: 3 * time.Nanosecond},
		{P50: 4 * time.Nanosecond, P95: 5 * time.Nanosecond, P99: 6 * time.Nanosecond},
	}
	candidate.Measurements[0].Attempts = []Attempt{
		{P50: 7 * time.Nanosecond, P95: 8 * time.Nanosecond, P99: 9 * time.Nanosecond},
		{P50: 10 * time.Nanosecond, P95: 11 * time.Nanosecond, P99: 12 * time.Nanosecond},
	}
	baseline.IdleCPU.Attempts = []float64{0.01, 0.02}
	candidate.IdleCPU.Attempts = []float64{0.03, 0.04}
	baseline.Measurements[0].P50, baseline.Measurements[0].P95, baseline.Measurements[0].P99 = time.Nanosecond, 2*time.Nanosecond, 3*time.Nanosecond
	candidate.Measurements[0].P50, candidate.Measurements[0].P95, candidate.Measurements[0].P99 = 7*time.Nanosecond, 8*time.Nanosecond, 9*time.Nanosecond
	baseline.IdleCPU.Value = 0.01
	candidate.IdleCPU.Value = 0.03
	if _, err := Compare(baseline, candidate); err != nil {
		t.Fatalf("Compare() rejected equal attempt counts with different timings: %v", err)
	}

	for _, mutate := range []struct {
		name  string
		apply func(*Report)
	}{
		{"measurement", func(report *Report) {
			report.Measurements[0].Attempts = append(report.Measurements[0].Attempts, Attempt{P50: 12 * time.Nanosecond, P95: 13 * time.Nanosecond, P99: 14 * time.Nanosecond})
		}},
		{"idle CPU", func(report *Report) {
			report.IdleCPU.Attempts = append(report.IdleCPU.Attempts, 0.05)
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			mismatched := candidate
			mismatched.Measurements = append([]Measurement(nil), candidate.Measurements...)
			mismatched.Measurements[0].Attempts = append([]Attempt(nil), candidate.Measurements[0].Attempts...)
			mismatched.IdleCPU.Attempts = append([]float64(nil), candidate.IdleCPU.Attempts...)
			mutate.apply(&mismatched)
			if _, err := Compare(baseline, mismatched); err == nil {
				t.Fatal("Compare() accepted mismatched attempt counts")
			}
		})
	}
}

func TestCompareRejectsDifferentDeclaredInvariantsAndDisposition(t *testing.T) {
	baseline := comparisonFixture()
	for _, mutate := range []struct {
		name  string
		apply func(*Report)
	}{
		{"invariants", func(report *Report) { report.Invariants = []string{"fixture", "changed"} }},
		{"measurement disposition", func(report *Report) { report.Measurements[0].Disposition = "median retained attempt" }},
		{"idle CPU disposition", func(report *Report) { report.IdleCPU.Disposition = "mean retained window" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			candidate := comparisonFixture()
			mutate.apply(&candidate)
			if _, err := Compare(baseline, candidate); err == nil {
				t.Fatal("Compare() accepted incompatible declared collection contract")
			}
		})
	}
}

func TestValidateReportRetainedAttemptsMatchCollectorSelection(t *testing.T) {
	valid := comparisonFixture()
	valid.Measurements[0].Attempts = []Attempt{
		{P50: 0, P95: 0, P99: 0},
		{P50: 4 * time.Nanosecond, P95: 5 * time.Nanosecond, P99: 6 * time.Nanosecond},
		{P50: 3 * time.Nanosecond, P95: 4 * time.Nanosecond, P99: 7 * time.Nanosecond},
	}
	// The collector's zero-P95 sentinel replaces the initial zero attempt, then
	// picks the lowest following P95.
	valid.Measurements[0].P50, valid.Measurements[0].P95, valid.Measurements[0].P99 = 3*time.Nanosecond, 4*time.Nanosecond, 7*time.Nanosecond
	valid.IdleCPU.Attempts = []float64{0.4, 0.2, 0.3}
	valid.IdleCPU.Value = 0.2
	if err := ValidateReport(valid); err != nil {
		t.Fatalf("ValidateReport() rejected collector-selected attempts: %v", err)
	}

	tie := comparisonFixture()
	tie.Measurements[0].Attempts = []Attempt{
		{P50: time.Nanosecond, P95: 2 * time.Nanosecond, P99: 3 * time.Nanosecond},
		{P50: 0, P95: 2 * time.Nanosecond, P99: 4 * time.Nanosecond},
	}
	tie.Measurements[0].P50, tie.Measurements[0].P95, tie.Measurements[0].P99 = time.Nanosecond, 2*time.Nanosecond, 3*time.Nanosecond
	if err := ValidateReport(tie); err != nil {
		t.Fatalf("ValidateReport() rejected first retained tie: %v", err)
	}

	for _, mutate := range []struct {
		name  string
		apply func(*Report)
	}{
		{"wrong selected aggregate", func(report *Report) { report.Measurements[0].P95 = 5 * time.Nanosecond }},
		{"unordered attempt", func(report *Report) {
			report.Measurements[0].Attempts[0] = Attempt{P50: 3 * time.Nanosecond, P95: 2 * time.Nanosecond, P99: 4 * time.Nanosecond}
		}},
		{"idle CPU not minimum", func(report *Report) { report.IdleCPU.Value = 0.3 }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			invalid := valid
			invalid.Measurements = append([]Measurement(nil), valid.Measurements...)
			invalid.Measurements[0].Attempts = append([]Attempt(nil), valid.Measurements[0].Attempts...)
			invalid.IdleCPU.Attempts = append([]float64(nil), valid.IdleCPU.Attempts...)
			mutate.apply(&invalid)
			if err := ValidateReport(invalid); err == nil {
				t.Fatal("ValidateReport() accepted inconsistent retained attempts")
			}
		})
	}
}

func TestComparisonJSONDistinguishesDefinedAndUndefinedZeroPercentDelta(t *testing.T) {
	baseline, candidate := comparisonFixture(), comparisonFixture()
	comparison, err := Compare(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if percent := percentDeltaForMetric(t, comparison, "alloc_bytes"); percent == nil || *percent != 0 {
		t.Fatalf("defined zero percent delta = %v, want 0", percent)
	}
	if percentDeltaFieldCount(t, comparison, "alloc_bytes") != 1 {
		t.Fatal("defined percent delta was emitted more than once")
	}

	baseline.AllocBytes, candidate.AllocBytes = 0, 0
	comparison, err = Compare(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if percentDeltaForMetric(t, comparison, "alloc_bytes") != nil {
		t.Fatal("undefined zero-baseline percent delta was emitted")
	}
}

func percentDeltaForMetric(t *testing.T, comparison Comparison, metric string) *float64 {
	t.Helper()
	data, err := json.Marshal(comparison)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Deltas []struct {
			Metric       string   `json:"metric"`
			PercentDelta *float64 `json:"percentDelta"`
		} `json:"deltas"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for _, delta := range wire.Deltas {
		if delta.Metric == metric {
			return delta.PercentDelta
		}
	}
	t.Fatalf("comparison omitted metric %q", metric)
	return nil
}

func percentDeltaFieldCount(t *testing.T, comparison Comparison, metric string) int {
	t.Helper()
	data, err := json.Marshal(comparison)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Deltas []json.RawMessage `json:"deltas"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for _, delta := range wire.Deltas {
		var header struct {
			Metric string `json:"metric"`
		}
		if err := json.Unmarshal(delta, &header); err != nil {
			t.Fatal(err)
		}
		if header.Metric == metric {
			return bytes.Count(delta, []byte(`"percentDelta"`))
		}
	}
	t.Fatalf("comparison omitted metric %q", metric)
	return 0
}

func TestDecodeReportRejectsDuplicateAndUnknownFields(t *testing.T) {
	report := comparisonFixture()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReport(data); err != nil {
		t.Fatalf("DecodeReport() error=%v", err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte(nil), data[:len(data)-1]...), []byte(`,"host":"other"}`)...),
		append(append([]byte(nil), data[:len(data)-1]...), []byte(`,"unexpected":true}`)...),
		append(data, []byte(` {}`)...),
	} {
		if _, err := DecodeReport(invalid); err == nil {
			t.Fatal("DecodeReport() accepted hostile artifact")
		}
	}
}

func TestDecodeReportRejectsCaseVariantTypedKeys(t *testing.T) {
	data, err := json.Marshal(comparisonFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []struct {
		old, new []byte
	}{
		{[]byte(`"host":"host"`), []byte(`"host":"host","Host":"other"`)},
		{[]byte(`"sampleCount":101`), []byte(`"sampleCount":101,"SampleCount":101`)},
	} {
		mutated := bytes.Replace(data, replacement.old, replacement.new, 1)
		if bytes.Equal(mutated, data) {
			t.Fatalf("fixture did not add case-variant key %s", replacement.new)
		}
		if _, err := DecodeReport(mutated); err == nil {
			t.Fatalf("DecodeReport() accepted case-variant typed key %s", replacement.new)
		}
	}
}

func comparisonFixture() Report {
	return Report{
		Host: "host", GoVersion: "go1.22.0", GOOS: "linux", GOARCH: "amd64", CPUs: 8,
		Nodes: 10000, NodeShape: "balanced-binary", Renderer: "stave.render/v1", Viewport: layout.Size{Width: 120, Height: 40}, Capabilities: capability.Manifest{Width: 120, Height: 40},
		Reproducibility: Reproducibility{Invocation: []string{"stave-performance", "-strict"}, SampleCount: 101, Strict: true, GOMAXPROCS: 1, VCSRevision: "baseline"},
		AllocBytes:      100, AllocLimit: 200, AllocWithin: true,
		IdleCPU:      RatioMeasurement{Name: "idle_cpu.percent_one_core", Window: time.Second, Value: 0.20, Limit: 1, AllWithinBudget: true},
		Measurements: []Measurement{{Name: "render.p95", Samples: 101, P95: 100 * time.Nanosecond, P99: 100 * time.Nanosecond, Limit: time.Microsecond, AllWithinBudget: true}},
	}
}

func TestComparePreservesP95AbsoluteBudget(t *testing.T) {
	report := comparisonFixture()
	report.Measurements[0].P99 = report.Measurements[0].Limit + time.Nanosecond
	report.IdleCPU.Attempts = []float64{report.IdleCPU.Limit + 1, report.IdleCPU.Value}
	if _, err := Compare(report, report); err != nil {
		t.Fatalf("valid p95/retry report rejected: %v", err)
	}
}

func TestCompareAllowsBuildProvenanceAndObservedIdleWindowChanges(t *testing.T) {
	baseline, candidate := comparisonFixture(), comparisonFixture()
	candidate.Reproducibility.VCSModified = !baseline.Reproducibility.VCSModified
	candidate.IdleCPU.Window += time.Nanosecond
	if _, err := Compare(baseline, candidate); err != nil {
		t.Fatalf("source provenance or observed elapsed window rejected: %v", err)
	}
}

func TestReportJSONValidationDoesNotCopyEnclosingSubtrees(t *testing.T) {
	data := []byte(`{"unknown":` + strings.Repeat("[", 48) + `"` + strings.Repeat("x", 1<<20) + `"` + strings.Repeat("]", 48) + `}`)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := validateJSONKeys(data, 0); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	// Allow decoder buffering and token copies, but not one full value copy per ancestor.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > uint64(12*len(data)) {
		t.Fatalf("nested JSON validation allocated %d bytes for %d input bytes", allocated, len(data))
	}
}

func TestReportJSONValidationRetainsNestedContracts(t *testing.T) {
	for _, data := range []string{`{"a":[{"x":1,"x":2}]}`, `{"a":[1,]}`, `{"a":[]} {}`, strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66)} {
		if err := validateJSONKeys([]byte(data), 0); err == nil {
			t.Fatalf("invalid nested JSON accepted: %.80s", data)
		}
	}
	if err := validateJSONKeys([]byte(strings.Repeat("[", 65)+`0`+strings.Repeat("]", 65)), 0); err != nil {
		t.Fatalf("depth boundary rejected: %v", err)
	}
}

func TestReportJSONValidationCachesTypedFieldMaps(t *testing.T) {
	data := []byte(`{"measurements":[` + strings.Repeat(`{},`, 9_999) + `{}]}`)
	if err := validateReportJSONKeys(data); err != nil {
		t.Fatal(err)
	}
	measure := func(validate func([]byte) error) uint64 {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if err := validate(data); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	generic := measure(func(data []byte) error { return validateJSONKeys(data, 0) })
	typed := measure(validateReportJSONKeys)
	if typed > generic+uint64(4*len(data)) {
		t.Fatalf("typed field schema validation allocated %d bytes versus generic %d for %d-byte input", typed, generic, len(data))
	}
}
