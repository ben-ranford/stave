// Package performance contains deterministic, dependency-free fixtures used by
// the release performance gate.  The fixtures intentionally use public Stave
// APIs so their measurements track the contract consumers exercise.
package performance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/metrics"
	"sort"
	"time"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/runtime/human"
	"github.com/ben-ranford/stave/secret"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/surface"
)

type Budget struct {
	Name  string        `json:"name"`
	Limit time.Duration `json:"limit"`
}
type Reproducibility struct {
	Invocation  []string `json:"invocation"`
	SampleCount int      `json:"sampleCount"`
	Strict      bool     `json:"strict"`
	GOMAXPROCS  int      `json:"gomaxprocs"`
	VCSRevision string   `json:"vcsRevision,omitempty"`
	VCSModified bool     `json:"vcsModified,omitempty"`
}
type Attempt struct {
	P50 time.Duration `json:"p50"`
	P95 time.Duration `json:"p95"`
	P99 time.Duration `json:"p99"`
}
type Measurement struct {
	Name            string        `json:"name"`
	Samples         int           `json:"samples"`
	P50             time.Duration `json:"p50"`
	P95             time.Duration `json:"p95"`
	P99             time.Duration `json:"p99"`
	AllWithinBudget bool          `json:"allWithinBudget"`
	Limit           time.Duration `json:"limit"`
	Disposition     string        `json:"disposition,omitempty"`
	Attempts        []Attempt     `json:"attempts,omitempty"`
}
type Report struct {
	GeneratedAt     time.Time           `json:"generatedAt"`
	Host            string              `json:"host"`
	GoVersion       string              `json:"goVersion"`
	GOOS            string              `json:"goos"`
	GOARCH          string              `json:"goarch"`
	CPUs            int                 `json:"cpus"`
	Nodes           int                 `json:"nodes"`
	NodeShape       string              `json:"nodeDistribution"`
	Renderer        string              `json:"renderer"`
	Viewport        layout.Size         `json:"viewport"`
	Capabilities    capability.Manifest `json:"capabilities"`
	Reproducibility Reproducibility     `json:"reproducibility"`
	AllocBytes      uint64              `json:"allocBytes"`
	AllocLimit      uint64              `json:"allocLimitBytes"`
	AllocWithin     bool                `json:"allocWithinBudget"`
	IdleCPU         RatioMeasurement    `json:"idleCpu"`
	Measurements    []Measurement       `json:"measurements"`
	Invariants      []string            `json:"invariants"`
}

type RatioMeasurement struct {
	Name            string        `json:"name"`
	Window          time.Duration `json:"window"`
	Value           float64       `json:"value"`
	Limit           float64       `json:"limit"`
	AllWithinBudget bool          `json:"allWithinBudget"`
	Disposition     string        `json:"disposition,omitempty"`
}

const Samples = 101

// Percentiles returns deterministic nearest-rank p50/p95/p99 values.
func Percentiles(samples []time.Duration) (p50, p95, p99 time.Duration, err error) {
	if len(samples) == 0 {
		return 0, 0, 0, fmt.Errorf("no samples")
	}
	v := append([]time.Duration(nil), samples...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	rate := func(p int) time.Duration {
		idx := (len(v)*p+99)/100 - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(v) {
			idx = len(v) - 1
		}
		return v[idx]
	}
	return rate(50), rate(95), rate(99), nil
}

// Fixture creates a balanced semantic tree with exactly n nodes.
func Fixture(n int) (semantic.Tree, error) {
	if n < 1 {
		return semantic.Tree{}, fmt.Errorf("node count must be positive")
	}
	var next int
	var build func(int) (semantic.Node, error)
	build = func(left int) (semantic.Node, error) {
		next++
		id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "performance", View: "bench", Kind: "node", Entity: fmt.Sprint(next), Slot: "main"})
		if err != nil {
			return semantic.Node{}, err
		}
		take := 0
		if left > 1 {
			take = left / 2
			if take == left {
				take--
			}
		}
		children := make([]semantic.Node, 0, 2)
		if take > 0 {
			c, err := build(take)
			if err != nil {
				return semantic.Node{}, err
			}
			children = append(children, c)
		}
		if left-1-take > 0 {
			c, err := build(left - 1 - take)
			if err != nil {
				return semantic.Node{}, err
			}
			children = append(children, c)
		}
		role := semantic.Role("text")
		name := fmt.Sprintf("node-%d", next)
		return semantic.NewNode(semantic.NodeSpec{ID: id, Role: role, Name: name, Value: semantic.Value{Text: name}, Children: children, Flags: semantic.Flags{Visible: true}})
	}
	root, err := build(n)
	if err != nil {
		return semantic.Tree{}, err
	}
	return semantic.NewTree(1, root)
}

func SurfaceFixture(w, h int) (surface.Surface, error) {
	_, err := surface.NewBounded(w, h, w*h)
	if err != nil {
		return surface.Surface{}, err
	}
	b, err := surface.NewBuilder(w, h, w*h)
	if err != nil {
		return surface.Surface{}, err
	}
	for y := 0; y < h; y++ {
		if err := b.WithText(0, y, "Stave performance fixture", surface.ResolvedStyle{Foreground: "#64c8ff"}, "", semantic.NodeID(""), 0, layout.Rect{Width: w, Height: 1}); err != nil {
			return surface.Surface{}, err
		}
	}
	return b.Surface(), nil
}

func Measure(fn func()) time.Duration { start := time.Now(); fn(); return time.Since(start) }

// MeasureIdleCPU reports estimated Go user CPU as a percentage of one core
// during an otherwise idle window. The runtime metric is deliberately compared
// only with itself, matching the contract documented by runtime/metrics.
func MeasureIdleCPU(window time.Duration) RatioMeasurement {
	if window <= 0 {
		window = 250 * time.Millisecond
	}
	read := func() float64 {
		samples := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}}
		metrics.Read(samples)
		return samples[0].Value.Float64()
	}
	before := read()
	start := time.Now()
	time.Sleep(window)
	elapsed := time.Since(start)
	used := read() - before
	percent := used / elapsed.Seconds() * 100
	return RatioMeasurement{
		Name:            "idle_cpu.percent_one_core",
		Window:          elapsed,
		Value:           percent,
		Limit:           1,
		AllWithinBudget: percent < 1,
		Disposition:     "runtime/metrics estimate; investigate misses via ADR before release",
	}
}

// ActionAckLine is a pure protocol acknowledgement round-trip fixture.
func ActionAckLine() ([]byte, error) {
	return json.Marshal(protocol.Request{JSONRPC: protocol.JSONRPC, ID: json.RawMessage(`1`), Method: "actions/ack", Params: json.RawMessage(`{"status":"accepted"}`)})
}

func OrdinaryEvent() event.Event {
	e, _ := event.New(event.Key, event.KeyPayload{Key: "enter"})
	return e
}

// ReduceCounter is the smallest representative pure reducer used by the
// performance gate; it deliberately performs no I/O or allocation-heavy work.
func ReduceCounter(ctx context.Context, count int, ev event.Event) (int, []effect.Request, error) {
	if err := ctx.Err(); err != nil {
		return count, nil, err
	}
	if ev.Kind == event.Key {
		return count + 1, nil, nil
	}
	return count, nil, nil
}

func Context() context.Context { return context.Background() }

// TerminalRestoreAfterCancellation exercises the public human-runtime
// cancellation path and its mandatory restore/close lifecycle.
func TerminalRestoreAfterCancellation() error {
	driver := &measurementDriver{events: make(chan event.Event), manifest: capability.Manifest{TTY: true, Interactive: true}}
	runtime, err := human.New(human.Options{Driver: driver})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = runtime.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if driver.restores != 1 || driver.closes != 1 {
		return fmt.Errorf("terminal lifecycle restore=%d close=%d", driver.restores, driver.closes)
	}
	return nil
}

// ResizeStormRecovery measures the bounded runtime path from a queued resize
// burst through the final resize and terminal shutdown.
func ResizeStormRecovery(count int) error {
	if count < 1 {
		return fmt.Errorf("resize count must be positive")
	}
	driver := &measurementDriver{events: make(chan event.Event, count+1), manifest: capability.Manifest{TTY: true, Interactive: true}}
	for i := 1; i <= count; i++ {
		driver.events <- event.Event{Kind: event.Resize, Payload: event.ResizePayload{Width: 80 + i, Height: 24}}
	}
	driver.events <- event.Event{Kind: event.Shutdown}
	var last int
	runtime, err := human.New(human.Options{Driver: driver, QueueCapacity: 8, Handle: func(_ context.Context, ev event.Event) error {
		if ev.Kind == event.Resize {
			last = ev.Payload.(event.ResizePayload).Width
		}
		return nil
	}})
	if err != nil {
		return err
	}
	if err := runtime.Run(context.Background()); err != nil {
		return err
	}
	if last != 80+count {
		return fmt.Errorf("last resize=%d want=%d", last, 80+count)
	}
	return nil
}

type measurementDriver struct {
	events           chan event.Event
	manifest         capability.Manifest
	restores, closes int
}

func (d *measurementDriver) Open(context.Context, capability.Policy) (capability.Manifest, error) {
	return d.manifest, nil
}
func (d *measurementDriver) Events() <-chan event.Event { return d.events }
func (d *measurementDriver) Draw(context.Context, surface.Surface, surface.Patch) error {
	return nil
}
func (d *measurementDriver) ReadSecret(context.Context, input.SecretPrompt) (secret.Handle, error) {
	return secret.Handle{}, human.ErrSecureInputDenied
}
func (d *measurementDriver) Restore(context.Context) error { d.restores++; return nil }
func (d *measurementDriver) Close() error                  { d.closes++; return nil }
