package input

import (
	"errors"
	"testing"

	staveevent "github.com/ben-ranford/stave/event"
)

func TestNormalizeTextRemovesControlInjection(t *testing.T) {
	text, diagnostics, err := NormalizeText("ok\x1b[31m\x00done", 64)
	if err != nil {
		t.Fatalf("normalize text: %v", err)
	}
	if text.Value != "ok[31mdone" {
		t.Fatalf("unexpected normalized text: %q", text.Value)
	}
	if !hasCode(diagnostics, "INPUT_CONTROL_REMOVED") {
		t.Fatal("expected control-removal diagnostic")
	}
}

func TestNormalizePasteTruncatesToConfiguredLimit(t *testing.T) {
	text, diagnostics, err := NormalizePaste([]byte("abcdef"), 4)
	if err != nil {
		t.Fatalf("normalize paste: %v", err)
	}
	if text.Value != "abcd" {
		t.Fatalf("unexpected truncated value: %q", text.Value)
	}
	if !text.Truncated || !hasCode(diagnostics, "INPUT_TRUNCATED") {
		t.Fatal("expected truncation diagnostic")
	}
}

func TestParseKeyNormalizesRunesAndModifiers(t *testing.T) {
	chord, err := ParseKey("Ctrl+Shift+J")
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if chord.Code != KeyRune || chord.Rune != 'J' {
		t.Fatalf("unexpected chord: %#v", chord)
	}
	if chord.Mods != (ModCtrl | ModShift) {
		t.Fatalf("unexpected modifiers: %v", chord.Mods)
	}
}

func TestMemorySecretStoreOnlyExposesOpaqueHandle(t *testing.T) {
	store := NewMemorySecretStore()
	handle, err := store.Put([]byte("secret"))
	if err != nil {
		t.Fatalf("put secret: %v", err)
	}
	if handle.ID == "" {
		t.Fatal("expected opaque handle id")
	}
	var used string
	if err := store.Use(handle, func(value []byte) error {
		used = string(value)
		return nil
	}); err != nil {
		t.Fatalf("use secret: %v", err)
	}
	if used != "secret" {
		t.Fatalf("unexpected secret material: %q", used)
	}
	if err := store.Use(handle, func([]byte) error { return nil }); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected single-use handle, got %v", err)
	}
	if err := store.Destroy(handle); err != nil {
		t.Fatalf("destroy secret: %v", err)
	}
}

func TestEventConstructorsUseCanonicalPayloads(t *testing.T) {
	key := KeyEvent(KeyChord{Code: KeyEnter})
	if key.Kind != staveevent.Key {
		t.Fatalf("unexpected key event kind: %s", key.Kind)
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("key event invalid: %v", err)
	}
	if payload, ok := key.Payload.(staveevent.KeyPayload); !ok || payload.Key != string(KeyEnter) {
		t.Fatalf("unexpected key payload: %#v", key.Payload)
	}

	resize := ResizeEvent(80, 24)
	if err := resize.Validate(); err != nil {
		t.Fatalf("resize event invalid: %v", err)
	}
	payload, ok := resize.Payload.(staveevent.ResizePayload)
	if !ok || payload.Width != 80 || payload.Height != 24 {
		t.Fatalf("unexpected resize payload: %#v", resize.Payload)
	}

	if signal := SignalEvent("INT"); signal.Kind != staveevent.Shutdown || signal.Payload != nil {
		t.Fatalf("unexpected signal event: %#v", signal)
	}
	if eof := EOFEvent(); eof.Kind != staveevent.Shutdown || eof.Payload != nil {
		t.Fatalf("unexpected EOF event: %#v", eof)
	}
	secretEvent := SecretInputEvent(SecretHandle{ID: "opaque"})
	if err := secretEvent.Validate(); err != nil {
		t.Fatalf("secret event invalid: %v", err)
	}
	if secretEvent.Kind != SecretEventKind || secretEvent.Kind == staveevent.Diagnostic {
		t.Fatalf("secret input must be a typed event: %#v", secretEvent)
	}
	if _, ok := secretEvent.Payload.(staveevent.SecureInputPayload); !ok {
		t.Fatalf("unexpected secret payload: %#v", secretEvent.Payload)
	}
}
