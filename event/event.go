package event

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ben-ranford/stave/internal/canonical"
	"github.com/ben-ranford/stave/secret"
)

const (
	SchemaVersion        = "stave.event/v1"
	MaxPayloadTextBytes  = 1 << 20
	MaxViewportDimension = 1 << 16
)

type Kind string

const (
	Key           Kind = "key"
	Text          Kind = "text"
	Resize        Kind = "resize"
	Pointer       Kind = "pointer"
	Focus         Kind = "focus"
	Blur          Kind = "blur"
	ActionInvoked Kind = "action_invoked"
	EffectResult  Kind = "effect_result"
	Tick          Kind = "tick"
	Cancel        Kind = "cancel"
	Shutdown      Kind = "shutdown"
	Diagnostic    Kind = "diagnostic"
	SecureInput   Kind = "secure_input"
)

type LogicalTime struct {
	Tick uint64 `json:"tick,omitempty"`
}

type Metadata struct {
	Source          string `json:"source,omitempty"`
	Coalesced       bool   `json:"coalesced,omitempty"`
	CompletionIndex uint32 `json:"completionIndex,omitempty"`
	Lane            string `json:"lane,omitempty"`
}

type Target struct {
	NodeID           string `json:"nodeId,omitempty"`
	Generation       uint32 `json:"generation,omitempty"`
	ObservedRevision uint64 `json:"observedRevision,omitempty"`
}

type KeyPayload struct {
	Key       string   `json:"key"`
	Rune      rune     `json:"rune,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type TextPayload struct {
	Text      string `json:"text"`
	Committed bool   `json:"committed,omitempty"`
}

type ResizePayload struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type PointerPayload struct {
	SourceID string   `json:"sourceId"`
	X        int      `json:"x"`
	Y        int      `json:"y"`
	Phase    string   `json:"phase,omitempty"`
	Buttons  []string `json:"buttons,omitempty"`
}

type ActionInvokedPayload struct {
	CallID    string  `json:"callId"`
	ActionID  string  `json:"actionId"`
	Target    *Target `json:"target,omitempty"`
	Arguments any     `json:"arguments,omitempty"`
	Sensitive bool    `json:"sensitive,omitempty"`
}

type EffectResultPayload struct {
	CallID    string `json:"callId"`
	Ordinal   uint32 `json:"ordinal"`
	Lane      string `json:"lane,omitempty"`
	Status    string `json:"status"`
	Value     any    `json:"value,omitempty"`
	Error     string `json:"error,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

type DiagnosticPayload struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	SafeContext map[string]string `json:"safeContext,omitempty"`
}

// SecureInputPayload contains only an opaque handle; secret material and
// diagnostic identifiers are never placed in the event stream.
type SecureInputPayload struct {
	Handle secret.Handle `json:"handle"`
}

type Redaction struct {
	Redacted bool   `json:"redacted"`
	Reason   string `json:"reason,omitempty"`
}

type Event struct {
	SchemaVersion string      `json:"schemaVersion"`
	Kind          Kind        `json:"kind"`
	Sequence      uint64      `json:"sequence,omitempty"`
	Revision      uint64      `json:"revision,omitempty"`
	Timestamp     LogicalTime `json:"timestamp,omitempty"`
	Payload       any         `json:"payload,omitempty"`
	Meta          Metadata    `json:"meta,omitempty"`
}

func New(kind Kind, payload any) (Event, error) {
	ev := Event{SchemaVersion: SchemaVersion, Kind: kind, Payload: payload}
	if err := ev.Validate(); err != nil {
		return Event{}, err
	}
	return ev, nil
}

func (e Event) WithAccepted(sequence, revision uint64) Event {
	e.SchemaVersion = SchemaVersion
	e.Sequence = sequence
	e.Revision = revision
	e.Timestamp = LogicalTime{Tick: sequence}
	return e
}

func (e Event) Coalescible() bool {
	return e.Kind == Resize || e.Kind == Pointer
}

func (e Event) CoalescingKey() string {
	switch payload := e.Payload.(type) {
	case ResizePayload:
		return string(Resize)
	case PointerPayload:
		return string(Pointer) + ":" + payload.SourceID
	default:
		return ""
	}
}

func (e Event) Hash() ([32]byte, error) {
	return canonical.Hash(e.sanitized())
}

func (e Event) HashString() (string, error) {
	sum, err := e.Hash()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

func (e Event) CanonicalJSON() ([]byte, error) {
	return canonical.Encode(e.sanitized())
}

func (e Event) Clone() (Event, error) {
	data, err := canonical.Encode(e)
	if err != nil {
		return Event{}, err
	}
	var raw struct {
		SchemaVersion string          `json:"schemaVersion"`
		Kind          Kind            `json:"kind"`
		Sequence      uint64          `json:"sequence"`
		Revision      uint64          `json:"revision"`
		Timestamp     LogicalTime     `json:"timestamp"`
		Payload       json.RawMessage `json:"payload"`
		Meta          Metadata        `json:"meta"`
	}
	if err := canonical.Decode(data, &raw); err != nil {
		return Event{}, err
	}
	payload, err := decodePayload(raw.Kind, raw.Payload)
	if err != nil {
		return Event{}, err
	}
	out := Event{SchemaVersion: raw.SchemaVersion, Kind: raw.Kind, Sequence: raw.Sequence, Revision: raw.Revision, Timestamp: raw.Timestamp, Payload: payload, Meta: raw.Meta}
	if err := out.Validate(); err != nil {
		return Event{}, err
	}
	return out, nil
}

func Equal(a, b Event) bool {
	return canonical.Equal(a.sanitized(), b.sanitized())
}

func (e Event) Validate() error {
	if e.SchemaVersion == "" {
		e.SchemaVersion = SchemaVersion
	}
	if e.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported event schemaVersion %q", e.SchemaVersion)
	}
	switch e.Kind {
	case Key:
		payload, ok := e.Payload.(KeyPayload)
		if !ok {
			return errors.New("key event requires KeyPayload")
		}
		if err := validateKeyPayload(payload); err != nil {
			return err
		}
		return validateTextFields(append([]string{payload.Key}, payload.Modifiers...)...)
	case Text:
		payload, ok := e.Payload.(TextPayload)
		if !ok {
			return errors.New("text event requires TextPayload")
		}
		return validateText(payload.Text)
	case Resize:
		payload, ok := e.Payload.(ResizePayload)
		if !ok {
			return errors.New("resize event requires ResizePayload")
		}
		if payload.Width < 0 || payload.Height < 0 {
			return errors.New("resize dimensions must be non-negative")
		}
		if payload.Width > MaxViewportDimension || payload.Height > MaxViewportDimension {
			return errors.New("resize dimensions exceed supported bounds")
		}
		return nil
	case Pointer:
		payload, ok := e.Payload.(PointerPayload)
		if !ok {
			return errors.New("pointer event requires PointerPayload")
		}
		if strings.TrimSpace(payload.SourceID) == "" {
			return errors.New("pointer sourceId is required")
		}
		fields := append([]string{payload.SourceID, payload.Phase}, payload.Buttons...)
		return validateTextFields(fields...)
	case Focus, Blur, Tick, Cancel, Shutdown:
		if e.Payload != nil {
			return fmt.Errorf("%s event must not carry a payload", e.Kind)
		}
		return nil
	case ActionInvoked:
		payload, ok := e.Payload.(ActionInvokedPayload)
		if !ok {
			return errors.New("action_invoked event requires ActionInvokedPayload")
		}
		if strings.TrimSpace(payload.CallID) == "" || strings.TrimSpace(payload.ActionID) == "" {
			return errors.New("action_invoked requires callId and actionId")
		}
		if err := validateTextFields(payload.CallID, payload.ActionID); err != nil {
			return err
		}
		if payload.Target != nil {
			if err := validateText(payload.Target.NodeID); err != nil {
				return err
			}
		}
		return nil
	case EffectResult:
		payload, ok := e.Payload.(EffectResultPayload)
		if !ok {
			return errors.New("effect_result event requires EffectResultPayload")
		}
		if strings.TrimSpace(payload.CallID) == "" || strings.TrimSpace(payload.Status) == "" {
			return errors.New("effect_result requires callId and status")
		}
		return validateTextFields(payload.CallID, payload.Status, payload.Lane, payload.Error)
	case Diagnostic:
		payload, ok := e.Payload.(DiagnosticPayload)
		if !ok {
			return errors.New("diagnostic event requires DiagnosticPayload")
		}
		if strings.TrimSpace(payload.Code) == "" || strings.TrimSpace(payload.Message) == "" {
			return errors.New("diagnostic requires code and message")
		}
		if err := validateTextFields(payload.Code, payload.Message); err != nil {
			return err
		}
		for k, v := range payload.SafeContext {
			if err := validateTextFields(k, v); err != nil {
				return err
			}
		}
		return nil
	case SecureInput:
		payload, ok := e.Payload.(SecureInputPayload)
		if !ok || payload.Handle.ID == "" {
			return errors.New("secure_input requires opaque handle")
		}
		return nil
	default:
		return fmt.Errorf("unsupported event kind %q", e.Kind)
	}
}

func validateKeyPayload(payload KeyPayload) error {
	if payload.Key == "rune" {
		if payload.Rune == 0 || !unicode.IsPrint(payload.Rune) || unicode.IsControl(payload.Rune) {
			return errors.New("rune key requires printable rune identity")
		}
	} else {
		if payload.Rune != 0 {
			return errors.New("named key must not carry rune identity")
		}
		if !canonicalNamedKey(payload.Key) {
			return fmt.Errorf("unsupported canonical key %q", payload.Key)
		}
	}
	seen := map[string]bool{}
	for _, modifier := range payload.Modifiers {
		if modifier != "shift" && modifier != "ctrl" && modifier != "alt" && modifier != "meta" {
			return fmt.Errorf("unsupported key modifier %q", modifier)
		}
		if seen[modifier] {
			return fmt.Errorf("duplicate key modifier %q", modifier)
		}
		seen[modifier] = true
	}
	return nil
}

func canonicalNamedKey(key string) bool {
	switch key {
	case "enter", "escape", "tab", "backspace", "delete", "insert", "space", "up", "down", "left", "right", "home", "end", "pageup", "pagedown":
		return true
	}
	if len(key) >= 2 && key[0] == 'f' {
		n, err := strconv.Atoi(key[1:])
		return err == nil && n >= 1 && n <= 24
	}
	return false
}

func (e Event) MarshalJSON() ([]byte, error) {
	sanitized := e.sanitized()
	if err := sanitized.Validate(); err != nil {
		return nil, err
	}
	type dto Event
	return json.Marshal(dto(sanitized))
}

func (e *Event) UnmarshalJSON(data []byte) error {
	var raw struct {
		SchemaVersion string          `json:"schemaVersion"`
		Kind          Kind            `json:"kind"`
		Sequence      uint64          `json:"sequence"`
		Revision      uint64          `json:"revision"`
		Timestamp     LogicalTime     `json:"timestamp"`
		Payload       json.RawMessage `json:"payload"`
		Meta          Metadata        `json:"meta"`
	}
	if err := decodeJSON(data, &raw); err != nil {
		return err
	}
	payload, err := decodePayload(raw.Kind, raw.Payload)
	if err != nil {
		return err
	}
	out := Event{
		SchemaVersion: raw.SchemaVersion,
		Kind:          raw.Kind,
		Sequence:      raw.Sequence,
		Revision:      raw.Revision,
		Timestamp:     raw.Timestamp,
		Payload:       payload,
		Meta:          raw.Meta,
	}
	if out.SchemaVersion == "" {
		out.SchemaVersion = SchemaVersion
	}
	if err := out.Validate(); err != nil {
		return err
	}
	*e = out
	return nil
}

func (e Event) sanitized() Event {
	if e.SchemaVersion == "" {
		e.SchemaVersion = SchemaVersion
	}
	switch payload := e.Payload.(type) {
	case ActionInvokedPayload:
		payload = sanitizeAction(payload)
		e.Payload = payload
	case EffectResultPayload:
		payload = sanitizeEffectResult(payload)
		e.Payload = payload
	case DiagnosticPayload:
		copyCtx := map[string]string{}
		for k, v := range payload.SafeContext {
			copyCtx[k] = v
		}
		payload.SafeContext = copyCtx
		e.Payload = payload
	case KeyPayload:
		payload.Modifiers = append([]string(nil), payload.Modifiers...)
		e.Payload = payload
	case PointerPayload:
		payload.Buttons = append([]string(nil), payload.Buttons...)
		e.Payload = payload
	}
	return e
}

func sanitizeAction(payload ActionInvokedPayload) ActionInvokedPayload {
	if payload.Sensitive {
		payload.Arguments = Redaction{Redacted: true, Reason: "sensitive"}
	}
	return payload
}

func sanitizeEffectResult(payload EffectResultPayload) EffectResultPayload {
	if payload.Sensitive {
		payload.Value = Redaction{Redacted: true, Reason: "sensitive"}
		payload.Error = "effect execution failed"
	}
	return payload
}

func decodePayload(kind Kind, raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	switch kind {
	case Key:
		var payload KeyPayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case Text:
		var payload TextPayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case Resize:
		var payload ResizePayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case Pointer:
		var payload PointerPayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case ActionInvoked:
		var payload ActionInvokedPayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case EffectResult:
		var payload EffectResultPayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case Diagnostic:
		var payload DiagnosticPayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case SecureInput:
		var payload SecureInputPayload
		if err := decodeJSON(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	case Focus, Blur, Tick, Cancel, Shutdown:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported event kind %q", kind)
	}
}

func decodeJSON(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}

func validateTextFields(fields ...string) error {
	for _, field := range fields {
		if field == "" {
			continue
		}
		if err := validateText(field); err != nil {
			return err
		}
	}
	return nil
}

func validateText(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("invalid UTF-8")
	}
	if len(value) > MaxPayloadTextBytes {
		return errors.New("payload text exceeds supported bounds")
	}
	if strings.ContainsAny(value, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f") {
		return errors.New("payload text contains control characters")
	}
	return nil
}
