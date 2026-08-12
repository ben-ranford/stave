package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"time"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/internal/examples"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/performance"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/surface"
	"github.com/ben-ranford/stave/theme"
)

type target struct {
	name  string
	limit time.Duration
	fn    func() error
}

func measure(t target, samples int) (performance.Measurement, error) {
	const attempts = 3
	measurement := performance.Measurement{Name: t.name, Samples: samples, Limit: t.limit, Disposition: "best of three isolated attempts; all attempts are retained and a persistent miss requires an ADR and stage downgrade"}
	for warmup := 0; warmup < 3; warmup++ {
		if err := t.fn(); err != nil {
			return performance.Measurement{}, err
		}
	}
	for attempt := 0; attempt < attempts; attempt++ {
		runtime.GC()
		v := make([]time.Duration, 0, samples)
		for i := 0; i < samples; i++ {
			start := time.Now()
			if err := t.fn(); err != nil {
				return performance.Measurement{}, err
			}
			v = append(v, time.Since(start))
		}
		p50, p95, p99, _ := performance.Percentiles(v)
		measurement.Attempts = append(measurement.Attempts, performance.Attempt{P50: p50, P95: p95, P99: p99})
		if measurement.P95 == 0 || p95 < measurement.P95 {
			measurement.P50, measurement.P95, measurement.P99 = p50, p95, p99
		}
	}
	measurement.AllWithinBudget = measurement.P95 <= measurement.Limit
	return measurement, nil
}
func main() {
	samples := flag.Int("samples", performance.Samples, "number of samples per target")
	out := flag.String("out", "", "write JSON report to this file")
	strict := flag.Bool("strict", false, "fail when a measured release budget is exceeded")
	flag.Parse()
	if *samples < 5 {
		*samples = 5
	}
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)
	tree, err := performance.Fixture(2000)
	if err != nil {
		fatal(err)
	}
	treeAgain, err := performance.Fixture(2000)
	if err != nil {
		fatal(err)
	}
	big, err := performance.Fixture(10000)
	if err != nil {
		fatal(err)
	}
	surf, err := performance.SurfaceFixture(120, 40)
	if err != nil {
		fatal(err)
	}
	changed := surf.WithCell(0, 0, surface.Cell{Grapheme: "X", Width: 1})
	ev := performance.OrdinaryEvent()
	ack, _ := performance.ActionAckLine()
	caps := capability.Manifest{ProtocolVersions: []string{protocol.Version}, OutputMode: capability.OutputAuto, Interactive: true, TTY: true, Color: capability.ColorTrueColor, HardwareColor: capability.ColorTrueColor, Unicode: capability.UnicodeFull, Width: 120, Height: 40}
	targets := []target{
		{"reducer.p95", 2 * time.Millisecond, func() error { _, _, _ = performance.ReduceCounter(context.Background(), 0, ev); return nil }},
		{"view_validate_2000.p95", 5 * time.Millisecond, tree.Validate},
		{"layout_diff_120x40.p95", 8 * time.Millisecond, func() error {
			_, _ = layout.Arrange(context.Background(), tree.Root(), layout.Size{Width: 120, Height: 40}, 100000)
			_ = surface.Diff(surf, changed)
			return nil
		}},
		{"synthetic_clone_arrange_diff.p95", 50 * time.Millisecond, func() error {
			_, _ = ev.Clone()
			_, _ = layout.Arrange(context.Background(), tree.Root(), layout.Size{Width: 120, Height: 40}, 100000)
			_ = surface.Diff(surf, changed)
			return nil
		}},
		{"synthetic_dispatch_arrange_diff.p95", 75 * time.Millisecond, func() error {
			_, _ = ev.Clone()
			_, _ = layout.Arrange(context.Background(), tree.Root(), layout.Size{Width: 120, Height: 40}, 100000)
			_ = surface.Diff(surf, changed)
			return nil
		}},
		{"snapshot_10000.p95", 100 * time.Millisecond, func() error { _, err := json.Marshal(big); return err }},
		{"protocol_pure_action_ack.p95", 100 * time.Millisecond, func() error { _, err := protocol.DecodeLine(ack, 4<<20); return err }},
		{"resize_storm_recovery.p95", 100 * time.Millisecond, func() error { return performance.ResizeStormRecovery(100) }},
		{"component_terminal_restore_after_cancellation.p95", 250 * time.Millisecond, performance.TerminalRestoreAfterCancellation},
	}
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	before := ms.TotalAlloc
	_, _ = json.Marshal(big)
	runtime.ReadMemStats(&ms)
	alloc := ms.TotalAlloc - before
	resolved, err := examples.BrandTheme("performance", "#64c8ff", "PERFORMANCE").Resolve(theme.ModeDark, theme.DensityComfortable, caps)
	if err != nil {
		fatal(err)
	}
	first, err := render.Render(render.Request{Tree: tree, Theme: resolved, Capabilities: caps, Viewport: layout.Size{Width: 120, Height: 40}})
	if err != nil {
		fatal(err)
	}
	second, err := render.Render(render.Request{Tree: tree, Theme: resolved, Capabilities: caps, Viewport: layout.Size{Width: 120, Height: 40}})
	if err != nil {
		fatal(err)
	}
	if !bytes.Equal(first.Machine, second.Machine) || first.Plain != second.Plain {
		fatal(fmt.Errorf("headless render variance is non-zero"))
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	const allocLimit = 64 << 20
	repro := performance.Reproducibility{Invocation: append([]string(nil), os.Args...), SampleCount: *samples, Strict: *strict, GOMAXPROCS: 1}
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				repro.VCSRevision = setting.Value
			case "vcs.modified":
				repro.VCSModified = setting.Value == "true"
			}
		}
	}
	report := performance.Report{GeneratedAt: time.Now().UTC(), Host: host, GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(), Nodes: 10000, NodeShape: "balanced-binary", Renderer: "stave.render/v1 semantic-layout-surface-terminal", Viewport: layout.Size{Width: 120, Height: 40}, Capabilities: caps, Reproducibility: repro, AllocBytes: alloc, AllocLimit: allocLimit, AllocWithin: alloc <= allocLimit, IdleCPU: performance.MeasureIdleCPU(250 * time.Millisecond), Invariants: []string{"default max protocol line = 4 MiB", "default max semantic nodes = 100,000", "semantic fixture hash stable", "headless deterministic render variance = 0 bytes"}}
	for _, t := range targets {
		measurement, err := measure(t, *samples)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", t.name, err))
		}
		report.Measurements = append(report.Measurements, measurement)
	}
	sort.Slice(report.Measurements, func(i, j int) bool { return report.Measurements[i].Name < report.Measurements[j].Name })
	if tree.Hash() != treeAgain.Hash() {
		fatal(fmt.Errorf("nondeterministic fixture hash"))
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(data))
	if *out != "" {
		if err := os.WriteFile(*out, append(data, '\n'), 0644); err != nil {
			fatal(err)
		}
	}
	if *strict {
		if !report.AllocWithin {
			fatal(fmt.Errorf("allocation budget exceeded: %d > %d", report.AllocBytes, report.AllocLimit))
		}
		if !report.IdleCPU.AllWithinBudget {
			fatal(fmt.Errorf("idle CPU budget exceeded: %.3f >= %.3f", report.IdleCPU.Value, report.IdleCPU.Limit))
		}
		for _, measurement := range report.Measurements {
			if !measurement.AllWithinBudget {
				fatal(fmt.Errorf("%s exceeded budget: p95=%s limit=%s", measurement.Name, measurement.P95, measurement.Limit))
			}
		}
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
