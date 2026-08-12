package event

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKindsStayDistinct(t *testing.T) {
	seen := map[Kind]bool{}
	for _, kind := range []Kind{Key, Text, Resize, Pointer, Focus, Blur, ActionInvoked, EffectResult, Tick, Cancel, Shutdown, Diagnostic} {
		if seen[kind] {
			t.Fatalf("duplicate kind %q", kind)
		}
		seen[kind] = true
	}
}

func TestKeyPayloadRequiresCanonicalPresentation(t *testing.T) {
	invalid := []KeyPayload{
		{},
		{Key: "a"},
		{Key: "rune"},
		{Key: "enter", Rune: 'a'},
		{Key: "rune", Rune: '\n'},
		{Key: "enter", Modifiers: []string{"control"}},
		{Key: "enter", Modifiers: []string{"ctrl", "ctrl"}},
	}
	for _, payload := range invalid {
		if _, err := New(Key, payload); err == nil {
			t.Fatalf("invalid key payload accepted: %#v", payload)
		}
	}
	for _, payload := range []KeyPayload{{Key: "enter"}, {Key: "f12"}, {Key: "rune", Rune: 'é', Modifiers: []string{"ctrl"}}} {
		if _, err := New(Key, payload); err != nil {
			t.Fatalf("canonical key payload rejected: %#v: %v", payload, err)
		}
	}
}

func TestSensitivePayloadsAreRedactedOnSerialization(t *testing.T) {
	ev, err := New(ActionInvoked, ActionInvokedPayload{
		CallID:    "call-1",
		ActionID:  "action.open.v1",
		Arguments: map[string]any{"token": "secret"},
		Sensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatalf("serialized event leaked sensitive content: %s", data)
	}

	var roundTrip Event
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	payload := roundTrip.Payload.(ActionInvokedPayload)
	redacted, ok := payload.Arguments.(map[string]any)
	if !ok || redacted["redacted"] != true {
		t.Fatalf("expected redacted payload, got %#v", payload.Arguments)
	}
}

func TestEffectResultRoundTripAndHashDeterminism(t *testing.T) {
	ev, err := New(EffectResult, EffectResultPayload{
		CallID:  "call-1",
		Ordinal: 2,
		Lane:    "default",
		Status:  "completed",
		Value:   map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev = ev.WithAccepted(4, 3)

	firstHash, err := ev.HashString()
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := ev.HashString()
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("hash changed between runs: %s != %s", firstHash, secondHash)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !Equal(ev, decoded) {
		t.Fatalf("roundtrip changed event:\nwant=%#v\ngot=%#v", ev, decoded)
	}
}

func TestCoalescingKeysOnlyApplyToResizeAndPointer(t *testing.T) {
	resize, err := New(Resize, ResizePayload{Width: 100, Height: 40})
	if err != nil {
		t.Fatal(err)
	}
	if got := resize.CoalescingKey(); got != "resize" {
		t.Fatalf("unexpected resize coalescing key %q", got)
	}

	pointer, err := New(Pointer, PointerPayload{SourceID: "mouse-1", X: 10, Y: 20})
	if err != nil {
		t.Fatal(err)
	}
	if got := pointer.CoalescingKey(); got != "pointer:mouse-1" {
		t.Fatalf("unexpected pointer coalescing key %q", got)
	}

	key, err := New(Key, KeyPayload{Key: "enter"})
	if err != nil {
		t.Fatal(err)
	}
	if key.Coalescible() || key.CoalescingKey() != "" {
		t.Fatalf("key event must not be coalescible: %#v", key)
	}
}

func TestHostileBoundsFailClosed(t *testing.T) {
	if _, err := New(Resize, ResizePayload{Width: MaxViewportDimension + 1, Height: 1}); err == nil {
		t.Fatal("expected oversized resize to fail")
	}
	if _, err := New(Text, TextPayload{Text: string([]byte{0xff})}); err == nil {
		t.Fatal("expected invalid UTF-8 to fail")
	}
}

func TestClonePreservesJSONNumberInActionArguments(t *testing.T) {
	ev, err := New(ActionInvoked, ActionInvokedPayload{CallID: "c", ActionID: "a", Arguments: map[string]any{"n": json.Number("9007199254740993")}})
	if err != nil {
		t.Fatal(err)
	}
	clone, err := ev.Clone()
	if err != nil {
		t.Fatal(err)
	}
	args := clone.Payload.(ActionInvokedPayload).Arguments.(map[string]any)
	if got, ok := args["n"].(json.Number); !ok || got.String() != "9007199254740993" {
		t.Fatalf("number changed type/value: %#v", args["n"])
	}
}

func TestEventUnmarshalRejectsUnknownTopLevelFields(t *testing.T) {
	var ev Event
	if err := json.Unmarshal([]byte(`{"schemaVersion":"stave.event/v1","kind":"key","payload":{"key":"enter"},"unexpected":true}`), &ev); err == nil {
		t.Fatal("unknown event envelope field was accepted")
	}
}

func TestEventUnmarshalRejectsUnknownPayloadFields(t *testing.T) {
	var ev Event
	if err := json.Unmarshal([]byte(`{"schemaVersion":"stave.event/v1","kind":"key","payload":{"key":"enter","unexpected":true}}`), &ev); err == nil {
		t.Fatal("unknown event payload field was accepted")
	}
}
