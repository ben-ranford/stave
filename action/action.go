package action

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ben-ranford/stave/diag"
	"github.com/ben-ranford/stave/internal/canonical"
	"github.com/ben-ranford/stave/semantic"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ID string
type Safety string
type Idempotency string

const (
	ReadOnly      Safety      = "read_only"
	Reversible    Safety      = "reversible"
	Consequential Safety      = "consequential"
	Destructive   Safety      = "destructive"
	Idempotent    Idempotency = "idempotent"
	NonIdempotent Idempotency = "non_idempotent"
)

type Schema struct {
	ID     string                             `json:"id"`
	JSON   json.RawMessage                    `json:"json"`
	Decode func(json.RawMessage) (any, error) `json:"-"`
	Encode func(any) (json.RawMessage, error) `json:"-"`
}

func compileSchema(raw json.RawMessage) error {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return errors.New("invalid schema")
	}
	if err := enforceNumericPolicy(v); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing schema")
	}
	return schemaWalk(v)
}
func schemaWalk(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return errors.New("schema must be object")
	}
	allowed := map[string]bool{"type": true, "required": true, "additionalProperties": true, "properties": true, "enum": true, "minimum": true, "maximum": true, "minLength": true, "maxLength": true, "minItems": true, "maxItems": true, "items": true}
	for k := range m {
		if !allowed[k] {
			return fmt.Errorf("unsupported schema keyword %s", k)
		}
	}
	if t, ok := m["type"]; ok {
		ts, yes := t.(string)
		valid := map[string]bool{"object": true, "array": true, "string": true, "number": true, "integer": true, "boolean": true, "null": true}
		if !yes || !valid[ts] {
			return errors.New("invalid schema type")
		}
	}
	if req, ok := m["required"]; ok {
		a, yes := req.([]any)
		if !yes || len(a) == 0 {
			return errors.New("invalid required")
		}
		seen := map[string]bool{}
		for _, x := range a {
			s, yes := x.(string)
			if !yes || s == "" || seen[s] {
				return errors.New("invalid required")
			}
			seen[s] = true
		}
	}
	for _, k := range []string{"minLength", "maxLength", "minItems", "maxItems"} {
		if x, ok := m[k]; ok {
			n, yes := numInt(x)
			if !yes || n < 0 {
				return errors.New("invalid bound")
			}
		}
	}
	for _, k := range []string{"minimum", "maximum"} {
		if x, ok := m[k]; ok {
			if _, yes := numRat(x); !yes {
				return errors.New("invalid numeric bound")
			}
		}
	}
	if a, ok := numRat(m["minLength"]); ok {
		if b, yes := numRat(m["maxLength"]); yes && a.Cmp(b) > 0 {
			return errors.New("impossible length bounds")
		}
	}
	if a, ok := numRat(m["minItems"]); ok {
		if b, yes := numRat(m["maxItems"]); yes && a.Cmp(b) > 0 {
			return errors.New("impossible item bounds")
		}
	}
	if a, ok := numRat(m["minimum"]); ok {
		if b, yes := numRat(m["maximum"]); yes && a.Cmp(b) > 0 {
			return errors.New("impossible bounds")
		}
	}
	if e, ok := m["enum"]; ok {
		a, yes := e.([]any)
		if !yes || len(a) == 0 {
			return errors.New("invalid enum")
		}
	}
	if ap, ok := m["additionalProperties"]; ok {
		if _, yes := ap.(bool); !yes {
			return errors.New("invalid additionalProperties")
		}
	}
	if raw, present := m["properties"]; present {
		p, ok := raw.(map[string]any)
		if !ok {
			return errors.New("invalid properties")
		}
		for _, x := range p {
			if err := schemaWalk(x); err != nil {
				return err
			}
		}
	}
	if x, ok := m["items"]; ok {
		return schemaWalk(x)
	}
	return nil
}
func validateJSON(raw json.RawMessage, s json.RawMessage) error {
	var v, sp any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return errors.New("invalid JSON")
	}
	if err := enforceNumericPolicy(v); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing JSON")
	}
	sd := json.NewDecoder(bytes.NewReader(s))
	sd.UseNumber()
	if sd.Decode(&sp) != nil {
		return errors.New("invalid schema")
	}
	if err := enforceNumericPolicy(sp); err != nil {
		return err
	}
	return validateValue(v, sp, "$")
}
func validateValue(v, sp any, path string) error {
	m, ok := sp.(map[string]any)
	if !ok {
		return nil
	}
	if e, ok := m["enum"].([]any); ok {
		found := false
		for _, x := range e {
			if enumEqual(x, v) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s not in enum", path)
		}
	}
	if t, ok := m["type"].(string); ok {
		good := map[string]bool{"object": func() bool { _, x := v.(map[string]any); return x }(), "array": func() bool { _, x := v.([]any); return x }(), "string": func() bool { _, x := v.(string); return x }(), "number": func() bool { _, x := v.(json.Number); return x }(), "integer": func() bool {
			x, ok := v.(json.Number)
			if !ok {
				return false
			}
			r, e := new(big.Rat).SetString(x.String())
			return e && r.IsInt()
		}(), "boolean": func() bool { _, x := v.(bool); return x }(), "null": v == nil}
		if !good[t] {
			return fmt.Errorf("%s type violation", path)
		}
	}
	if x, ok := v.(map[string]any); ok {
		if req, ok := m["required"].([]any); ok {
			for _, k := range req {
				ks := fmt.Sprint(k)
				if _, yes := x[ks]; !yes {
					return fmt.Errorf("%s missing required %s", path, ks)
				}
			}
		}
		p := map[string]any{}
		if raw, ok := m["properties"]; ok {
			p, _ = raw.(map[string]any)
		}
		if raw, ok := m["properties"]; ok {
			_ = raw
			for k, sv := range p {
				if vv, yes := x[k]; yes {
					if err := validateValue(vv, sv, path+"."+k); err != nil {
						return err
					}
				}
			}
		}
		if ap, ok := m["additionalProperties"].(bool); ok && !ap {
			for k := range x {
				if _, yes := p[k]; !yes {
					return fmt.Errorf("%s additional property %s", path, k)
				}
			}
		}
	}
	if x, ok := v.(string); ok {
		if n, ok := numInt(m["minLength"]); ok && len([]rune(x)) < n {
			return fmt.Errorf("%s minLength", path)
		}
		if n, ok := numInt(m["maxLength"]); ok && len([]rune(x)) > n {
			return fmt.Errorf("%s maxLength", path)
		}
	}
	if x, ok := v.(json.Number); ok {
		xr, e := new(big.Rat).SetString(x.String())
		if e {
			if n, yes := numRat(m["minimum"]); yes && xr.Cmp(n) < 0 {
				return fmt.Errorf("%s minimum", path)
			}
			if n, yes := numRat(m["maximum"]); yes && xr.Cmp(n) > 0 {
				return fmt.Errorf("%s maximum", path)
			}
		}
	}
	if x, ok := v.([]any); ok {
		if n, yes := numInt(m["minItems"]); yes && len(x) < n {
			return fmt.Errorf("%s minItems", path)
		}
		if n, yes := numInt(m["maxItems"]); yes && len(x) > n {
			return fmt.Errorf("%s maxItems", path)
		}
		if sp, yes := m["items"]; yes {
			for i, z := range x {
				if err := validateValue(z, sp, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// enumEqual follows JSON Schema's value equality for the values Stave accepts.
// json.Number values are compared as exact rationals, so equivalent spellings
// (1, 1.0, and 1e0) match without converting large values through float64.
func enumEqual(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		ar, aok := parseNumber(av)
		br, bok := parseNumber(bv)
		return aok && bok && ar.Cmp(br) == 0
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !enumEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, x := range av {
			y, exists := bv[k]
			if !exists || !enumEqual(x, y) {
				return false
			}
		}
		return true
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nil:
		return b == nil
	}
	return false
}

const (
	maxNumericLiteralBytes = 1024
	maxNumericExponent     = 10000
)

// ErrNumericLimit indicates that a numeric literal exceeds Stave's bounded
// parsing policy (1024 literal bytes and exponent magnitude <= 10000).
var ErrNumericLimit = errors.New("numeric literal exceeds resource limits")

func parseNumber(x json.Number) (*big.Rat, bool) {
	s := x.String()
	if len(s) == 0 || len(s) > maxNumericLiteralBytes {
		return nil, false
	}
	mantissa := s
	if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
		exp := mantissa[i+1:]
		if len(exp) == 0 || len(exp) > 6 {
			return nil, false
		}
		n, err := strconv.Atoi(exp)
		if err != nil || n > maxNumericExponent || n < -maxNumericExponent {
			return nil, false
		}
		mantissa = mantissa[:i]
	}
	digits := strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(mantissa, "-"), "+"), "0")
	digits = strings.ReplaceAll(digits, ".", "")
	if len(digits) > maxNumericLiteralBytes {
		return nil, false
	}
	r, err := new(big.Rat).SetString(s)
	return r, err
}

func enforceNumericPolicy(v any) error {
	switch x := v.(type) {
	case json.Number:
		if _, ok := parseNumber(x); !ok {
			return ErrNumericLimit
		}
	case []any:
		for _, item := range x {
			if err := enforceNumericPolicy(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range x {
			if err := enforceNumericPolicy(item); err != nil {
				return err
			}
		}
	}
	return nil
}
func numRat(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case json.Number:
		return parseNumber(x)
	case float64:
		return new(big.Rat).SetFloat64(x), true
	}
	return nil, false
}
func numInt(v any) (int, bool) {
	r, ok := numRat(v)
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		return 0, false
	}
	return int(r.Num().Int64()), true
}
func tokenOnlyPresentation(c Confirmation) bool {
	return c.Token != "" && c.SessionID != "" && c.ActionID == "" && c.Target == (semantic.Target{}) && c.ArgHash == "" && c.ExpiresAt.IsZero() && !c.Used && c.ActionVersion == "" && c.RevisionMin == 0 && c.RevisionMax == 0 && c.Safety == "" && c.PolicyID == "" && c.PolicyEpoch == 0
}
func validPresentation(p, grant Confirmation) bool {
	if tokenOnlyPresentation(p) {
		return true
	}
	return reflect.DeepEqual(p, grant)
}

func (s Schema) Validate(b json.RawMessage) (any, error) {
	if len(s.JSON) == 0 {
		return nil, errors.New("missing schema")
	}
	if err := compileSchema(s.JSON); err != nil {
		return nil, err
	}
	var x any
	if err := validateJSON(b, s.JSON); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&x); err != nil {
		return nil, err
	}
	if s.Decode != nil {
		return s.Decode(b)
	}
	return x, nil
}

type ConfirmationPolicy struct {
	Required  bool `json:"required"`
	SingleUse bool `json:"singleUse"`
}
type Definition struct {
	ID           ID                 `json:"id"`
	Version      string             `json:"version"`
	Title        string             `json:"title"`
	Description  string             `json:"description,omitempty"`
	InputSchema  Schema             `json:"inputSchema"`
	OutputSchema Schema             `json:"outputSchema"`
	Safety       Safety             `json:"safety"`
	Idempotency  Idempotency        `json:"idempotency"`
	Cancellable  bool               `json:"cancellable,omitempty"`
	TargetRoles  []semantic.Role    `json:"targetRoles,omitempty"`
	RequiredCaps []string           `json:"requiredCaps,omitempty"`
	Confirmation ConfirmationPolicy `json:"confirmation,omitempty"`
	Timeout      time.Duration      `json:"timeout,omitempty"`
}

func (d Definition) Validate() error {
	if d.ID == "" || d.Version == "" || d.InputSchema.ID == "" || d.OutputSchema.ID == "" {
		return errors.New("invalid action definition")
	}
	if d.Safety == "" {
		return errors.New("missing safety")
	}
	switch d.Safety {
	case ReadOnly, Reversible, Consequential, Destructive:
	default:
		return errors.New("invalid safety")
	}
	switch d.Idempotency {
	case "", Idempotent, NonIdempotent:
	default:
		return errors.New("invalid idempotency")
	}
	return nil
}

// EffectiveIdempotency returns the conservative default for legacy definitions
// that omit idempotency. Empty idempotency is treated as non_idempotent.
func (d Definition) EffectiveIdempotency() Idempotency {
	if d.Idempotency == "" {
		return NonIdempotent
	}
	return d.Idempotency
}

type Confirmation struct {
	Token         string          `json:"token"`
	SessionID     string          `json:"sessionId"`
	ActionID      ID              `json:"actionId"`
	Target        semantic.Target `json:"target"`
	ArgHash       string          `json:"argHash"`
	ExpiresAt     time.Time       `json:"expiresAt"`
	Used          bool            `json:"used"`
	ActionVersion string          `json:"actionVersion"`
	RevisionMin   uint64          `json:"revisionMin"`
	RevisionMax   uint64          `json:"revisionMax"`
	Safety        Safety          `json:"safety"`
	PolicyID      string          `json:"policyId"`
	PolicyEpoch   uint64          `json:"policyEpoch"`
}

func NewConfirmation(session string, d Definition, target semantic.Target, args json.RawMessage, expiry time.Time) (Confirmation, error) {
	return NewConfirmationE(session, d, target, args, expiry)
}
func NewConfirmationE(session string, d Definition, target semantic.Target, args json.RawMessage, expiry time.Time) (Confirmation, error) {
	h, e := canonical.JSON(args)
	if e != nil {
		return Confirmation{}, e
	}
	sum := sha256.Sum256(h)
	token := make([]byte, 32)
	if _, e = rand.Read(token); e != nil {
		return Confirmation{}, e
	}
	return Confirmation{Token: hex.EncodeToString(token), SessionID: session, ActionID: d.ID, ActionVersion: d.Version, Target: target, ArgHash: hex.EncodeToString(sum[:]), ExpiresAt: expiry, Safety: d.Safety}, nil
}

type Call struct {
	CallID       string
	ActionID     ID
	Target       semantic.Target
	Arguments    json.RawMessage
	Confirmation *Confirmation
	Deadline     time.Time
	PolicyID     string
	PolicyEpoch  uint64
	SessionID    string
}
type ResultStatus string

const (
	ResultOK       ResultStatus = "ok"
	ResultRejected ResultStatus = "rejected"
)

type Result struct {
	CallID      string
	ActionID    ID
	Target      semantic.Target
	Status      ResultStatus
	Output      json.RawMessage
	Error       *Error
	Revision    uint64
	TreeHash    string
	Diagnostics []diag.Diagnostic
}
type Code string

const (
	InvalidRequest        Code = "INVALID_REQUEST"
	ActionNotFound        Code = "ACTION_NOT_FOUND"
	InvalidArgument       Code = "INVALID_ARGUMENT"
	OutputSchemaViolation Code = "OUTPUT_SCHEMA_VIOLATION"
	NodeNotFound          Code = "NODE_NOT_FOUND"
	NodeReplaced          Code = "NODE_REPLACED"
	AmbiguousTarget       Code = "AMBIGUOUS_TARGET"
	StaleSnapshot         Code = "STALE_SNAPSHOT"
	CapabilityMismatch    Code = "CAPABILITY_MISMATCH"
	ConfirmationRequired  Code = "CONFIRMATION_REQUIRED"
	ConfirmationInvalid   Code = "CONFIRMATION_INVALID"
	Forbidden             Code = "FORBIDDEN"
	Conflict              Code = "CONFLICT"
	DeadlineExceeded      Code = "DEADLINE_EXCEEDED"
	Cancelled             Code = "CANCELLED"
	ResourceLimit         Code = "RESOURCE_LIMIT"
	Internal              Code = "INTERNAL"
)

type Error struct {
	Code       Code            `json:"code"`
	Message    string          `json:"message"`
	Retryable  bool            `json:"retryable"`
	ActionID   ID              `json:"actionId,omitempty"`
	NodeID     semantic.NodeID `json:"nodeId,omitempty"`
	Revision   uint64          `json:"revision,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
	CauseClass string          `json:"causeClass,omitempty"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

type Handler func(context.Context, Call, any) (any, error)
type entry struct {
	Def Definition
	H   Handler
}

func cloneRaw(raw json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), raw...) }

func cloneSchema(s Schema) Schema {
	s.JSON = cloneRaw(s.JSON)
	return s
}

func cloneDefinition(d Definition) Definition {
	d.InputSchema = cloneSchema(d.InputSchema)
	d.OutputSchema = cloneSchema(d.OutputSchema)
	d.TargetRoles = append([]semantic.Role(nil), d.TargetRoles...)
	d.RequiredCaps = append([]string(nil), d.RequiredCaps...)
	return d
}

type Registry struct {
	mu     sync.RWMutex
	m      map[ID]entry
	used   map[string]bool
	grants map[string]Confirmation
}

func NewRegistry() *Registry {
	return &Registry{m: map[ID]entry{}, used: map[string]bool{}, grants: map[string]Confirmation{}}
}
func (r *Registry) IssueConfirmation(c Confirmation) error {
	if c.Token == "" || c.SessionID == "" {
		return errors.New("invalid confirmation")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.grants[c.Token] = c
	return nil
}
func (r *Registry) Register(d Definition, h Handler) error {
	d = cloneDefinition(d)
	if err := d.Validate(); err != nil {
		return err
	}
	if h == nil {
		return errors.New("nil handler")
	}
	if err := compileSchema(d.InputSchema.JSON); err != nil {
		return err
	}
	if err := compileSchema(d.OutputSchema.JSON); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[d.ID]; ok {
		return errors.New("duplicate action")
	}
	r.m[d.ID] = entry{Def: d, H: h}
	return nil
}
func Register[In, Out any](r *Registry, d Definition, decode func(json.RawMessage) (In, error), encode func(Out) (json.RawMessage, error), h func(context.Context, Call, In) (Out, error)) error {
	return r.Register(d, func(ctx context.Context, c Call, v any) (any, error) {
		in, e := decode(c.Arguments)
		if e != nil {
			return nil, e
		}
		out, e := h(ctx, c, in)
		if e != nil {
			return nil, e
		}
		return encode(out)
	})
}
func (r *Registry) Definition(id ID) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.m[id]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(e.Def), true
}
func (r *Registry) Manifest() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Definition, 0, len(r.m))
	for _, e := range r.m {
		out = append(out, cloneDefinition(e.Def))
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i].ID) < string(out[j].ID) })
	return out
}
func (r *Registry) Invoke(ctx context.Context, c Call) Result {
	r.mu.RLock()
	e, ok := r.m[c.ActionID]
	r.mu.RUnlock()
	if !ok {
		return reject(c, ActionNotFound, "action not found")
	}
	if !c.Deadline.IsZero() && time.Now().After(c.Deadline) {
		return reject(c, DeadlineExceeded, "deadline exceeded")
	}
	v, err := e.Def.InputSchema.Validate(c.Arguments)
	if err != nil {
		if errors.Is(err, ErrNumericLimit) {
			return reject(c, ResourceLimit, err.Error())
		}
		return reject(c, InvalidArgument, err.Error())
	}
	if e.Def.Confirmation.Required {
		if c.Confirmation == nil {
			return reject(c, ConfirmationRequired, "confirmation required")
		}
		r.mu.Lock()
		grant, issued := r.grants[c.Confirmation.Token]
		if !issued || grant.SessionID != c.SessionID {
			r.mu.Unlock()
			return reject(c, ConfirmationInvalid, "unissued or wrong-session confirmation")
		}
		if !validPresentation(*c.Confirmation, grant) {
			r.mu.Unlock()
			return reject(c, ConfirmationInvalid, "confirmation binding mismatch")
		}
		r.mu.Unlock()
		if grant.Used || time.Now().After(grant.ExpiresAt) || grant.ActionID != c.ActionID || grant.ActionVersion != e.Def.Version || grant.Target != c.Target || grant.Safety != e.Def.Safety || (grant.PolicyID != "" && (grant.PolicyID != c.PolicyID || grant.PolicyEpoch != c.PolicyEpoch)) || (grant.RevisionMin > 0 && c.Target.ObservedRevision < grant.RevisionMin) || (grant.RevisionMax > 0 && c.Target.ObservedRevision > grant.RevisionMax) {
			return reject(c, ConfirmationInvalid, "invalid confirmation")
		}
		canon, ce := canonical.JSON(c.Arguments)
		if ce != nil {
			return reject(c, InvalidArgument, ce.Error())
		}
		h := sha256.Sum256(canon)
		if grant.ArgHash != hex.EncodeToString(h[:]) {
			return reject(c, ConfirmationInvalid, "argument hash mismatch")
		}
		r.mu.Lock()
		if r.used[c.Confirmation.Token] {
			r.mu.Unlock()
			return reject(c, ConfirmationInvalid, "confirmation replay")
		}
		r.used[c.Confirmation.Token] = true
		r.mu.Unlock()
	}
	out, err := e.H(ctx, c, v)
	if err != nil {
		return reject(c, Internal, err.Error())
	}
	var b json.RawMessage
	if x, ok := out.(json.RawMessage); ok {
		b = x
	} else if x, ok := out.([]byte); ok {
		b = x
	} else {
		b, _ = json.Marshal(out)
	}
	if err := validateJSON(b, e.Def.OutputSchema.JSON); err != nil {
		if errors.Is(err, ErrNumericLimit) {
			return reject(c, ResourceLimit, err.Error())
		}
		return reject(c, OutputSchemaViolation, err.Error())
	}
	return Result{CallID: c.CallID, ActionID: c.ActionID, Target: c.Target, Status: ResultOK, Output: b}
}
func reject(c Call, code Code, msg string) Result {
	return Result{CallID: c.CallID, ActionID: c.ActionID, Target: c.Target, Status: ResultRejected, Error: &Error{Code: code, Message: msg}}
}

var _ = fmt.Sprintf
