package config

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	schemaconfig "github.com/ben-ranford/stave/schema/config"
)

func TestDefaultsValidateAndHash(t *testing.T) {
	c := Defaults()
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	first, second := HashString(c), HashString(c)
	if first != second {
		t.Fatal("hash not deterministic")
	}
	if !strings.HasPrefix(CanonicalVersion(c), schemaconfig.Version+"+") {
		t.Fatalf("unexpected canonical version: %s", CanonicalVersion(c))
	}
}

func TestParseRejectsBadSchema(t *testing.T) {
	if _, err := Parse([]byte(`{"schemaVersion":"bad"}`)); err == nil {
		t.Fatal("expected schema error")
	}
}

func TestParseRejectsUnknownKeyAndType(t *testing.T) {
	if _, err := Parse([]byte(`{"schemaVersion":"stave.config/v1","theme":{"mode":"dark","bogus":true}}`)); err == nil {
		t.Fatal("expected unknown key rejection")
	}
	if _, err := Parse([]byte(`{"schemaVersion":"stave.config/v1","viewport":{"width":"wide","height":20}}`)); err == nil {
		t.Fatal("expected type rejection")
	}
}

func TestParseRejectsTrailingJSON(t *testing.T) {
	if _, err := ParseLayer([]byte(`{"schemaVersion":"stave.config/v1"} {}`)); err == nil {
		t.Fatal("accepted trailing config JSON")
	}
}

func TestMergeConfigLayersAndCloneBindings(t *testing.T) {
	base := Defaults()
	base.Keymap.Bindings = []string{"base"}
	brand := Config{Theme: Theme{ID: "brand", Mode: "dark"}, Keymap: Keymap{Profile: "brand"}}
	app := Config{App: App{ID: "app"}, Viewport: Viewport{Width: 100, Height: 40}}
	merged := Merge(base, brand, app)
	base.Keymap.Bindings[0] = "mutated"
	if merged.Theme.ID != "brand" || merged.Theme.Density != "comfortable" || merged.App.ID != "app" || merged.Viewport.Width != 100 || merged.Keymap.Bindings[0] != "base" {
		t.Fatalf("sparse merge/clone failed: %+v", merged)
	}
	layered, err := MergeLayers(merged)
	if err != nil {
		t.Fatal(err)
	}
	merged.Keymap.Bindings[0] = "again"
	if layered.Keymap.Bindings[0] != "base" {
		t.Fatal("MergeLayers retained bindings alias")
	}
}

func TestMergeLayersPrecedence(t *testing.T) {
	fileLayer, err := ParseLayer([]byte(`{
		"schemaVersion":"stave.config/v1",
		"theme":{"id":"file-theme","mode":"light","density":"dense"},
		"viewport":{"width":100,"height":40},
		"protocol":{"transport":"stdio-json","enabled":true}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	envLayer, err := LayerFromEnv(map[string]string{
		"STAVE_THEME_MODE":                 "dark",
		"STAVE_VIEWPORT_WIDTH":             "120",
		"STAVE_VIEWPORT_HEIGHT":            "50",
		"STAVE_PROTOCOL_MAX_MESSAGE_BYTES": "12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	flagLayer, err := LayerFromFlags(map[string]string{
		"theme.id":           "flag-theme",
		"runtime.mode":       "terminal",
		"protocol.transport": "stdio-jsonl",
	})
	if err != nil {
		t.Fatal(err)
	}

	explicit := Defaults()
	explicit.Theme.ID = "explicit-theme"
	got, err := MergeLayers(explicit, fileLayer, envLayer, flagLayer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme.ID != "flag-theme" || got.Theme.Mode != "dark" {
		t.Fatalf("theme precedence failed: %+v", got.Theme)
	}
	if got.Viewport.Width != 120 || got.Viewport.Height != 50 {
		t.Fatalf("viewport precedence failed: %+v", got.Viewport)
	}
	if got.Protocol.Transport != "stdio-jsonl" || got.Protocol.MaxMessageBytes != 12345 {
		t.Fatalf("protocol precedence failed: %+v", got.Protocol)
	}
}

func TestValidateCrossFieldAndRangeErrors(t *testing.T) {
	cfg := Defaults()
	cfg.Viewport.Width = 80
	cfg.Runtime.InputQueue = 0
	cfg.Protocol.Transport = "none"
	cfg.Security.ConfirmationTTL = "zero"

	var verr ValidationError
	if err := Validate(cfg); !errors.As(err, &verr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if len(verr.Problems) < 3 {
		t.Fatalf("expected multiple problems, got %+v", verr.Problems)
	}
}

func TestCanonicalRoundTripAndHashDeterminism(t *testing.T) {
	cfg := Defaults()
	cfg.Theme.ID = "brand"
	cfg.Viewport = Viewport{Width: 100, Height: 40}
	data := CanonicalJSON(cfg)
	roundTrip, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, CanonicalJSON(roundTrip)) {
		t.Fatalf("canonical roundtrip mismatch:\n%s\n%s", data, CanonicalJSON(roundTrip))
	}
	if HashString(cfg) != HashString(roundTrip) {
		t.Fatalf("hash mismatch: %s != %s", HashString(cfg), HashString(roundTrip))
	}
}

func TestLayerHelpersRejectUnknownKeys(t *testing.T) {
	if _, err := LayerFromEnv(map[string]string{"STAVE_UNKNOWN": "1"}); err == nil {
		t.Fatal("expected env key rejection")
	}
	if _, err := LayerFromFlags(map[string]string{"unknown.path": "1"}); err == nil {
		t.Fatal("expected flag key rejection")
	}
}

func TestSchemaFreshness(t *testing.T) {
	if schemaconfig.Version != Defaults().SchemaVersion {
		t.Fatalf("schema version drift: %s != %s", schemaconfig.Version, Defaults().SchemaVersion)
	}
	if !slices.Contains(schemaconfig.JSONPaths, "theme.mode") || !slices.Contains(schemaconfig.EnvKeys, "STAVE_THEME_MODE") || !slices.Contains(schemaconfig.FlagKeys, "theme.mode") {
		t.Fatalf("schema metadata incomplete: %+v %+v %+v", schemaconfig.JSONPaths, schemaconfig.EnvKeys, schemaconfig.FlagKeys)
	}
}

func TestConfigDiagnosticsRedactValues(t *testing.T) {
	_, err := LayerFromEnv(map[string]string{"STAVE_SECURITY_CONFIRMATION_TTL": "not-a-duration"})
	if err != nil {
		t.Fatalf("layer building should accept raw strings: %v", err)
	}

	cfg := Defaults()
	cfg.Security.ConfirmationTTL = "not-a-duration"
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), "not-a-duration") {
		t.Fatalf("validation leaked field contents: %v", err)
	}
}

func TestConfigConcurrentCanonicalAccess(t *testing.T) {
	cfg := Defaults()
	cfg.Theme.ID = "brand"
	cfg.Viewport = Viewport{Width: 120, Height: 50}

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if len(CanonicalJSON(cfg)) == 0 {
				t.Error("canonical JSON empty")
			}
			if HashString(cfg) == "" {
				t.Error("hash empty")
			}
		}()
	}
	wg.Wait()
}
