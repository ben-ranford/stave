package performance

import (
	"encoding/json"
	"math"
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
		func(r *Report) { r.Measurements[0].P95 = -time.Nanosecond },
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
		{"regression", func(r *Report) { r.Measurements[0].P95 = 120 * time.Nanosecond }, false},
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

func comparisonFixture() Report {
	return Report{
		Host: "host", GoVersion: "go1.22.0", GOOS: "linux", GOARCH: "amd64", CPUs: 8,
		Nodes: 10000, NodeShape: "balanced-binary", Renderer: "stave.render/v1", Viewport: layout.Size{Width: 120, Height: 40}, Capabilities: capability.Manifest{Width: 120, Height: 40},
		Reproducibility: Reproducibility{Invocation: []string{"stave-performance", "-strict"}, SampleCount: 101, Strict: true, GOMAXPROCS: 1, VCSRevision: "baseline"},
		AllocBytes:      100, AllocLimit: 200, AllocWithin: true,
		IdleCPU:      RatioMeasurement{Name: "idle_cpu.percent_one_core", Value: 0.20, Limit: 1, AllWithinBudget: true},
		Measurements: []Measurement{{Name: "render.p95", Samples: 101, P95: 100 * time.Nanosecond, Limit: time.Microsecond, AllWithinBudget: true}},
	}
}
