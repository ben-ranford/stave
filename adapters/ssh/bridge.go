package stavessh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	charmssh "charm.land/ssh"
	wish "charm.land/wish/v2"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/diag"
)

const (
	ProtocolVersion              = "1.0"
	defaultMaxEnvironmentEntries = 64
	defaultMaxEnvironmentBytes   = 16 << 10
	defaultMaxCommandBytes       = 8 << 10
	defaultMaxInputBytes         = 1 << 20
	defaultMaxInputChunkBytes    = 4 << 10
	defaultMaxOutputBytes        = 1 << 20
	defaultMaxDiagnosticBytes    = 64 << 10
	defaultMaxTreeNodes          = 100_000
	defaultOpenTimeout           = 5 * time.Second
	defaultDispatchTimeout       = 5 * time.Second
	defaultRestoreTimeout        = 2 * time.Second
	defaultCloseTimeout          = 2 * time.Second
)

type SessionFactory interface {
	Open(context.Context, OpenRequest) (Session, error)
}

type SessionFactoryFunc func(context.Context, OpenRequest) (Session, error)

func (fn SessionFactoryFunc) Open(ctx context.Context, req OpenRequest) (Session, error) {
	return fn(ctx, req)
}

type Session interface {
	Dispatch(context.Context, Event) error
	Wait() error
	Restore(context.Context) error
	Close() error
}

type Kind string

const (
	KindText     Kind = "text"
	KindResize   Kind = "resize"
	KindCancel   Kind = "cancel"
	KindShutdown Kind = "shutdown"
)

type Event struct {
	Kind    Kind
	Payload any
}

type OpenRequest struct {
	ID            string
	User          string
	Command       []string
	RawCommand    string
	Subsystem     string
	Environment   map[string]string
	Manifest      capability.Manifest
	Limits        Limits
	InitialWindow Window
	RemoteAddr    string
	LocalAddr     string
	ClientVersion string
	ServerVersion string
	Output        io.Writer
	Diagnostics   io.Writer
}

type Window struct {
	Width  int
	Height int
}

type InputChunk struct {
	Data []byte
}

type Resize struct {
	Window Window
}

type Lifecycle struct {
	Reason string
	Err    string
}

type Limits struct {
	MaxEnvironmentEntries int
	MaxEnvironmentBytes   int
	MaxCommandBytes       int
	MaxInputBytes         int
	MaxInputChunkBytes    int
	MaxOutputBytes        int
	MaxDiagnosticBytes    int
	MaxTreeNodes          int
}

type Timeouts struct {
	Open     time.Duration
	Dispatch time.Duration
	Restore  time.Duration
	Close    time.Duration
	Session  time.Duration
}

type Observer interface {
	Event(context.Context, diag.Diagnostic)
}

type ObserverFunc func(context.Context, diag.Diagnostic)

func (fn ObserverFunc) Event(ctx context.Context, d diag.Diagnostic) {
	fn(ctx, d)
}

type Option func(*options)

func WithCapabilityPolicy(policy capability.Policy) Option {
	return func(o *options) {
		o.policy = policy
	}
}

func WithLimits(limits Limits) Option {
	return func(o *options) {
		o.limits = limits
	}
}

func WithTimeouts(timeouts Timeouts) Option {
	return func(o *options) {
		o.timeouts = timeouts
	}
}

func WithObserver(observer Observer) Option {
	return func(o *options) {
		o.observer = observer
	}
}

func WithManifestDecorator(fn func(OpenRequest, capability.Manifest) capability.Manifest) Option {
	return func(o *options) {
		o.decorateManifest = fn
	}
}

func Handler(factory SessionFactory, opts ...Option) charmssh.Handler {
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(raw charmssh.Session) {
		transport := bridge{factory: factory, options: cfg}
		transport.serve(raw)
	}
}

func Middleware(factory SessionFactory, opts ...Option) wish.Middleware {
	handler := Handler(factory, opts...)
	return func(_ charmssh.Handler) charmssh.Handler {
		return handler
	}
}

type options struct {
	policy           capability.Policy
	limits           Limits
	timeouts         Timeouts
	observer         Observer
	decorateManifest func(OpenRequest, capability.Manifest) capability.Manifest
}

func defaultOptions() options {
	return options{
		limits: Limits{
			MaxEnvironmentEntries: defaultMaxEnvironmentEntries,
			MaxEnvironmentBytes:   defaultMaxEnvironmentBytes,
			MaxCommandBytes:       defaultMaxCommandBytes,
			MaxInputBytes:         defaultMaxInputBytes,
			MaxInputChunkBytes:    defaultMaxInputChunkBytes,
			MaxOutputBytes:        defaultMaxOutputBytes,
			MaxDiagnosticBytes:    defaultMaxDiagnosticBytes,
			MaxTreeNodes:          defaultMaxTreeNodes,
		},
		timeouts: Timeouts{
			Open:     defaultOpenTimeout,
			Dispatch: defaultDispatchTimeout,
			Restore:  defaultRestoreTimeout,
			Close:    defaultCloseTimeout,
		},
	}
}

type bridge struct {
	factory    SessionFactory
	options    options
	dispatcher *dispatchGate
}

type dispatchGate struct{ token chan struct{} }

func (b bridge) serve(raw charmssh.Session) {
	if b.dispatcher == nil {
		b.dispatcher = &dispatchGate{token: make(chan struct{}, 1)}
		b.dispatcher.token <- struct{}{}
	}
	ctx := context.Context(raw.Context())
	if b.options.timeouts.Session > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.options.timeouts.Session)
		defer cancel()
	}

	req, err := b.openRequest(raw)
	if err != nil {
		diagWriter := newLockedWriter(raw.Stderr(), b.options.limits.MaxDiagnosticBytes)
		b.emitDiagnostic(ctx, diagWriter, diag.ErrorSeverity, "RESOURCE_LIMIT", err.Error(), nil, true)
		_ = raw.Exit(1)
		return
	}

	openCtx := withTimeout(ctx, b.options.timeouts.Open)
	session, err := b.factory.Open(openCtx, req)
	cancelTimeout(openCtx)
	if err != nil {
		b.emitDiagnostic(ctx, req.Diagnostics, diag.ErrorSeverity, "SESSION_OPEN_FAILED", "ssh adapter session initialization failed", nil, true)
		_ = raw.Exit(1)
		return
	}

	var pumps sync.WaitGroup

	if _, winch, ok := raw.Pty(); ok && winch != nil {
		pumps.Add(1)
		go func() {
			defer pumps.Done()
			b.pumpResize(ctx, session, winch, req.Diagnostics)
		}()
	}

	pumps.Add(1)
	go func() {
		defer pumps.Done()
		b.pumpDisconnect(ctx, raw, session, req.Diagnostics)
	}()

	pumps.Add(1)
	go func() {
		defer pumps.Done()
		b.pumpInput(ctx, raw, session, req.Diagnostics)
	}()

	waitErr := session.Wait()
	exitCode := 0
	if waitErr != nil && !isBenign(waitErr) {
		exitCode = 1
		b.emitDiagnostic(ctx, req.Diagnostics, diag.ErrorSeverity, "SESSION_WAIT_FAILED", "bridge session terminated with an error", nil, true)
	}

	b.cleanup(raw, session, req.Diagnostics, exitCode)
	pumps.Wait()
}

func (b bridge) cleanup(raw charmssh.Session, session Session, diagnostics io.Writer, exitCode int) {
	restoreCtx := withTimeout(context.Background(), b.options.timeouts.Restore)
	if err := session.Restore(restoreCtx); err != nil && !isBenign(err) {
		b.emitDiagnostic(context.Background(), diagnostics, diag.Warning, "RESTORE_FAILED", "session restore failed during shutdown", nil, true)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	cancelTimeout(restoreCtx)

	_ = raw.Exit(exitCode)

	closeCtx := withTimeout(context.Background(), b.options.timeouts.Close)
	if err := session.Close(); err != nil && !isBenign(err) {
		b.emitDiagnostic(closeCtx, diagnostics, diag.Warning, "SESSION_CLOSE_FAILED", "session close failed during shutdown", nil, true)
	}
	cancelTimeout(closeCtx)
}

func (b bridge) pumpDisconnect(ctx context.Context, raw charmssh.Session, session Session, diagnostics io.Writer) {
	<-raw.Context().Done()
	errText := ""
	if err := raw.Context().Err(); err != nil {
		errText = err.Error()
	}
	_ = b.dispatch(ctx, session, Event{
		Kind: KindCancel,
		Payload: Lifecycle{
			Reason: "client_disconnect",
			Err:    errText,
		},
	}, diagnostics)
}

func (b bridge) pumpResize(ctx context.Context, session Session, winch <-chan charmssh.Window, diagnostics io.Writer) {
	queue := make(chan Window, 1)
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for win := range winch {
			select {
			case queue <- Window{Width: win.Width, Height: win.Height}:
			default:
				select {
				case <-queue:
				default:
				}
				queue <- Window{Width: win.Width, Height: win.Height}
			}
		}
		close(queue)
	}()

	for win := range queue {
		if err := b.dispatch(ctx, session, Event{
			Kind:    KindResize,
			Payload: Resize{Window: win},
		}, diagnostics); err != nil {
			b.emitDiagnostic(ctx, diagnostics, diag.Warning, "RESIZE_DISPATCH_FAILED", "resize dispatch failed", nil, true)
			break
		}
	}
	readers.Wait()
}

func (b bridge) pumpInput(ctx context.Context, raw charmssh.Session, session Session, diagnostics io.Writer) {
	limits := b.options.limits
	if limits.MaxInputChunkBytes <= 0 {
		limits.MaxInputChunkBytes = defaultMaxInputChunkBytes
	}
	buf := make([]byte, limits.MaxInputChunkBytes)
	total := 0
	for {
		n, err := raw.Read(buf)
		if n > 0 {
			total += n
			if limits.MaxInputBytes > 0 && total > limits.MaxInputBytes {
				b.emitDiagnostic(ctx, diagnostics, diag.ErrorSeverity, "RESOURCE_LIMIT", "input stream exceeded configured byte budget", nil, true)
				_ = b.dispatch(ctx, session, Event{
					Kind: KindCancel,
					Payload: Lifecycle{
						Reason: "resource_limit",
					},
				}, diagnostics)
				return
			}
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if dispatchErr := b.dispatch(ctx, session, Event{
				Kind:    KindText,
				Payload: InputChunk{Data: chunk},
			}, diagnostics); dispatchErr != nil {
				b.emitDiagnostic(ctx, diagnostics, diag.Warning, "DISPATCH_FAILED", "input dispatch failed", nil, true)
				return
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			_ = b.dispatch(ctx, session, Event{
				Kind: KindShutdown,
				Payload: Lifecycle{
					Reason: "stdin_eof",
				},
			}, diagnostics)
			return
		}
		if !isBenign(err) {
			b.emitDiagnostic(ctx, diagnostics, diag.Warning, "INPUT_READ_FAILED", "ssh input read failed", nil, true)
			_ = b.dispatch(ctx, session, Event{
				Kind: KindCancel,
				Payload: Lifecycle{
					Reason: "input_error",
					Err:    err.Error(),
				},
			}, diagnostics)
		}
		return
	}
}

func (b bridge) openRequest(raw charmssh.Session) (OpenRequest, error) {
	env, err := parseEnvironment(raw.Environ(), b.options.limits)
	if err != nil {
		return OpenRequest{}, err
	}
	pty, _, hasPTY := raw.Pty()
	width, height := 0, 0
	if hasPTY {
		width = pty.Window.Width
		height = pty.Window.Height
	}
	runtime := capability.Detect(capability.Probe{
		Env:    env,
		TTY:    hasPTY,
		Width:  width,
		Height: height,
	})
	runtime.ProtocolVersions = []string{ProtocolVersion}
	runtime.SnapshotModes = []string{"semantic-json", "plain-text"}
	runtime.ActionFamilies = []string{"human"}
	runtime.Limits = capability.Limits{
		MaxTreeNodes:    positiveOrDefault(b.options.limits.MaxTreeNodes, defaultMaxTreeNodes),
		MaxMessageBytes: positiveOrDefault(b.options.limits.MaxOutputBytes, defaultMaxOutputBytes),
		MaxInputBytes:   positiveOrDefault(b.options.limits.MaxInputBytes, defaultMaxInputBytes),
	}
	manifest, _ := (capability.Negotiation{
		RuntimeDetected: runtime,
		Application:     b.options.policy,
	}).Resolve()

	stdout := newLockedWriter(raw, b.options.limits.MaxOutputBytes)
	stderr := newLockedWriter(raw.Stderr(), b.options.limits.MaxDiagnosticBytes)
	req := OpenRequest{
		ID:            raw.Context().SessionID(),
		User:          raw.User(),
		Command:       append([]string(nil), raw.Command()...),
		RawCommand:    raw.RawCommand(),
		Subsystem:     raw.Subsystem(),
		Environment:   env,
		Manifest:      manifest,
		Limits:        b.options.limits,
		InitialWindow: Window{Width: width, Height: height},
		RemoteAddr:    addrString(raw.RemoteAddr()),
		LocalAddr:     addrString(raw.LocalAddr()),
		ClientVersion: raw.Context().ClientVersion(),
		ServerVersion: raw.Context().ServerVersion(),
		Output:        stdout,
		Diagnostics:   stderr,
	}
	if b.options.decorateManifest != nil {
		decorated := b.options.decorateManifest(req, req.Manifest.Clone())
		req.Manifest = denyOnlyManifest(req.Manifest, decorated)
	}
	if err := validateCommandBudget(req, b.options.limits); err != nil {
		return OpenRequest{}, err
	}
	return req, nil
}

func (b bridge) dispatch(ctx context.Context, session Session, ev Event, diagnostics io.Writer) error {
	if b.dispatcher == nil {
		b.dispatcher = &dispatchGate{token: make(chan struct{}, 1)}
		b.dispatcher.token <- struct{}{}
	}
	// Cancellation and shutdown are control-plane messages and must not wait
	// behind a blocked data dispatch during disconnect cleanup.
	if ev.Kind != KindCancel && ev.Kind != KindShutdown {
		select {
		case <-b.dispatcher.token:
			defer func() { b.dispatcher.token <- struct{}{} }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	callCtx := withTimeout(ctx, b.options.timeouts.Dispatch)
	defer cancelTimeout(callCtx)
	if err := session.Dispatch(callCtx, ev); err != nil {
		if !isBenign(err) {
			b.emitDiagnostic(ctx, diagnostics, diag.Warning, "DISPATCH_FAILED", "event dispatch failed", map[string]string{"kind": string(ev.Kind)}, true)
		}
		return err
	}
	return nil
}

func (b bridge) emitDiagnostic(ctx context.Context, out io.Writer, severity diag.Severity, code, message string, attributes map[string]string, redacted bool) {
	d := diag.Diagnostic{
		SchemaVersion: "stave.diag/v1",
		ID:            code,
		Time:          time.Now().UTC(),
		Severity:      severity,
		Category:      "adapter.ssh",
		Code:          code,
		Message:       message,
		Attributes:    attributes,
		Redacted:      redacted,
	}
	if b.options.observer != nil {
		b.options.observer.Event(ctx, d)
	}
	if out == nil {
		return
	}
	line := safeDiagnosticLine(d)
	_, _ = io.WriteString(out, line)
}

func safeDiagnosticLine(d diag.Diagnostic) string {
	message := sanitizeText(d.Message)
	if message == "" {
		message = "redacted diagnostic"
	}
	return fmt.Sprintf("[%s] %s: %s\n", d.Severity, d.Code, message)
}

func parseEnvironment(values []string, limits Limits) (map[string]string, error) {
	env := make(map[string]string, len(values))
	totalBytes := 0
	for _, entry := range values {
		if limits.MaxEnvironmentEntries > 0 && len(env) >= limits.MaxEnvironmentEntries {
			return nil, errors.New("environment exceeds configured entry budget")
		}
		totalBytes += len(entry)
		if limits.MaxEnvironmentBytes > 0 && totalBytes > limits.MaxEnvironmentBytes {
			return nil, errors.New("environment exceeds configured byte budget")
		}
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}
	return env, nil
}

func validateCommandBudget(req OpenRequest, limits Limits) error {
	total := len(req.RawCommand)
	for _, part := range req.Command {
		total += len(part)
	}
	if limits.MaxCommandBytes > 0 && total > limits.MaxCommandBytes {
		return errors.New("command exceeds configured byte budget")
	}
	return nil
}

type lockedWriter struct {
	mu        sync.Mutex
	writer    io.Writer
	remaining int
}

func newLockedWriter(writer io.Writer, budget int) *lockedWriter {
	return &lockedWriter{
		writer:    writer,
		remaining: budget,
	}
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validateTerminalOutput(p); err != nil {
		return 0, err
	}
	if w.remaining == 0 {
		return 0, errors.New("writer byte budget exhausted")
	}
	if w.remaining > 0 && len(p) > w.remaining {
		return 0, errors.New("writer byte budget exhausted")
	}
	n, err := w.writer.Write(p)
	if w.remaining > 0 {
		w.remaining -= n
	}
	if err == nil && w.remaining == 0 {
		err = errors.New("writer byte budget exhausted")
	}
	return n, err
}

// validateTerminalOutput permits printable UTF-8, newlines, tabs, and SGR
// colour/style sequences only. Cursor movement, OSC, DCS, clipboard, title,
// and other terminal control protocols are rejected at the final SSH sink.
func validateTerminalOutput(p []byte) error {
	if !utf8.Valid(p) {
		return errors.New("output is not valid UTF-8")
	}
	for i := 0; i < len(p); {
		if p[i] == '\x1b' {
			next, ok := consumeSGR(p, i)
			if !ok {
				return errors.New("output contains an unsafe ANSI sequence")
			}
			i = next
			continue
		}
		r, size := utf8.DecodeRune(p[i:])
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return errors.New("output contains unsafe control characters")
		}
		i += size
	}
	return nil
}

func consumeSGR(p []byte, start int) (int, bool) {
	if start+2 >= len(p) || p[start+1] != '[' {
		return 0, false
	}
	for i := start + 2; i < len(p); i++ {
		switch p[i] {
		case 'm':
			return i + 1, true
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ';':
			continue
		default:
			return 0, false
		}
	}
	return 0, false
}

// Decorators are capability deny-lists: they may turn advertised capabilities
// off, but cannot elevate a terminal beyond the negotiated runtime manifest.
func denyOnlyManifest(base, decorated capability.Manifest) capability.Manifest {
	decorated.ProtocolVersions = append([]string(nil), base.ProtocolVersions...)
	decorated.Width, decorated.Height = minPositive(base.Width, decorated.Width), minPositive(base.Height, decorated.Height)
	decorated.Color = denyColor(base.Color, decorated.Color)
	decorated.HardwareColor = denyColor(base.HardwareColor, decorated.HardwareColor)
	decorated.Interactive = base.Interactive && decorated.Interactive
	decorated.TTY = base.TTY && decorated.TTY
	decorated.CursorAddressing = base.CursorAddressing && decorated.CursorAddressing
	decorated.AlternateScreen = base.AlternateScreen && decorated.AlternateScreen
	decorated.Mouse = base.Mouse && decorated.Mouse
	decorated.BracketedPaste = base.BracketedPaste && decorated.BracketedPaste
	decorated.SecureInput = base.SecureInput && decorated.SecureInput
	decorated.ScreenReader = base.ScreenReader && decorated.ScreenReader
	decorated.Clipboard = base.Clipboard && decorated.Clipboard
	decorated.CoordinateFallback = base.CoordinateFallback && decorated.CoordinateFallback
	return decorated
}

func minPositive(base, requested int) int {
	if base <= 0 {
		return requested
	}
	if requested <= 0 || requested > base {
		return base
	}
	return requested
}

func denyColor(base, requested capability.ColorLevel) capability.ColorLevel {
	order := map[capability.ColorLevel]int{capability.ColorNone: 0, capability.ColorMonochrome: 1, capability.ColorANSI16: 2, capability.ColorANSI256: 3, capability.ColorTrueColor: 4}
	if order[requested] > order[base] {
		return base
	}
	return requested
}

func sanitizeText(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if r == '\n' || r == '\t' {
			builder.WriteRune(r)
			continue
		}
		if r == utf8.RuneError {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		if !unicode.IsPrint(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func addrString(addr fmt.Stringer) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

func positiveOrDefault(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func withTimeout(ctx context.Context, timeout time.Duration) context.Context {
	if timeout <= 0 {
		return ctx
	}
	next, cancel := context.WithTimeout(ctx, timeout)
	return context.WithValue(next, timeoutCancelKey{}, cancel)
}

type timeoutCancelKey struct{}

func cancelTimeout(ctx context.Context) {
	cancel, _ := ctx.Value(timeoutCancelKey{}).(context.CancelFunc)
	if cancel != nil {
		cancel()
	}
}

func isBenign(err error) bool {
	return err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF)
}
