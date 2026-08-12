package diag

import (
	"encoding/json"
	"github.com/ben-ranford/stave/semantic"
	"time"
)

type Severity string

const (
	Info          Severity = "info"
	Warning       Severity = "warning"
	ErrorSeverity Severity = "error"
)

type Diagnostic struct {
	SchemaVersion string            `json:"schemaVersion"`
	ID            string            `json:"id"`
	Sequence      uint64            `json:"sequence,omitempty"`
	Revision      uint64            `json:"revision,omitempty"`
	Time          time.Time         `json:"time,omitempty"`
	Severity      Severity          `json:"severity"`
	Category      string            `json:"category,omitempty"`
	Code          string            `json:"code,omitempty"`
	Message       string            `json:"message,omitempty"`
	NodeID        semantic.NodeID   `json:"nodeId,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	Retryable     bool              `json:"retryable,omitempty"`
	Redacted      bool              `json:"redacted,omitempty"`
}

func (d Diagnostic) MarshalJSON() ([]byte, error) {
	type alias Diagnostic
	x := d
	if x.Redacted {
		x.Message = "[REDACTED]"
		x.Attributes = nil
	}
	return json.Marshal(alias(x))
}
