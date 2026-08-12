package effect

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/internal/canonical"
)

const (
	SchemaVersion = "stave.effect/v1"
	IDAlgorithm   = "stave-effect-id-v1"
)

var (
	ErrClosed       = errors.New("effect executor closed")
	ErrMissingPort  = errors.New("effect port unavailable")
	ErrBackpressure = errors.New("effect executor saturated")
)

type Delivery string

const (
	DeclarationOrder Delivery = "declaration_order"
	CompletionOrder  Delivery = "completion_order"
)

type Status string

const (
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusTimedOut  Status = "timed_out"
)

type Spec struct {
	Kind        string        `json:"kind"`
	Lane        string        `json:"lane,omitempty"`
	Cancellable bool          `json:"cancellable,omitempty"`
	Sensitive   bool          `json:"sensitive,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
}

type Request struct {
	Spec  Spec `json:"spec"`
	Input any  `json:"input,omitempty"`
}

type Call struct {
	ID       string `json:"id"`
	Sequence uint64 `json:"sequence"`
	Revision uint64 `json:"revision"`
	Ordinal  uint32 `json:"ordinal"`
	Spec     Spec   `json:"spec"`
	Input    any    `json:"input,omitempty"`
}

type Outcome struct {
	ID              string `json:"id"`
	Sequence        uint64 `json:"sequence"`
	Revision        uint64 `json:"revision"`
	Ordinal         uint32 `json:"ordinal"`
	Lane            string `json:"lane,omitempty"`
	Status          Status `json:"status"`
	Value           any    `json:"value,omitempty"`
	Error           string `json:"error,omitempty"`
	Sensitive       bool   `json:"sensitive,omitempty"`
	CompletionIndex uint32 `json:"completionIndex,omitempty"`
}

type Port interface {
	Run(context.Context, Call) (any, error)
}

type PortFunc func(context.Context, Call) (any, error)

func (f PortFunc) Run(ctx context.Context, call Call) (any, error) {
	return f(ctx, call)
}

type Sink func(event.Event) error

type Options struct {
	Ports            map[string]Port
	Parallelism      int
	Delivery         Delivery
	MaxBatch         int
	MaxInputBytes    int
	MaxOutputBytes   int
	MaxActiveBatches int
	OnSinkError      func(error)
}

type Executor struct {
	mu             sync.Mutex
	ports          map[string]Port
	parallelism    int
	delivery       Delivery
	maxBatch       int
	maxInputBytes  int
	maxOutputBytes int
	closed         bool
	nextBatchID    uint64
	active         map[uint64]context.CancelFunc
	batchSlots     chan struct{}
	ctx            context.Context
	cancel         context.CancelFunc
	onSinkError    func(error)
}

func NewExecutor(opts Options) *Executor {
	if opts.Parallelism <= 0 {
		opts.Parallelism = 1
	}
	if opts.Delivery == "" {
		opts.Delivery = DeclarationOrder
	}
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = 256
	}
	if opts.MaxInputBytes <= 0 {
		opts.MaxInputBytes = 1 << 20
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = 1 << 20
	}
	if opts.MaxActiveBatches <= 0 {
		opts.MaxActiveBatches = 1
	}
	executorCtx, executorCancel := context.WithCancel(context.Background())
	ports := map[string]Port{}
	for kind, port := range opts.Ports {
		ports[kind] = port
	}
	return &Executor{
		ports:          ports,
		parallelism:    opts.Parallelism,
		delivery:       opts.Delivery,
		maxBatch:       opts.MaxBatch,
		maxInputBytes:  opts.MaxInputBytes,
		maxOutputBytes: opts.MaxOutputBytes,
		active:         map[uint64]context.CancelFunc{},
		batchSlots:     make(chan struct{}, opts.MaxActiveBatches),
		ctx:            executorCtx,
		cancel:         executorCancel,
		onSinkError:    opts.OnSinkError,
	}
}

func ID(sessionID string, sequence uint64, ordinal uint32, kind string) string {
	sum, _ := canonical.Hash(struct {
		Algorithm string `json:"algorithm"`
		SessionID string `json:"sessionId"`
		Sequence  uint64 `json:"sequence"`
		Ordinal   uint32 `json:"ordinal"`
		Kind      string `json:"kind"`
	}{
		Algorithm: IDAlgorithm,
		SessionID: sessionID,
		Sequence:  sequence,
		Ordinal:   ordinal,
		Kind:      kind,
	})
	return "fx1_" + hex.EncodeToString(sum[:16])
}

func Bind(sessionID string, sequence, revision uint64, requests []Request) ([]Call, error) {
	calls := make([]Call, 0, len(requests))
	for i, request := range requests {
		if err := request.Validate(); err != nil {
			return nil, err
		}
		input, err := cloneAny(request.Input)
		if err != nil {
			return nil, err
		}
		calls = append(calls, Call{
			ID:       ID(sessionID, sequence, uint32(i), request.Spec.Kind),
			Sequence: sequence,
			Revision: revision,
			Ordinal:  uint32(i),
			Spec:     request.Spec,
			Input:    input,
		})
	}
	return calls, nil
}

func (r Request) Validate() error {
	return r.Spec.Validate()
}

func (s Spec) Validate() error {
	if s.Kind == "" {
		return errors.New("effect kind is required")
	}
	if s.Timeout < 0 {
		return errors.New("effect timeout must be non-negative")
	}
	return nil
}

func (o Outcome) Event() event.Event {
	errorText := sanitizeErrorText(o.Error)
	return event.Event{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.EffectResult,
		Revision:      o.Revision,
		Payload: event.EffectResultPayload{
			CallID:    o.ID,
			Ordinal:   o.Ordinal,
			Lane:      o.Lane,
			Status:    string(o.Status),
			Value:     o.Value,
			Error:     errorText,
			Sensitive: o.Sensitive,
		},
		Meta: event.Metadata{
			Lane:            o.Lane,
			CompletionIndex: o.CompletionIndex,
		},
	}
}

func sanitizeErrorText(value string) string {
	if value == "" {
		return ""
	}
	runes := make([]rune, 0, len(value))
	for _, r := range value {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			continue
		}
		runes = append(runes, r)
		if len(runes) == 512 {
			break
		}
	}
	if len(runes) == 0 {
		return "effect execution failed"
	}
	return string(runes)
}

func (e *Executor) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	e.cancel()
	active := make([]context.CancelFunc, 0, len(e.active))
	for _, cancel := range e.active {
		active = append(active, cancel)
	}
	e.mu.Unlock()
	for _, cancel := range active {
		cancel()
	}
}

func (e *Executor) Deliver(ctx context.Context, calls []Call, sink Sink) error {
	if len(calls) == 0 {
		return nil
	}
	if len(calls) > e.maxBatch {
		return fmt.Errorf("effect batch exceeds limit")
	}
	for _, call := range calls {
		data, err := canonical.Encode(call.Input)
		if err != nil {
			return err
		}
		if len(data) > e.maxInputBytes {
			return fmt.Errorf("effect input exceeds limit")
		}
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	select {
	case e.batchSlots <- struct{}{}:
	default:
		e.mu.Unlock()
		return ErrBackpressure
	}
	batchCtx, cancel := context.WithCancel(ctx)
	batchID := e.nextBatchID
	e.nextBatchID++
	e.active[batchID] = cancel
	e.mu.Unlock()

	go e.runBatch(batchID, batchCtx, calls, sink)
	return nil
}

func (e *Executor) runBatch(batchID uint64, batchCtx context.Context, calls []Call, sink Sink) {
	defer e.finishBatch(batchID)

	results := make(chan Outcome, len(calls))
	jobs := make(chan Call, len(calls))
	var workers sync.WaitGroup
	workerCount := e.parallelism
	if workerCount > len(calls) {
		workerCount = len(calls)
	}
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for call := range jobs {
				results <- e.runCall(batchCtx, call)
			}
		}()
	}
	for _, call := range calls {
		jobs <- call
	}
	close(jobs)

	go func() {
		workers.Wait()
		close(results)
	}()

	switch e.delivery {
	case CompletionOrder:
		var completionIndex uint32
		for outcome := range results {
			outcome.CompletionIndex = completionIndex
			completionIndex++
			if err := sink(outcome.Event()); err != nil {
				e.reportSinkError(err)
				return
			}
		}
	default:
		pending := map[uint32]Outcome{}
		next := uint32(0)
		for outcome := range results {
			pending[outcome.Ordinal] = outcome
			for {
				ready, ok := pending[next]
				if !ok {
					break
				}
				delete(pending, next)
				if err := sink(ready.Event()); err != nil {
					e.reportSinkError(err)
					return
				}
				next++
			}
		}
	}
}

func (e *Executor) runCall(parent context.Context, call Call) (out Outcome) {
	out = Outcome{
		ID:        call.ID,
		Sequence:  call.Sequence,
		Revision:  call.Revision,
		Ordinal:   call.Ordinal,
		Lane:      call.Spec.Lane,
		Sensitive: call.Spec.Sensitive,
		Status:    StatusCompleted,
	}
	ctx, cancel := context.WithCancel(e.ctx)
	stopParent := func() bool { return false }
	if call.Spec.Cancellable {
		stopParent = context.AfterFunc(parent, cancel)
	}
	defer stopParent()
	if call.Spec.Timeout > 0 {
		deadlineCtx, deadlineCancel := context.WithTimeout(ctx, call.Spec.Timeout)
		oldCancel := cancel
		ctx = deadlineCtx
		cancel = func() { deadlineCancel(); oldCancel() }
	}
	defer cancel()

	defer func() {
		if recovered := recover(); recovered != nil {
			out.Status = StatusFailed
			out.Value = nil
			out.Error = "effect execution failed"
		}
	}()

	e.mu.Lock()
	port := e.ports[call.Spec.Kind]
	e.mu.Unlock()
	if port == nil {
		out.Status = StatusFailed
		out.Error = ErrMissingPort.Error()
		return out
	}

	value, err := port.Run(ctx, call)
	if err == nil {
		encoded, encodeErr := canonical.Encode(value)
		if encodeErr != nil || len(encoded) > e.maxOutputBytes {
			out.Status = StatusFailed
			out.Error = "effect output exceeds limit"
			return out
		}
		out.Value = value
		return out
	}
	out.Value = nil
	if call.Spec.Sensitive {
		out.Error = "effect execution failed"
	} else {
		out.Error = err.Error()
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		out.Status = StatusTimedOut
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		out.Status = StatusCancelled
	default:
		out.Status = StatusFailed
	}
	return out
}

func (e *Executor) finishBatch(batchID uint64) {
	e.mu.Lock()
	cancel := e.active[batchID]
	delete(e.active, batchID)
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-e.batchSlots
}

func (e *Executor) reportSinkError(err error) {
	if err != nil && e.onSinkError != nil {
		e.onSinkError(err)
	}
}

func cloneAny(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := canonical.Encode(value)
	if err != nil {
		return nil, err
	}
	var out any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
