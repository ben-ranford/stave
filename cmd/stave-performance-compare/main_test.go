package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/performance"
)

func TestRunDistinguishesMatchRegressionAndInvalid(t *testing.T) {
	dir := t.TempDir()
	baseline, candidate := filepath.Join(dir, "baseline.json"), filepath.Join(dir, "candidate.json")
	report := fixtureReport()
	writeFixture(t, baseline, report)
	writeFixture(t, candidate, report)
	if code := run([]string{"-baseline", baseline, "-candidate", candidate}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("match code=%d", code)
	}
	report.Measurements[0].P95 = 120 * time.Nanosecond
	report.Measurements[0].P99 = 120 * time.Nanosecond
	writeFixture(t, candidate, report)
	if code := run([]string{"-baseline", baseline, "-candidate", candidate}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitRegression {
		t.Fatalf("regression code=%d", code)
	}
	if err := os.WriteFile(candidate, []byte(`{"host":"secret"`), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-baseline", baseline, "-candidate", candidate}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitInvalid {
		t.Fatalf("invalid code=%d", code)
	}
}

func writeFixture(t *testing.T, path string, report performance.Report) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureReport() performance.Report {
	return performance.Report{Host: "host", GoVersion: "go1.22.0", GOOS: "linux", GOARCH: "amd64", CPUs: 8, Nodes: 10000, NodeShape: "balanced", Renderer: "stave.render/v1", Viewport: layout.Size{Width: 120, Height: 40}, Capabilities: capability.Manifest{Width: 120, Height: 40}, Reproducibility: performance.Reproducibility{Invocation: []string{"stave-performance", "-strict"}, SampleCount: 101, Strict: true, GOMAXPROCS: 1}, AllocBytes: 100, AllocLimit: 200, AllocWithin: true, IdleCPU: performance.RatioMeasurement{Name: "idle_cpu.percent_one_core", Window: time.Second, Value: .2, Limit: 1, AllWithinBudget: true}, Measurements: []performance.Measurement{{Name: "render.p95", Samples: 101, P95: 100 * time.Nanosecond, P99: 100 * time.Nanosecond, Limit: time.Microsecond, AllWithinBudget: true}}}
}
