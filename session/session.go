package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/replay"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
)

var (
	ErrSessionClosed = errors.New("session closed")
	ErrBackpressure  = errors.New("BACKPRESSURE")
	ErrViewRequired  = errors.New("session view is required")
	ErrReducerNil    = errors.New("session reducer is required")
)

type Lifecycle string

const (
	LifecycleStarting   Lifecycle = "starting"
	LifecycleRunning    Lifecycle = "running"
	LifecycleCancelling Lifecycle = "cancelling"
	LifecycleClosed     Lifecycle = "closed"
)

type Reducer[M any] func(context.Context, M, event.Event) (M, []effect.Request, error)

type ViewResult struct {
	Tree        semantic.Tree
	SurfaceHash string
	Diagnostics []Diagnostic
}

type View[M any] func(context.Context, M) (ViewResult, error)

type Diagnostic struct {
	Code        string
	Message     string
	SafeContext map[string]string
}

type Options[M any] struct {
	SessionID         string
	Initial           M
	Reduce            Reducer[M]
	View              View[M]
	QueueCapacity     int
	ReduceTimeout     time.Duration
	ViewTimeout       time.Duration
	EffectParallelism int
	EffectDelivery    effect.Delivery
	EffectPorts       map[string]effect.Port
	MaxActiveBatches  int
	ModelPolicy       state.ModelPolicy[M]
	Capabilities      capability.Manifest
	ConfigHash        string
	ThemeHash         string
	WidthPolicy       string
	Versions          state.Versions
}

type Session[M any] struct {
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	reduce           Reducer[M]
	view             View[M]
	reduceTimeout    time.Duration
	viewTimeout      time.Duration
	modelPolicy      state.ModelPolicy[M]
	queue            *eventQueue
	effectAdmissions *effectAdmissionQueue
	effects          *effect.Executor
	effectDelivery   effect.Delivery
	lifecycle        Lifecycle
	current          state.State[M]
	diagnosticCount  uint64
	diagnostics      []Diagnostic
	transcript       replay.Transcript
	closeOnce        sync.Once
	loopDone         chan struct{}
}

func New[M any](ctx context.Context, opts Options[M]) (*Session[M], error) {
	if opts.Reduce == nil {
		return nil, ErrReducerNil
	}
	if opts.View == nil {
		return nil, ErrViewRequired
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = 256
	}
	if opts.EffectParallelism <= 0 {
		opts.EffectParallelism = 1
	}
	if opts.EffectDelivery == "" {
		opts.EffectDelivery = effect.DeclarationOrder
	}
	if opts.SessionID == "" {
		opts.SessionID = "stave-session"
	}
	sessionCtx, cancel := context.WithCancel(ctx)

	s := &Session[M]{
		ctx:              sessionCtx,
		cancel:           cancel,
		reduce:           opts.Reduce,
		view:             opts.View,
		reduceTimeout:    opts.ReduceTimeout,
		viewTimeout:      opts.ViewTimeout,
		modelPolicy:      opts.ModelPolicy,
		queue:            newEventQueue(opts.QueueCapacity),
		effectAdmissions: newEffectAdmissionQueue(opts.QueueCapacity),
		effects:          effect.NewExecutor(effect.Options{Ports: opts.EffectPorts, Parallelism: opts.EffectParallelism, Delivery: opts.EffectDelivery, MaxActiveBatches: opts.MaxActiveBatches}),
		effectDelivery:   opts.EffectDelivery,
		lifecycle:        LifecycleStarting,
		loopDone:         make(chan struct{}),
	}

	initialState, viewDiagnostics, err := s.renderState(opts.SessionID, opts.Initial, 0, 0, opts)
	if err != nil {
		cancel()
		return nil, err
	}
	for _, diagnostic := range viewDiagnostics {
		s.addDiagnosticLocked(diagnostic)
	}
	initialState.DiagnosticCount = s.diagnosticCount
	checkpoint, err := initialState.Checkpoint(s.modelPolicy)
	if err != nil {
		cancel()
		return nil, err
	}

	s.current = initialState
	s.lifecycle = LifecycleRunning
	s.transcript = replay.NewTranscript(initialState.SessionID, initialState.Versions, checkpoint)

	go s.loop()
	go s.admitEffects()
	return s, nil
}

func (s *Session[M]) Send(ev event.Event) error {
	// Clone at the ownership boundary. Callers may reuse or mutate payload
	// slices/maps immediately after Send returns.
	frozen, err := ev.Clone()
	if err != nil {
		return err
	}
	overflowEpisode, err := s.queue.push(frozen)
	if overflowEpisode {
		s.addTransientDiagnostic("BACKPRESSURE", "input queue saturated", map[string]string{"kind": string(frozen.Kind)})
	}
	return err
}

func (s *Session[M]) Snapshot() (state.State[M], error) {
	s.mu.RLock()
	current := s.current
	s.mu.RUnlock()
	return current.Clone(s.modelPolicy)
}

func (s *Session[M]) Checkpoint() (state.Checkpoint, error) {
	s.mu.RLock()
	current := s.current
	s.mu.RUnlock()
	return current.Checkpoint(s.modelPolicy)
}

func (s *Session[M]) Transcript() (replay.Transcript, error) {
	s.mu.RLock()
	transcript := s.transcript
	s.mu.RUnlock()
	return transcript.Clone()
}

func (s *Session[M]) Diagnostics() []Diagnostic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Diagnostic, 0, len(s.diagnostics))
	for _, diagnostic := range s.diagnostics {
		out = append(out, cloneDiagnostic(diagnostic))
	}
	return out
}

func (s *Session[M]) Lifecycle() Lifecycle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lifecycle
}

func (s *Session[M]) Wait(ctx context.Context, predicate func(state.State[M]) bool) error {
	for {
		snapshot, err := s.Snapshot()
		if err != nil {
			return err
		}
		if predicate(snapshot) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *Session[M]) Cancel() {
	s.Close()
}

func (s *Session[M]) Close() {
	s.beginClose()
	<-s.loopDone
}

func (s *Session[M]) loop() {
	defer close(s.loopDone)
	defer func() {
		s.mu.Lock()
		s.lifecycle = LifecycleClosed
		s.mu.Unlock()
	}()
	for {
		ev, ok := s.queue.pop(s.ctx)
		if !ok {
			return
		}
		s.handleEventSafely(ev)
	}
}

func (s *Session[M]) handleEventSafely(ev event.Event) {
	defer func() {
		if recover() != nil {
			s.mu.RLock()
			current := s.current
			s.mu.RUnlock()
			s.rejectEvent(current, ev.WithAccepted(current.Sequence+1, current.Revision), "INTERNAL_PANIC", "session callback failed", nil)
		}
	}()
	s.handleEvent(ev)
}

func (s *Session[M]) handleEvent(raw event.Event) {
	s.mu.RLock()
	current := s.current
	s.mu.RUnlock()

	reducerInput, err := s.modelPolicyClone(current.Model)
	if err != nil {
		s.rejectEvent(current, raw.WithAccepted(current.Sequence+1, current.Revision), "MODEL_CLONE_FAILED", "model clone failed", nil)
		return
	}

	accepted := raw.WithAccepted(current.Sequence+1, current.Revision)
	reduceCtx, reduceCancel := withOptionalTimeout(s.ctx, s.reduceTimeout)
	nextModel, requests, reduceErr := s.reduce(reduceCtx, reducerInput, accepted)
	reduceCancel()
	if reduceErr != nil {
		code := "REDUCE_FAILED"
		if errors.Is(reduceErr, context.DeadlineExceeded) {
			code = "REDUCE_TIMEOUT"
		} else if errors.Is(reduceErr, context.Canceled) {
			code = "REDUCE_CANCELLED"
		}
		s.rejectEvent(current, accepted, code, "", nil)
		return
	}

	rendered, viewDiagnostics, err := s.renderState(current.SessionID, nextModel, accepted.Sequence, current.Revision, Options[M]{
		ConfigHash:   current.ConfigHash,
		ThemeHash:    current.ThemeHash,
		WidthPolicy:  current.WidthPolicy,
		Versions:     current.Versions,
		Capabilities: current.Capabilities,
	})
	if err != nil {
		code := "VIEW_FAILED"
		if errors.Is(err, context.DeadlineExceeded) {
			code = "VIEW_TIMEOUT"
		} else if errors.Is(err, context.Canceled) {
			code = "VIEW_CANCELLED"
		}
		s.rejectEvent(current, accepted, code, "", nil)
		return
	}

	for _, diagnostic := range viewDiagnostics {
		s.addDiagnostic(diagnostic.Code, diagnostic.Message, diagnostic.SafeContext)
	}
	if rendered.Hashes.Model != current.Hashes.Model || rendered.Hashes.Tree != current.Hashes.Tree || rendered.Hashes.Surface != current.Hashes.Surface {
		rendered, err = s.revisionState(rendered, current.Revision+1)
		if err != nil {
			s.rejectEvent(current, accepted, "REVISION_FAILED", "semantic revision failed", nil)
			return
		}
	}
	rendered.Sequence = accepted.Sequence
	rendered.DiagnosticCount = s.diagnosticCount
	accepted.Revision = rendered.Revision

	calls, declarations, bindErr := s.bindEffects(rendered, requests)
	if bindErr != nil {
		s.rejectEvent(current, accepted, "EFFECT_BIND_FAILED", "effect declaration invalid", nil)
		return
	}
	rendered.Hashes.Declarations = declarations
	if accepted.Kind == event.EffectResult {
		ledger, hashErr := state.HashString(struct {
			Prior string      `json:"prior,omitempty"`
			Event event.Event `json:"event"`
		}{current.Hashes.EffectLedger, accepted})
		if hashErr != nil {
			s.rejectEvent(current, accepted, "EFFECT_LEDGER_FAILED", "effect ledger failed", nil)
			return
		}
		rendered.Hashes.EffectLedger = ledger
	} else {
		rendered.Hashes.EffectLedger = current.Hashes.EffectLedger
	}

	if err := s.admitEffectCalls(calls); err != nil {
		s.rejectEvent(current, accepted, "EFFECT_ADMISSION_BACKPRESSURE", "pending effect admission queue saturated", nil)
		return
	}
	s.publish(current, rendered, accepted)

	if raw.Kind == event.Cancel || raw.Kind == event.Shutdown {
		s.beginClose()
	}
}

func (s *Session[M]) rejectEvent(current state.State[M], ev event.Event, code, message string, safe map[string]string) {
	count := s.addDiagnostic(code, message, safe)
	rejected, err := state.New(current.SessionID, current.Model, current.Tree, state.Meta{
		Sequence:        ev.Sequence,
		Revision:        current.Revision,
		Capabilities:    current.Capabilities,
		ConfigHash:      current.ConfigHash,
		ThemeHash:       current.ThemeHash,
		SurfaceHash:     current.SurfaceHash,
		WidthPolicy:     current.WidthPolicy,
		Versions:        current.Versions,
		DiagnosticCount: count,
		EffectLedger:    current.Hashes.EffectLedger,
		Declarations:    current.Hashes.Declarations,
	}, s.modelPolicy)
	if err != nil {
		return
	}
	rejected.DiagnosticCount = count
	s.publish(current, rejected, ev)
}

func (s *Session[M]) publish(prior, next state.State[M], accepted event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = next
	record := replay.Record{
		SchemaVersion: accepted.SchemaVersion,
		Event:         accepted,
		Prior:         replay.DigestFromState(prior),
		Result:        replay.DigestFromState(next),
	}
	if accepted.Kind == event.EffectResult {
		record.Delivery = string(s.effectDelivery)
		record.CompletionIndex = accepted.Meta.CompletionIndex
	}
	record.SchemaVersion = replay.SchemaVersion
	s.transcript.Append(record)
}

func (s *Session[M]) bindEffects(snapshot state.State[M], requests []effect.Request) ([]effect.Call, string, error) {
	if len(requests) == 0 {
		return nil, snapshot.Hashes.Declarations, nil
	}
	calls, err := effect.Bind(snapshot.SessionID, snapshot.Sequence, snapshot.Revision, requests)
	if err != nil {
		return nil, "", err
	}
	hash, err := state.HashString(struct {
		Prior string        `json:"prior,omitempty"`
		Calls []effect.Call `json:"calls"`
	}{snapshot.Hashes.Declarations, calls})
	if err != nil {
		return nil, "", err
	}
	return calls, hash, nil
}

func (s *Session[M]) admitEffectCalls(calls []effect.Call) error {
	if len(calls) == 0 {
		return nil
	}
	return s.effectAdmissions.push(calls)
}

// admitEffects owns the only pending admission retry loop. A published
// declaration has already reserved capacity in effectAdmissions, so it stays
// durable until admitted or session shutdown cancels the pending queue.
func (s *Session[M]) admitEffects() {
	for {
		calls, ok := s.effectAdmissions.front(s.ctx)
		if !ok {
			return
		}
		for {
			err := s.effects.Deliver(s.ctx, calls, s.enqueueInternalEvent)
			if err == nil {
				s.effectAdmissions.shift()
				break
			}
			if errors.Is(err, effect.ErrBackpressure) {
				select {
				case <-s.ctx.Done():
					return
				case <-time.After(time.Millisecond):
				}
				continue
			}
			if !errors.Is(err, effect.ErrClosed) || s.ctx.Err() == nil {
				s.addTransientDiagnostic("EFFECT_DELIVER_FAILED", "effect delivery failed", nil)
			}
			s.effectAdmissions.shift()
			break
		}
	}
}

func (s *Session[M]) enqueueInternalEvent(ev event.Event) error {
	for {
		overflowEpisode, err := s.queue.push(ev)
		if overflowEpisode {
			s.addTransientDiagnostic("BACKPRESSURE", "internal queue saturated", map[string]string{"kind": string(ev.Kind)})
		}
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrSessionClosed) || errors.Is(s.ctx.Err(), context.Canceled) {
			s.addTransientDiagnostic("LATE_EFFECT_RESULT", "late effect result discarded", safeEffectContext(ev))
			return ErrSessionClosed
		}
		if !errors.Is(err, ErrBackpressure) {
			return err
		}
		select {
		case <-s.ctx.Done():
			s.addTransientDiagnostic("LATE_EFFECT_RESULT", "late effect result discarded", safeEffectContext(ev))
			return ErrSessionClosed
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *Session[M]) renderState(sessionID string, model M, sequence, revision uint64, opts Options[M]) (state.State[M], []Diagnostic, error) {
	viewModel, err := s.modelPolicyClone(model)
	if err != nil {
		return state.State[M]{}, nil, err
	}
	viewCtx, viewCancel := withOptionalTimeout(s.ctx, s.viewTimeout)
	viewResult, err := s.view(viewCtx, viewModel)
	viewCancel()
	if err != nil {
		return state.State[M]{}, nil, err
	}
	if viewResult.Tree.SchemaVersion() == "" || viewResult.Tree.Hash() == "" {
		return state.State[M]{}, nil, errors.New("view returned invalid tree")
	}
	if revision == 0 {
		revision = viewResult.Tree.Revision()
		if revision == 0 {
			revision = 1
		}
	}
	tree, err := viewResult.Tree.WithRevision(revision)
	if err != nil {
		return state.State[M]{}, nil, err
	}
	st, err := state.New(sessionID, model, tree, state.Meta{
		Sequence:     sequence,
		Revision:     revision,
		Capabilities: opts.Capabilities,
		ConfigHash:   opts.ConfigHash,
		ThemeHash:    opts.ThemeHash,
		SurfaceHash:  viewResult.SurfaceHash,
		WidthPolicy:  opts.WidthPolicy,
		Versions:     opts.Versions,
	}, s.modelPolicy)
	if err != nil {
		return state.State[M]{}, nil, err
	}
	return st, cloneDiagnostics(viewResult.Diagnostics), nil
}

func (s *Session[M]) revisionState(current state.State[M], revision uint64) (state.State[M], error) {
	tree, err := current.Tree.WithRevision(revision)
	if err != nil {
		return state.State[M]{}, err
	}
	return state.New(current.SessionID, current.Model, tree, state.Meta{
		Sequence: current.Sequence, Revision: revision, Capabilities: current.Capabilities,
		ConfigHash: current.ConfigHash, ThemeHash: current.ThemeHash, SurfaceHash: current.SurfaceHash,
		WidthPolicy: current.WidthPolicy, Versions: current.Versions, DiagnosticCount: current.DiagnosticCount,
		EffectLedger: current.Hashes.EffectLedger, Declarations: current.Hashes.Declarations,
	}, s.modelPolicy)
}

func (s *Session[M]) addDiagnostic(code, message string, safe map[string]string) uint64 {
	diagnostic := sanitizeDiagnostic(Diagnostic{Code: code, Message: message, SafeContext: safe})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addDiagnosticLocked(diagnostic)
}

func (s *Session[M]) addDiagnosticLocked(diagnostic Diagnostic) uint64 {
	diagnostic = sanitizeDiagnostic(diagnostic)
	s.diagnosticCount++
	s.diagnostics = append(s.diagnostics, cloneDiagnostic(diagnostic))
	return s.diagnosticCount
}

func (s *Session[M]) addTransientDiagnostic(code, message string, safe map[string]string) {
	s.mu.Lock()
	s.diagnostics = append(s.diagnostics, sanitizeDiagnostic(Diagnostic{Code: code, Message: message, SafeContext: safe}))
	s.mu.Unlock()
}

func (s *Session[M]) modelPolicyClone(model M) (M, error) {
	if s.modelPolicy.Clone != nil {
		return s.modelPolicy.Clone(model)
	}
	return state.Clone(model)
}

func safeEffectContext(ev event.Event) map[string]string {
	payload, ok := ev.Payload.(event.EffectResultPayload)
	if !ok {
		return nil
	}
	return map[string]string{
		"callId": payload.CallID,
		"status": payload.Status,
	}
}

func cloneDiagnostic(diagnostic Diagnostic) Diagnostic {
	diagnostic.SafeContext = cloneMap(diagnostic.SafeContext)
	return diagnostic
}

func sanitizeDiagnostic(diagnostic Diagnostic) Diagnostic {
	// Diagnostics cross application and agent boundaries. Keep messages stable
	// and bounded; never expose arbitrary reducer/view/port error text.
	diagnostic.Message = "operation failed"
	input := diagnostic.SafeContext
	diagnostic.SafeContext = nil
	if len(input) == 0 {
		return diagnostic
	}
	// Safe context is intentionally allow-listed to identifiers/statuses only.
	ctx := make(map[string]string)
	for key, value := range input {
		if key != "kind" && key != "callId" && key != "status" && key != "source" {
			continue
		}
		if len(value) > 128 {
			value = value[:128]
		}
		value = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, value)
		ctx[key] = value
	}
	diagnostic.SafeContext = ctx
	return diagnostic
}

func cloneDiagnostics(in []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(in))
	for _, diagnostic := range in {
		out = append(out, cloneDiagnostic(diagnostic))
	}
	return out
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func withOptionalTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

type eventQueue struct {
	mu          sync.Mutex
	capacity    int
	items       []event.Event
	notify      chan struct{}
	closed      bool
	overflowing bool
}

// effectAdmissionQueue bounds batches that have been accepted by the session
// but are waiting for executor capacity. A full queue rejects the producing
// event before its declaration is published, preserving declaration durability.
type effectAdmissionQueue struct {
	mu       sync.Mutex
	capacity int
	items    [][]effect.Call
	notify   chan struct{}
	closed   bool
}

func newEffectAdmissionQueue(capacity int) *effectAdmissionQueue {
	return &effectAdmissionQueue{
		capacity: capacity,
		notify:   make(chan struct{}, 1),
	}
}

func (q *effectAdmissionQueue) push(calls []effect.Call) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrSessionClosed
	}
	if len(q.items) >= q.capacity {
		return ErrBackpressure
	}
	q.items = append(q.items, calls)
	q.signal()
	return nil
}

func (q *effectAdmissionQueue) front(ctx context.Context) ([]effect.Call, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			calls := q.items[0]
			q.mu.Unlock()
			return calls, true
		}
		if q.closed {
			q.mu.Unlock()
			return nil, false
		}
		notify := q.notify
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false
		case <-notify:
		}
	}
}

func (q *effectAdmissionQueue) shift() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return
	}
	q.items[0] = nil
	q.items = q.items[1:]
}

func (q *effectAdmissionQueue) close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.items = nil
	q.mu.Unlock()
}

func (q *effectAdmissionQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func newEventQueue(capacity int) *eventQueue {
	return &eventQueue{
		capacity: capacity,
		notify:   make(chan struct{}, 1),
	}
}

func (q *eventQueue) push(ev event.Event) (bool, error) {
	if err := ev.Validate(); err != nil {
		return false, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false, ErrSessionClosed
	}
	if key := ev.CoalescingKey(); key != "" {
		for i := len(q.items) - 1; i >= 0; i-- {
			if q.items[i].CoalescingKey() == key {
				ev.Meta.Coalesced = true
				q.items[i] = ev
				q.overflowing = false
				q.signal()
				return false, nil
			}
		}
	}
	if len(q.items) >= q.capacity {
		episode := !q.overflowing
		q.overflowing = true
		return episode, ErrBackpressure
	}
	q.items = append(q.items, ev)
	q.overflowing = false
	q.signal()
	return false, nil
}

func (q *eventQueue) pop(ctx context.Context) (event.Event, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			ev := q.items[0]
			q.items[0] = event.Event{}
			q.items = q.items[1:]
			q.mu.Unlock()
			return ev, true
		}
		if q.closed {
			q.mu.Unlock()
			return event.Event{}, false
		}
		notify := q.notify
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return event.Event{}, false
		case <-notify:
		}
	}
}

func (q *eventQueue) close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.signal()
	q.mu.Unlock()
}

func (q *eventQueue) signal() {
	if q.closed {
		return
	}
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (s *Session[M]) beginClose() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if s.lifecycle != LifecycleClosed {
			s.lifecycle = LifecycleCancelling
		}
		s.mu.Unlock()
		s.queue.close()
		s.effectAdmissions.close()
		s.effects.Close()
		s.cancel()
	})
}
