package observer

import (
	"context"
	"time"
)

type Event struct {
	Name       string            `json:"name"`
	Time       time.Time         `json:"time"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Redacted   bool              `json:"redacted,omitempty"`
}
type Observer interface{ Observe(context.Context, Event) }
type Nop struct{}

func (Nop) Observe(context.Context, Event) {}

type Func func(context.Context, Event)

func (f Func) Observe(ctx context.Context, e Event) { f(ctx, e) }
