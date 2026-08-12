package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/diag"
	"github.com/ben-ranford/stave/semantic"
)

const Version = "1.0"
const JSONRPC = "2.0"

// ID is the exact JSON-RPC id token. It is deliberately not decoded to float64.
type ID json.RawMessage

func (i ID) Valid() bool {
	if len(i) == 0 || bytes.Equal(i, []byte("null")) {
		return false
	}
	if i[0] == '"' {
		var s string
		return json.Unmarshal(i, &s) == nil && s != ""
	}
	var n json.Number
	if err := json.Unmarshal(i, &n); err != nil {
		return false
	}
	return n.String() != ""
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	ParseError           = -32700
	InvalidRequest       = -32600
	MethodNotFound       = -32601
	InvalidParams        = -32602
	InternalError        = -32603
	RequestTooLarge      = -32001
	UnsupportedVersion   = -32002
	Backpressure         = -32003
	Cancelled            = -32004
	DeadlineExceeded     = -32005
	NotInitialized       = -32006
	CapabilityMismatch   = -32007
	ConfirmationRequired = -32008
	ConfirmationInvalid  = -32009
	StaleTarget          = -32011
	ResourceLimit        = -32012
	OutputLimit          = -32013
	Forbidden            = -32014
	ContextCancelled     = -32015
	ContextDeadline      = -32016
	SchemaViolation      = -32017
)

type InitializeParams struct {
	ProtocolVersions []string       `json:"protocolVersions"`
	Client           any            `json:"client,omitempty"`
	Capabilities     map[string]any `json:"capabilities,omitempty"`
	Limits           Limits         `json:"limits,omitempty"`
}
type Limits struct {
	MaxMessageBytes int `json:"maxMessageBytes,omitempty"`
	MaxOutputBytes  int `json:"maxOutputBytes,omitempty"`
	MaxTreeNodes    int `json:"maxTreeNodes,omitempty"`
}

// Application identifies the application whose semantics the Stave server is
// exposing. It is deliberately separate from the Stave server/runtime name so
// one framework binary can host multiple application identities.
type Application struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion  string            `json:"protocolVersion"`
	SessionID        string            `json:"sessionId"`
	Server           string            `json:"server,omitempty"`
	Application      Application       `json:"application"`
	Schemas          map[string]string `json:"schemas,omitempty"`
	Capabilities     any               `json:"capabilities,omitempty"`
	Limits           Limits            `json:"limits"`
	Manifest         any               `json:"manifest,omitempty"`
	ResolvedManifest any               `json:"resolvedManifest,omitempty"`
}
type SnapshotParams struct {
	Mode           string `json:"mode,omitempty"`
	IncludeActions bool   `json:"includeActions,omitempty"`
	SinceRevision  uint64 `json:"sinceRevision,omitempty"`
}
type SnapshotResult struct {
	SchemaVersion   string              `json:"schemaVersion"`
	SessionID       string              `json:"sessionId"`
	Sequence        uint64              `json:"sequence"`
	Mode            string              `json:"mode"`
	Snapshot        *semantic.Snapshot  `json:"snapshot,omitempty"`
	Patch           *semantic.Patch     `json:"patch,omitempty"`
	Revision        uint64              `json:"revision"`
	TreeHash        string              `json:"treeHash"`
	CapabilityHash  string              `json:"capabilityHash"`
	SemanticVersion string              `json:"semanticVersion"`
	ConfigHash      string              `json:"configHash"`
	ThemeHash       string              `json:"themeHash"`
	WidthVersion    string              `json:"widthVersion"`
	Actions         []action.Definition `json:"actions,omitempty"`
	Diagnostics     []diag.Diagnostic   `json:"diagnostics,omitempty"`
}
type InvokeParams struct {
	CallID       string                    `json:"callId"`
	ActionID     string                    `json:"actionId"`
	Target       json.RawMessage           `json:"target,omitempty"`
	Arguments    json.RawMessage           `json:"arguments,omitempty"`
	Confirmation *ConfirmationPresentation `json:"confirmation,omitempty"`
	DeadlineMS   int64                     `json:"deadlineMs,omitempty"`
	SessionID    string                    `json:"sessionId,omitempty"`
}
type ConfirmationPresentation struct {
	Token     string `json:"token"`
	SessionID string `json:"sessionId"`
}
type ConfirmParams struct {
	CallID    string          `json:"callId"`
	ActionID  string          `json:"actionId"`
	Target    json.RawMessage `json:"target,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// InvokeResult is the stable protocol representation of an action result.
// Core action.Result intentionally remains transport-agnostic.
type InvokeResult struct {
	CallID      string              `json:"callId"`
	ActionID    action.ID           `json:"actionId"`
	Target      semantic.Target     `json:"target,omitempty"`
	Status      action.ResultStatus `json:"status"`
	Output      json.RawMessage     `json:"output,omitempty"`
	Revision    uint64              `json:"revision,omitempty"`
	TreeHash    string              `json:"treeHash,omitempty"`
	Diagnostics []diag.Diagnostic   `json:"diagnostics,omitempty"`
}

// ActionErrorData carries only the stable, explicitly safe action context.
// Raw causes and application error strings never cross the protocol boundary.
type ActionErrorData struct {
	StaveCode  action.Code     `json:"staveCode"`
	ActionID   action.ID       `json:"actionId,omitempty"`
	NodeID     semantic.NodeID `json:"nodeId,omitempty"`
	Revision   uint64          `json:"revision,omitempty"`
	Retryable  bool            `json:"retryable"`
	CauseClass string          `json:"causeClass,omitempty"`
}

func Marshal(v any) ([]byte, error) { return json.Marshal(v) }
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
