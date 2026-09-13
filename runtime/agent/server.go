package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/config"
	"github.com/ben-ranford/stave/diag"
	"github.com/ben-ranford/stave/observer"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/semantic"
)

type SnapshotProvider func(context.Context) (any, uint64, string, error)
type SnapshotPatchProvider func(context.Context, uint64) (any, uint64, string, error)
type SnapshotEnvelope struct {
	Snapshot                                                                       *semantic.Snapshot
	Patch                                                                          *semantic.Patch
	Mode                                                                           string
	SessionID                                                                      string
	Sequence, Revision                                                             uint64
	TreeHash, CapabilityHash, SemanticVersion, ConfigHash, ThemeHash, WidthVersion string
	Actions                                                                        []action.Definition
	Diagnostics                                                                    []diag.Diagnostic
}
type SnapshotEnvelopeProvider func(context.Context, string, uint64) (SnapshotEnvelope, error)

// AuthorizeCall runs before Registry.Invoke.
// It is the authority boundary for target generation/revision, capabilities,
// authorization, and policy. A non-nil error prevents handler work.
type AuthorizeCall func(context.Context, action.Call) *action.Error

// AuthorizePreparedCall may return an authoritative call after policy
// enrichment. The returned call, rather than caller-supplied fields, is sent
// to confirmation and execution handlers.
type AuthorizePreparedCall func(context.Context, action.Call) (action.Call, *action.Error)
type ConfirmCall func(context.Context, action.Call) (action.Confirmation, error)
type CapabilityNegotiator func(context.Context, map[string]any) (capability.Manifest, error)
type Options struct {
	MaxMessageBytes  int
	MaxOutputBytes   int
	MaxTreeNodes     int
	MaxInFlight      int
	Queue            int
	Snapshot         SnapshotProvider
	SnapshotPatch    SnapshotPatchProvider
	SnapshotEnvelope SnapshotEnvelopeProvider
	Actions          *action.Registry
	// Deprecated: custom invoke handlers are unsupported because they bypass
	// registry validation. Use Actions as the execution authority.
	Invoke            func(context.Context, action.Call) action.Result
	Authorize         AuthorizeCall
	AuthorizePrepared AuthorizePreparedCall
	Confirm           ConfirmCall
	Negotiate         CapabilityNegotiator
	CancelSession     func(context.Context) error
	CompatibilityMode bool
	Observer          observer.Observer
	ServerName        string
	Application       protocol.Application
	SessionID         string
	Capabilities      map[string]any
	Manifest          any
}

// OptionsFromConfig explicitly projects the validated Config limits owned by
// the agent adapter. Applications retain ownership of transport selection and
// all other agent Options values, which can be set after this helper returns.
func OptionsFromConfig(cfg config.Config) (Options, error) {
	if err := config.Validate(cfg); err != nil {
		return Options{}, fmt.Errorf("agent: config: %w", err)
	}
	return Options{
		MaxMessageBytes: cfg.Protocol.MaxMessageBytes,
		MaxTreeNodes:    cfg.Security.MaxTreeNodes,
	}, nil
}

type Server struct {
	opt                                    Options
	mu                                     sync.Mutex
	initialized, ready, cancelling, closed bool
	calls                                  map[string]callSlot
	notify                                 chan protocol.Notification
	sessionID                              string
	negotiated                             any
	limits                                 protocol.Limits
	configErr                              error
}

type callSlot struct {
	ctx    context.Context
	cancel context.CancelFunc
}

const minimumOutputBytes = 128

var (
	ErrBackpressure = errors.New("protocol request queue full")
	ErrOutputLimit  = errors.New("protocol output exceeds limit")
)

func New(opts Options) *Server {
	if opts.MaxMessageBytes <= 0 {
		opts.MaxMessageBytes = 4 << 20
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = opts.MaxMessageBytes
	}
	if opts.MaxTreeNodes <= 0 {
		opts.MaxTreeNodes = 100_000
	}
	if opts.Queue <= 0 {
		opts.Queue = 64
	}
	if opts.MaxInFlight <= 0 || opts.MaxInFlight > opts.Queue {
		opts.MaxInFlight = min(4, opts.Queue)
	}
	if opts.Observer == nil {
		opts.Observer = observer.Nop{}
	}
	if opts.SessionID == "" {
		opts.SessionID = "stave-session"
	}
	if opts.ServerName == "" {
		opts.ServerName = "stave"
	}
	if opts.Application.ID == "" {
		opts.Application.ID = opts.ServerName
	}
	if opts.Application.Version == "" {
		opts.Application.Version = "development"
	}
	s := &Server{
		opt:       opts,
		calls:     map[string]callSlot{},
		notify:    make(chan protocol.Notification, opts.Queue),
		sessionID: opts.SessionID,
		limits: protocol.Limits{
			MaxMessageBytes: opts.MaxMessageBytes,
			MaxOutputBytes:  opts.MaxOutputBytes,
			MaxTreeNodes:    opts.MaxTreeNodes,
		},
	}
	if opts.MaxOutputBytes < minimumOutputBytes {
		s.configErr = fmt.Errorf("%w: max output bytes must be at least %d", ErrOutputLimit, minimumOutputBytes)
	}
	return s
}

// Serve processes one JSON-RPC object per UTF-8 line. The owned ReadCloser is
// closed on cancellation so no transport reader goroutine can outlive Serve.
// It never writes diagnostics to out.
func (s *Server) Serve(ctx context.Context, in io.ReadCloser, out io.Writer) error {
	s.mu.Lock()
	configErr := s.configErr
	if s.closed {
		s.mu.Unlock()
		return errors.New("server closed")
	}
	s.mu.Unlock()
	if configErr != nil {
		return configErr
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 4096), s.opt.MaxMessageBytes+1)
	type outbound struct {
		data []byte
		done chan error
	}
	writes := make(chan outbound, s.opt.Queue)
	notifications := s.notify
	writerDone := make(chan struct{})
	var writeErr error
	var writeMu sync.Mutex
	var errMu sync.Mutex
	setWriteErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if writeErr == nil {
			writeErr = err
		}
		errMu.Unlock()
	}
	getWriteErr := func() error {
		errMu.Lock()
		defer errMu.Unlock()
		return writeErr
	}
	go func() {
		defer close(writerDone)
		defer func() {
			if recover() != nil {
				setWriteErr(errors.New("protocol writer panic"))
			}
		}()
		for {
			select {
			case n, ok := <-notifications:
				if ok {
					b, e := json.Marshal(n)
					if e == nil && len(b) <= s.outputLimit() {
						writeMu.Lock()
						_, e = out.Write(append(b, '\n'))
						setWriteErr(e)
						writeMu.Unlock()
					}
					if e != nil {
						s.observe(ctx, "protocol.output_error", map[string]string{"cause": "write_failed"})
					}
				} else {
					notifications = nil
				}
			case item, ok := <-writes:
				if !ok {
					return
				}
				writeMu.Lock()
				_, e := out.Write(append(item.data, '\n'))
				setWriteErr(e)
				writeMu.Unlock()
				item.done <- e
			}
		}
	}()
	writeBytes := func(data []byte) error {
		done := make(chan error, 1)
		select {
		case writes <- outbound{data: data, done: done}:
		case <-writerDone:
			if err := getWriteErr(); err != nil {
				return err
			}
			return io.ErrClosedPipe
		}
		select {
		case err := <-done:
			return err
		case <-writerDone:
			if err := getWriteErr(); err != nil {
				return err
			}
			return io.ErrClosedPipe
		}
	}
	write := func(v any) error { return s.writeBounded(writeBytes, v) }

	type job struct{ request protocol.Request }
	jobs := make(chan job, s.opt.Queue)
	var wg sync.WaitGroup
	for range s.opt.MaxInFlight {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				resp := s.handleSafely(ctx, j.request)
				if len(j.request.ID) > 0 {
					if e := write(resp); e != nil {
						setWriteErr(e)
					}
				}
			}
		}()
	}
	control := func(method string) bool {
		switch method {
		case "stave.initialize", "initialize", "stave.initialized", "initialized", "stave.action.cancel", "stave.session.cancel", "stave.session.shutdown", "stave.ping":
			return true
		default:
			return false
		}
	}
	type scannedLine struct {
		line []byte
		err  error
		eof  bool
	}
	scanned := make(chan scannedLine)
	go func() {
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			select {
			case scanned <- scannedLine{line: line}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case scanned <- scannedLine{err: sc.Err(), eof: true}:
		case <-ctx.Done():
		}
	}()
	reading := true
	for reading {
		var scannedResult scannedLine
		select {
		case <-ctx.Done():
			_ = in.Close()
			setWriteErr(ctx.Err())
			reading = false
			continue
		case scannedResult = <-scanned:
			if scannedResult.eof {
				if scannedResult.err != nil {
					setWriteErr(scannedResult.err)
				}
				reading = false
				continue
			}
		}
		r, err := protocol.DecodeLine(scannedResult.line, s.inputLimit())
		if err != nil {
			s.observe(ctx, "protocol.parse_error", map[string]string{"cause": "invalid_jsonrpc"})
			_ = write(protocol.Response{JSONRPC: protocol.JSONRPC, Error: protocol.Errorf(protocol.ParseError, "%v", err)})
			continue
		}
		if control(r.Method) {
			resp := s.handleSafely(ctx, r)
			if len(r.ID) > 0 {
				if err := write(resp); err != nil {
					setWriteErr(err)
					break
				}
			}
			continue
		}
		reserved := false
		if r.Method == "stave.action.invoke" {
			if e := s.reserveCall(ctx, r); e != nil {
				if len(r.ID) > 0 {
					_ = write(protocol.Response{JSONRPC: protocol.JSONRPC, ID: r.ID, Error: protocol.Errorf(protocol.InvalidParams, "%s", e)})
				}
				continue
			}
			reserved = true
		}
		select {
		case jobs <- job{request: r}:
		case <-ctx.Done():
			if reserved {
				s.releaseReservedCall(r)
			}
		case <-writerDone:
			if reserved {
				s.releaseReservedCall(r)
			}
		default:
			if reserved {
				s.releaseReservedCall(r)
			}
			s.observe(ctx, "protocol.backpressure", map[string]string{"queue": "requests"})
			if len(r.ID) > 0 {
				_ = write(protocol.Response{JSONRPC: protocol.JSONRPC, ID: r.ID, Error: protocol.Errorf(protocol.Backpressure, "request queue is full")})
			}
		}
	}
	close(jobs)
	wg.Wait()
	close(writes)
	<-writerDone
	if err := getWriteErr(); err != nil {
		return err
	}
	return nil
}

func (s *Server) outputLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limits.MaxOutputBytes
}

func (s *Server) inputLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limits.MaxMessageBytes
}

func (s *Server) snapshotModeAllowed(mode string) bool {
	if mode == "" {
		mode = "full"
	}
	s.mu.Lock()
	compatibility := s.opt.CompatibilityMode
	resolved := s.negotiated
	s.mu.Unlock()
	if compatibility {
		return true
	}
	manifest, ok := resolved.(capability.Manifest)
	if !ok {
		return false
	}
	for _, offered := range manifest.SnapshotModes {
		if offered == mode {
			return true
		}
	}
	return false
}

func (s *Server) writeBounded(write func([]byte) error, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) <= s.outputLimit() {
		return write(b)
	}
	resp, ok := value.(protocol.Response)
	if !ok {
		return ErrOutputLimit
	}
	fallback := protocol.Response{
		JSONRPC: protocol.JSONRPC,
		ID:      resp.ID,
		Error:   protocol.Errorf(protocol.OutputLimit, "response exceeds output limit"),
	}
	b, err = json.Marshal(fallback)
	if err != nil || len(b) > s.outputLimit() {
		return fmt.Errorf("%w: typed error envelope does not fit", ErrOutputLimit)
	}
	return write(b)
}

func (s *Server) reserveCall(parent context.Context, r protocol.Request) error {
	var p protocol.InvokeParams
	if err := decodeStrict(r.Params, &p); err != nil || p.CallID == "" || p.ActionID == "" {
		return errors.New("invalid invoke params")
	}
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.calls[p.CallID]; exists {
		cancel()
		return errors.New("duplicate call id")
	}
	s.calls[p.CallID] = callSlot{ctx: ctx, cancel: cancel}
	return nil
}

func (s *Server) releaseReservedCall(r protocol.Request) {
	var p protocol.InvokeParams
	if decodeStrict(r.Params, &p) != nil {
		return
	}
	s.finishCall(p.CallID)
}

func (s *Server) callContext(callID string, fallback context.Context) (context.Context, context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot, ok := s.calls[callID]; ok {
		return slot.ctx, slot.cancel
	}
	ctx, cancel := context.WithCancel(fallback)
	s.calls[callID] = callSlot{ctx: ctx, cancel: cancel}
	return ctx, cancel
}

func (s *Server) finishCall(callID string) {
	s.mu.Lock()
	slot, ok := s.calls[callID]
	if ok {
		delete(s.calls, callID)
	}
	s.mu.Unlock()
	if ok {
		slot.cancel()
	}
}

func (s *Server) handle(parent context.Context, r protocol.Request) protocol.Response {
	resp := protocol.Response{JSONRPC: protocol.JSONRPC, ID: r.ID}
	fail := func(code int, msg string) protocol.Response {
		resp.Error = protocol.Errorf(code, "%s", msg)
		return resp
	}
	s.mu.Lock()
	init, ready, cancelling := s.initialized, s.ready, s.cancelling
	s.mu.Unlock()
	switch r.Method {
	case "stave.initialize", "initialize":
		if init {
			return fail(protocol.InvalidRequest, "session is already initialized")
		}
		var p protocol.InitializeParams
		if len(r.Params) > 0 {
			d := json.NewDecoder(bytesReader(r.Params))
			d.DisallowUnknownFields()
			if e := d.Decode(&p); e != nil || decoderTrailing(d) {
				return fail(protocol.InvalidParams, "invalid initialize params")
			}
		}
		if len(p.ProtocolVersions) == 0 && !s.opt.CompatibilityMode {
			return fail(protocol.UnsupportedVersion, "protocol version offer is required")
		}
		if len(p.ProtocolVersions) > 0 {
			ok := false
			for _, v := range p.ProtocolVersions {
				if v == protocol.Version {
					ok = true
				}
			}
			if !ok {
				return fail(protocol.UnsupportedVersion, "no compatible protocol version")
			}
		}
		if s.opt.Negotiate == nil && !s.opt.CompatibilityMode {
			return fail(protocol.CapabilityMismatch, "capability negotiation is required")
		}
		var resolved any
		if s.opt.Negotiate != nil {
			m, e := s.opt.Negotiate(parent, p.Capabilities)
			if e != nil {
				return fail(protocol.CapabilityMismatch, "capability negotiation failed")
			}
			resolved = m.Clone()
		} else {
			resolved = cloneJSONValue(s.opt.Manifest)
		}
		limits, e := intersectLimits(s.limits, p.Limits)
		if e != nil {
			return fail(protocol.InvalidParams, "invalid client limits")
		}
		s.mu.Lock()
		s.initialized = true
		s.negotiated = resolved
		s.limits = limits
		s.mu.Unlock()
		resp.Result = protocol.InitializeResult{ProtocolVersion: protocol.Version, SessionID: s.sessionID, Server: s.opt.ServerName, Application: s.opt.Application, Schemas: map[string]string{"semantic": "stave.semantic/v1", "actions": "stave.actions/v1", "protocol": "stave.protocol/v1"}, Capabilities: resolved, Limits: limits, ResolvedManifest: resolved}
		return resp
	case "stave.initialized", "initialized":
		if !init {
			return fail(protocol.NotInitialized, "initialize required")
		}
		s.mu.Lock()
		s.ready = true
		s.mu.Unlock()
		resp.Result = map[string]any{"ok": true, "sessionId": s.sessionID}
		return resp
	case "stave.ping":
		resp.Result = map[string]any{"ok": true, "protocolVersion": protocol.Version}
		return resp
	case "stave.session.shutdown":
		s.Close()
		resp.Result = map[string]any{"ok": true}
		return resp
	}
	if !init || !ready {
		return fail(protocol.NotInitialized, "session is not initialized")
	}
	if cancelling {
		return fail(protocol.Cancelled, "session is cancelling")
	}
	switch r.Method {
	case "stave.snapshot":
		if s.opt.SnapshotEnvelope == nil {
			return fail(protocol.InternalError, "snapshot unavailable")
		}
		var p protocol.SnapshotParams
		if len(r.Params) > 0 {
			d := json.NewDecoder(bytesReader(r.Params))
			d.DisallowUnknownFields()
			if e := d.Decode(&p); e != nil || decoderTrailing(d) {
				return fail(protocol.InvalidParams, "invalid snapshot params")
			}
		}
		if !s.snapshotModeAllowed(p.Mode) {
			return fail(protocol.CapabilityMismatch, "snapshot mode was not negotiated")
		}
		env, e := s.opt.SnapshotEnvelope(parent, p.Mode, p.SinceRevision)
		if e != nil {
			s.observe(parent, "snapshot.failed", map[string]string{"cause": "provider"})
			return fail(protocol.InternalError, "snapshot failed")
		}
		if env.Mode == "" {
			env.Mode = p.Mode
		}
		if env.Mode == "" {
			env.Mode = "full"
		}
		if env.Mode != "full" && env.Mode != "patch" {
			return fail(protocol.InvalidParams, "unsupported snapshot mode")
		}
		if p.Mode != "" && p.Mode != env.Mode {
			return fail(protocol.InvalidParams, "snapshot mode mismatch")
		}
		if env.SessionID != s.sessionID {
			return fail(protocol.InternalError, "snapshot session mismatch")
		}
		if env.SessionID == "" || !validHash(env.TreeHash) || !validHash(env.CapabilityHash) || env.SemanticVersion == "" || !validHash(env.ConfigHash) || !validHash(env.ThemeHash) || env.WidthVersion == "" {
			return fail(protocol.InternalError, "incomplete snapshot metadata")
		}
		if env.Revision == 0 || env.Sequence == 0 || (env.Mode == "full" && (env.Snapshot == nil || env.Patch != nil)) || (env.Mode == "patch" && (env.Patch == nil || env.Snapshot != nil || p.SinceRevision == 0)) {
			return fail(protocol.InternalError, "invalid snapshot envelope")
		}
		if env.Snapshot != nil && (env.Snapshot.Validate() != nil || env.Snapshot.Revision != env.Revision || env.Snapshot.TreeHash != env.TreeHash || countSnapshotNodes(env.Snapshot.Root, s.limits.MaxTreeNodes) < 0) {
			return fail(protocol.InternalError, "invalid full snapshot")
		}
		if env.Patch != nil && (env.Patch.Validate() != nil || env.Patch.FromRevision != p.SinceRevision || env.Patch.ToRevision != env.Revision) {
			return fail(protocol.InternalError, "invalid snapshot patch")
		}
		for _, d := range env.Actions {
			if err := d.Validate(); err != nil {
				return fail(protocol.InternalError, "invalid snapshot action manifest")
			}
		}
		actions := env.Actions
		if !p.IncludeActions {
			actions = nil
		}
		diagnostics := append([]diag.Diagnostic(nil), env.Diagnostics...)
		for i := range diagnostics {
			diagnostics[i].Redacted = true
		}
		resp.Result = protocol.SnapshotResult{SchemaVersion: "stave.semantic/v1", SessionID: env.SessionID, Sequence: env.Sequence, Mode: env.Mode, Snapshot: env.Snapshot, Patch: env.Patch, Revision: env.Revision, TreeHash: env.TreeHash, CapabilityHash: env.CapabilityHash, SemanticVersion: env.SemanticVersion, ConfigHash: env.ConfigHash, ThemeHash: env.ThemeHash, WidthVersion: env.WidthVersion, Actions: actions, Diagnostics: diagnostics}
		return resp
	case "stave.actions.list":
		if s.opt.Actions == nil {
			resp.Result = []action.Definition{}
			return resp
		}
		resp.Result = s.opt.Actions.Manifest()
		return resp
	case "stave.action.invoke":
		return s.invoke(parent, r, resp, fail)
	case "stave.action.confirm":
		return s.confirm(parent, r, resp, fail)
	case "stave.action.cancel":
		var p struct {
			CallID string `json:"callId"`
		}
		if decodeStrict(r.Params, &p) != nil || p.CallID == "" {
			return fail(protocol.InvalidParams, "invalid call id")
		}
		s.mu.Lock()
		c, ok := s.calls[p.CallID]
		s.mu.Unlock()
		if !ok {
			return fail(protocol.InvalidParams, "unknown call id")
		}
		c.cancel()
		resp.Result = map[string]any{"callId": p.CallID, "cancelled": true}
		return resp
	case "stave.session.cancel":
		if len(r.Params) > 0 && string(r.Params) != "null" && string(r.Params) != "{}" {
			var empty struct{}
			if decodeStrict(r.Params, &empty) != nil {
				return fail(protocol.InvalidParams, "session cancel takes no parameters")
			}
		}
		s.mu.Lock()
		s.cancelling = true
		calls := make([]context.CancelFunc, 0, len(s.calls))
		for _, slot := range s.calls {
			calls = append(calls, slot.cancel)
		}
		s.mu.Unlock()
		for _, cancel := range calls {
			cancel()
		}
		if s.opt.CancelSession != nil {
			if err := s.opt.CancelSession(parent); err != nil {
				s.observe(parent, "session.cancel_failed", map[string]string{"cause": "application"})
				return fail(protocol.InternalError, "session cancellation failed")
			}
		}
		resp.Result = map[string]any{"cancelled": true, "sessionId": s.sessionID}
		return resp
	default:
		return fail(protocol.MethodNotFound, "method not found")
	}
}

func (s *Server) handleSafely(parent context.Context, r protocol.Request) (resp protocol.Response) {
	defer func() {
		if recover() != nil {
			s.observe(parent, "runtime.callback_panic", map[string]string{"method": safeIdentifier(r.Method)})
			resp = protocol.Response{JSONRPC: protocol.JSONRPC, ID: r.ID, Error: protocol.Errorf(protocol.InternalError, "internal callback failure")}
			if r.Method == "stave.action.invoke" {
				s.releaseReservedCall(r)
			}
		}
	}()
	return s.handle(parent, r)
}

func (s *Server) confirm(ctx context.Context, r protocol.Request, resp protocol.Response, fail func(int, string) protocol.Response) protocol.Response {
	if s.opt.Confirm == nil {
		return fail(protocol.Forbidden, "confirmation authority is not configured")
	}
	var p protocol.ConfirmParams
	if decodeStrict(r.Params, &p) != nil || p.ActionID == "" {
		return fail(protocol.InvalidParams, "invalid confirmation params")
	}
	if p.SessionID != "" && p.SessionID != s.sessionID {
		return fail(protocol.InvalidParams, "wrong session")
	}
	var target semantic.Target
	if len(p.Target) > 0 {
		if e := decodeStrictTarget(p.Target, &target); e != nil {
			return fail(protocol.InvalidParams, "invalid target")
		}
	}
	args := p.Arguments
	if len(args) == 0 {
		args = json.RawMessage("null")
	}
	call := action.Call{CallID: p.CallID, ActionID: action.ID(p.ActionID), Target: target, Arguments: args, SessionID: s.sessionID}
	if s.opt.Authorize == nil && s.opt.AuthorizePrepared == nil {
		return fail(protocol.Forbidden, "action authority is not configured")
	}
	var ae *action.Error
	if s.opt.AuthorizePrepared != nil {
		call, ae = s.opt.AuthorizePrepared(ctx, call)
	} else if s.opt.Authorize != nil {
		ae = s.opt.Authorize(ctx, call)
	}
	if ae != nil {
		return actionErrorResponse(r.ID, ae)
	}
	c, e := s.opt.Confirm(ctx, call)
	if e != nil {
		s.observe(ctx, "confirmation.failed", map[string]string{"cause": "issuer"})
		return fail(protocol.ConfirmationInvalid, "confirmation could not be issued")
	}
	if c.SessionID == "" {
		c.SessionID = s.sessionID
	}
	// Bind the issued grant to the authority's prepared policy, rather than
	// trusting a confirmation issuer to repeat policy metadata correctly.
	c.PolicyID, c.PolicyEpoch = call.PolicyID, call.PolicyEpoch
	if s.opt.Actions != nil {
		if e := s.opt.Actions.IssueConfirmation(c); e != nil {
			if errors.Is(e, action.ErrConfirmationLimit) {
				return fail(protocol.ResourceLimit, "confirmation capacity reached")
			}
			return fail(protocol.ConfirmationInvalid, "confirmation could not be registered")
		}
	}
	resp.Result = protocol.ConfirmationPresentation{Token: c.Token, SessionID: c.SessionID}
	return resp
}

func (s *Server) invoke(parent context.Context, r protocol.Request, resp protocol.Response, fail func(int, string) protocol.Response) protocol.Response {
	var p protocol.InvokeParams
	if decodeStrict(r.Params, &p) != nil || p.ActionID == "" || p.CallID == "" {
		return fail(protocol.InvalidParams, "invalid invoke params")
	}
	defer s.finishCall(p.CallID)
	cctx, cancel := s.callContext(p.CallID, parent)
	defer cancel()
	if p.DeadlineMS < 0 {
		return fail(protocol.InvalidParams, "invalid deadline")
	}
	if p.DeadlineMS > 0 {
		var dc context.CancelFunc
		cctx, dc = context.WithTimeout(cctx, time.Duration(p.DeadlineMS)*time.Millisecond)
		defer dc()
	}
	if p.SessionID != "" && p.SessionID != s.sessionID {
		return fail(protocol.InvalidParams, "wrong session")
	}
	var target semantic.Target
	if len(p.Target) > 0 {
		if e := decodeStrictTarget(p.Target, &target); e != nil {
			return fail(protocol.InvalidParams, "invalid target")
		}
	}
	args := p.Arguments
	if len(args) == 0 {
		args = json.RawMessage("null")
	}
	call := action.Call{CallID: p.CallID, ActionID: action.ID(p.ActionID), Target: target, Arguments: args, SessionID: s.sessionID}
	if deadline, ok := cctx.Deadline(); ok {
		call.Deadline = deadline
	}
	if p.Confirmation != nil {
		var c action.Confirmation
		c.Token = p.Confirmation.Token
		c.SessionID = p.Confirmation.SessionID
		call.Confirmation = &c
	}
	if s.opt.Authorize == nil && s.opt.AuthorizePrepared == nil {
		return fail(protocol.Forbidden, "action authority is not configured")
	}
	var ae *action.Error
	if s.opt.AuthorizePrepared != nil {
		call, ae = s.opt.AuthorizePrepared(cctx, call)
	} else if s.opt.Authorize != nil {
		ae = s.opt.Authorize(cctx, call)
	}
	if ae != nil {
		return actionErrorResponse(r.ID, ae)
	}
	var result action.Result
	if s.opt.Actions != nil {
		result = s.opt.Actions.Invoke(cctx, call)
	} else {
		if s.opt.Invoke != nil {
			return fail(protocol.MethodNotFound, "custom invoke handler is unsupported; register actions instead")
		}
		return fail(protocol.MethodNotFound, "action invocation unavailable")
	}
	if result.Error != nil {
		return actionErrorResponse(r.ID, result.Error)
	}
	if result.Status == "" {
		result.Status = action.ResultOK
	}
	resp.Result = protocol.InvokeResult{CallID: result.CallID, ActionID: result.ActionID, Target: result.Target, Status: result.Status, Output: append(json.RawMessage(nil), result.Output...), Revision: result.Revision, TreeHash: result.TreeHash, Diagnostics: append([]diag.Diagnostic(nil), result.Diagnostics...)}
	return resp
}

func actionErrorResponse(id json.RawMessage, ae *action.Error) protocol.Response {
	if ae == nil {
		return protocol.Response{JSONRPC: protocol.JSONRPC, ID: id, Error: protocol.Errorf(protocol.InternalError, "internal action failure")}
	}
	data := protocol.ActionErrorData{StaveCode: ae.Code, ActionID: ae.ActionID, NodeID: ae.NodeID, Revision: ae.Revision, Retryable: ae.Retryable, CauseClass: safeIdentifier(ae.CauseClass)}
	return protocol.Response{JSONRPC: protocol.JSONRPC, ID: id, Error: &protocol.Error{Code: actionCode(ae.Code), Message: safeActionMessage(ae.Code), Data: data}}
}

func safeActionMessage(code action.Code) string {
	switch code {
	case action.InvalidRequest, action.InvalidArgument:
		return "invalid action request"
	case action.ActionNotFound:
		return "action not found"
	case action.NodeNotFound:
		return "target node not found"
	case action.NodeReplaced, action.StaleSnapshot:
		return "stale action target"
	case action.AmbiguousTarget:
		return "ambiguous action target"
	case action.CapabilityMismatch:
		return "capability mismatch"
	case action.ConfirmationRequired:
		return "confirmation required"
	case action.ConfirmationInvalid:
		return "confirmation invalid"
	case action.Forbidden:
		return "action forbidden"
	case action.Conflict:
		return "action conflict"
	case action.DeadlineExceeded:
		return "action deadline exceeded"
	case action.Cancelled:
		return "action cancelled"
	case action.ResourceLimit:
		return "action resource limit exceeded"
	case action.OutputSchemaViolation:
		return "action output schema violation"
	default:
		return "internal action failure"
	}
}
func actionCode(c action.Code) int {
	switch c {
	case action.InvalidRequest:
		return protocol.InvalidRequest
	case action.ActionNotFound:
		return protocol.MethodNotFound
	case action.InvalidArgument:
		return protocol.InvalidParams
	case action.ConfirmationRequired:
		return protocol.ConfirmationRequired
	case action.ConfirmationInvalid:
		return protocol.ConfirmationInvalid
	case action.NodeReplaced, action.StaleSnapshot:
		return protocol.StaleTarget
	case action.NodeNotFound:
		return protocol.StaleTarget
	case action.AmbiguousTarget, action.Conflict:
		return protocol.InvalidRequest
	case action.Forbidden:
		return protocol.Forbidden
	case action.OutputSchemaViolation:
		return protocol.SchemaViolation
	case action.Internal:
		return protocol.InternalError
	case action.CapabilityMismatch:
		return protocol.CapabilityMismatch
	case action.DeadlineExceeded:
		return protocol.DeadlineExceeded
	case action.Cancelled:
		return protocol.Cancelled
	case action.ResourceLimit:
		return protocol.ResourceLimit
	default:
		return protocol.InternalError
	}
}

func (s *Server) Notify(n protocol.Notification) error {
	b, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if len(b) > s.outputLimit() {
		return ErrOutputLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("server closed")
	}
	select {
	case s.notify <- n:
		return nil
	default:
		s.observe(context.Background(), "protocol.backpressure", map[string]string{"queue": "full"})
		return fmt.Errorf("%w: notification queue full", errors.New("BACKPRESSURE"))
	}
}

func (s *Server) observe(ctx context.Context, name string, attrs map[string]string) {
	s.opt.Observer.Observe(ctx, observer.Event{Name: name, Time: time.Now().UTC(), Attributes: attrs, Redacted: true})
}
func (s *Server) Notifications() <-chan protocol.Notification { return s.notify }
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for _, c := range s.calls {
		c.cancel()
	}
	s.calls = map[string]callSlot{}
	close(s.notify)
	s.mu.Unlock()
}
func bytesReader(b []byte) *bytes.Buffer { return bytes.NewBuffer(b) }

func decoderTrailing(d *json.Decoder) bool { var extra any; return d.Decode(&extra) != io.EOF }
func decodeStrictTarget(raw []byte, dst *semantic.Target) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*dst = semantic.Target{}
		return nil
	}
	if err := decodeStrict(raw, dst); err != nil {
		return err
	}
	// A non-null target is an authority-bound reference. Require all identity
	// components so malformed or ambiguous targets cannot reach authorization
	// or confirmation code paths. Targetless calls must use null/empty JSON.
	if !dst.NodeID.Valid() {
		return errors.New("invalid target node ID")
	}
	if dst.Generation == 0 {
		return errors.New("invalid target generation")
	}
	if dst.ObservedRevision == 0 {
		return errors.New("invalid target observed revision")
	}
	return nil
}
func decodeStrict(raw []byte, dst any) error {
	d := json.NewDecoder(bytesReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if decoderTrailing(d) {
		return errors.New("trailing JSON")
	}
	return nil
}
func intersectLimits(server, client protocol.Limits) (protocol.Limits, error) {
	if client.MaxMessageBytes < 0 || client.MaxOutputBytes < 0 || client.MaxTreeNodes < 0 {
		return protocol.Limits{}, errors.New("negative limit")
	}
	out := server
	if client.MaxMessageBytes > 0 && client.MaxMessageBytes < out.MaxMessageBytes {
		out.MaxMessageBytes = client.MaxMessageBytes
	}
	if client.MaxOutputBytes > 0 && client.MaxOutputBytes < out.MaxOutputBytes {
		out.MaxOutputBytes = client.MaxOutputBytes
	}
	if client.MaxTreeNodes > 0 && client.MaxTreeNodes < out.MaxTreeNodes {
		out.MaxTreeNodes = client.MaxTreeNodes
	}
	if out.MaxOutputBytes < minimumOutputBytes {
		return protocol.Limits{}, ErrOutputLimit
	}
	return out, nil
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func cloneJSONValue(value any) any {
	if value == nil {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		return nil
	}
	return out
}

func countSnapshotNodes(root semantic.Node, limit int) int {
	if limit <= 0 {
		return -1
	}
	stack := []semantic.Node{root}
	count := 0
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if count > limit {
			return -1
		}
		stack = append(stack, n.Children()...)
	}
	return count
}

func safeIdentifier(value string) string {
	if len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return value
}
