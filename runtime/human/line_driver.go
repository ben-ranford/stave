package human

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/secret"
	"github.com/ben-ranford/stave/surface"
)

// LineDriverOptions are explicit host inputs for the dependency-free line and
// non-TTY driver. Environment discovery remains at this adapter boundary and
// does not leak into Stave's deterministic core packages.
type LineDriverOptions struct {
	Input       io.Reader
	Output      io.Writer
	Environment map[string]string
	TTY         bool
	Width       int
	Height      int
	Queue       int
	ByteLimit   int
}

// LineDriver is a production human Driver for line-oriented terminals, pipes,
// CI logs, and tests. Full-screen/raw-mode ownership remains an optional
// adapter concern; both paths emit the same canonical events and surfaces.
type LineDriver struct {
	opt      LineDriverOptions
	events   chan event.Event
	mu       sync.Mutex
	writeMu  sync.Mutex
	opened   bool
	closed   bool
	manifest capability.Manifest
	cancel   context.CancelFunc
	close    sync.Once
}

func NewLineDriver(opts LineDriverOptions) (*LineDriver, error) {
	if opts.Output == nil {
		return nil, errors.New("human line driver output is required")
	}
	if opts.Queue <= 0 {
		opts.Queue = 64
	}
	if opts.Width <= 0 {
		opts.Width = 80
	}
	if opts.Height <= 0 {
		opts.Height = 24
	}
	opts.Environment = cloneStrings(opts.Environment)
	return &LineDriver{opt: opts, events: make(chan event.Event, opts.Queue)}, nil
}

func (d *LineDriver) Open(ctx context.Context, _ capability.Policy) (capability.Manifest, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return capability.Manifest{}, ErrStopped
	}
	if d.opened {
		return d.manifest.Clone(), nil
	}
	d.opened = true
	d.manifest = honestLineManifest(capability.DetectEnv(d.opt.Environment, d.opt.TTY, d.opt.Width, d.opt.Height))
	if !d.opt.TTY {
		d.manifest.Interactive = false
		d.manifest.Color = capability.ColorNone
		d.manifest.ColorDisabled = true
		d.manifest.OutputMode = capability.OutputPlain
	}
	scanCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	go d.scan(scanCtx)
	return d.manifest.Clone(), nil
}

func (d *LineDriver) Events() <-chan event.Event { return d.events }

func (d *LineDriver) Draw(_ context.Context, next surface.Surface, _ surface.Patch) error {
	d.mu.Lock()
	manifest := d.manifest.Clone()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return ErrStopped
	}
	text, err := (render.Writer{Manifest: manifest, ByteLimit: d.opt.ByteLimit}).Render(next)
	if err != nil {
		return err
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err = io.WriteString(d.opt.Output, text)
	return err
}

func (d *LineDriver) ReadSecret(context.Context, input.SecretPrompt) (secret.Handle, error) {
	return secret.Handle{}, ErrSecureInputDenied
}

func (d *LineDriver) Restore(context.Context) error { return nil }

func (d *LineDriver) Close() error {
	var closeErr error
	d.close.Do(func() {
		d.mu.Lock()
		d.closed = true
		cancel := d.cancel
		d.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if closer, ok := d.opt.Input.(io.Closer); ok {
			closeErr = closer.Close()
		}
	})
	return closeErr
}

func (d *LineDriver) scan(ctx context.Context) {
	defer close(d.events)
	if d.opt.Input == nil {
		d.emit(ctx, shutdownEvent())
		return
	}
	scanner := bufio.NewScanner(d.opt.Input)
	scanner.Buffer(make([]byte, 4096), input.DefaultMaxPasteBytes)
	for scanner.Scan() {
		normalized, _, err := input.NormalizeText(scanner.Text(), input.DefaultMaxTextBytes)
		if err != nil {
			d.emit(ctx, diagnosticEvent("LINE_INPUT_REJECTED", "line input was rejected"))
			continue
		}
		ev, err := event.New(event.Text, event.TextPayload{Text: normalized.Value, Committed: true})
		if err != nil {
			d.emit(ctx, diagnosticEvent("LINE_INPUT_REJECTED", "line input was rejected"))
			continue
		}
		if !d.emit(ctx, ev) {
			return
		}
	}
	if scanner.Err() != nil {
		d.emit(ctx, diagnosticEvent("LINE_INPUT_FAILED", "line input failed"))
	}
	d.emit(ctx, shutdownEvent())
}

func (d *LineDriver) emit(ctx context.Context, ev event.Event) bool {
	select {
	case d.events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func diagnosticEvent(code, message string) event.Event {
	ev, err := event.New(event.Diagnostic, event.DiagnosticPayload{Code: code, Message: message})
	if err != nil {
		panic(err)
	}
	return ev
}

func cloneStrings(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func honestLineManifest(m capability.Manifest) capability.Manifest {
	m.CursorAddressing = false
	m.AlternateScreen = false
	m.Mouse = false
	m.BracketedPaste = false
	return m
}
