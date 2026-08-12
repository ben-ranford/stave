package human

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/surface"
)

func TestLineDriverEmitsCanonicalTextAndShutdown(t *testing.T) {
	var output bytes.Buffer
	driver, err := NewLineDriver(LineDriverOptions{Input: strings.NewReader("alpha\nbeta\n"), Output: &output, Width: 40, Height: 10})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := driver.Open(context.Background(), capability.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.TTY || manifest.Interactive || manifest.Color != capability.ColorNone || manifest.OutputMode != capability.OutputPlain {
		t.Fatalf("non-TTY manifest invented capabilities: %+v", manifest)
	}
	var got []event.Event
	for ev := range driver.Events() {
		if err := ev.Validate(); err != nil {
			t.Fatalf("invalid emitted event: %v", err)
		}
		got = append(got, ev)
	}
	if len(got) != 3 || got[0].Payload.(event.TextPayload).Text != "alpha" || got[1].Payload.(event.TextPayload).Text != "beta" || got[2].Kind != event.Shutdown {
		t.Fatalf("unexpected line events: %#v", got)
	}
}

func TestLineDriverDrawUsesSafePlainWriter(t *testing.T) {
	var output bytes.Buffer
	driver, err := NewLineDriver(LineDriverOptions{Output: &output, Width: 8, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Open(context.Background(), capability.Policy{}); err != nil {
		t.Fatal(err)
	}
	s := surface.New(8, 1)
	nodeID := semantic.NodeID("n1_aaaaaaaaaaaaaaaaaaaaaaaaaa")
	s, err = s.WithText(0, 0, "ready", surface.ResolvedStyle{Foreground: "#ff0000"}, "", nodeID, 1, layout.Rect{Width: 8, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Draw(context.Background(), s, surface.Patch{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") || !strings.Contains(output.String(), "ready") {
		t.Fatalf("unsafe or missing plain output: %q", output.String())
	}
}

func TestLineDriverDoesNotInventUnsupportedTTYCapabilities(t *testing.T) {
	driver, err := NewLineDriver(LineDriverOptions{
		Output:      &bytes.Buffer{},
		TTY:         true,
		Width:       80,
		Height:      24,
		Environment: map[string]string{"TERM": "xterm-256color"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := driver.Open(context.Background(), capability.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.TTY || !manifest.Interactive || manifest.CursorAddressing || manifest.AlternateScreen || manifest.Mouse || manifest.BracketedPaste {
		t.Fatalf("line driver invented unsupported tty capabilities: %+v", manifest)
	}
}
