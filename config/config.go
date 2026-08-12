package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	itokens "github.com/ben-ranford/stave/internal/tokens"
	schemaconfig "github.com/ben-ranford/stave/schema/config"
)

type Config struct {
	SchemaVersion string       `json:"schemaVersion"`
	App           App          `json:"app"`
	Theme         Theme        `json:"theme"`
	Viewport      Viewport     `json:"viewport"`
	Capabilities  Capabilities `json:"capabilities"`
	Keymap        Keymap       `json:"keymap"`
	Runtime       Runtime      `json:"runtime"`
	Protocol      Protocol     `json:"protocol"`
	Security      Security     `json:"security"`
	Diagnostics   Diagnostics  `json:"diagnostics"`
}

type App struct {
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
}

type Theme struct {
	ID      string `json:"id,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Density string `json:"density,omitempty"`
}

type Viewport struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

type Capabilities struct {
	Color           string `json:"color,omitempty"`
	Unicode         string `json:"unicode,omitempty"`
	Motion          string `json:"motion,omitempty"`
	Mouse           string `json:"mouse,omitempty"`
	AlternateScreen string `json:"alternateScreen,omitempty"`
}

type Keymap struct {
	Profile  string   `json:"profile,omitempty"`
	Bindings []string `json:"bindings,omitempty"`
}

type Runtime struct {
	Mode           string `json:"mode,omitempty"`
	InputQueue     int    `json:"inputQueue,omitempty"`
	ActionQueue    int    `json:"actionQueue,omitempty"`
	RestoreOnPanic bool   `json:"restoreOnPanic,omitempty"`
}

type Protocol struct {
	Enabled         bool   `json:"enabled,omitempty"`
	Transport       string `json:"transport,omitempty"`
	MaxMessageBytes int    `json:"maxMessageBytes,omitempty"`
}

type Security struct {
	AllowClipboard          bool   `json:"allowClipboard,omitempty"`
	AllowCoordinateFallback bool   `json:"allowCoordinateFallback,omitempty"`
	ConfirmationTTL         string `json:"confirmationTTL,omitempty"`
	MaxTreeNodes            int    `json:"maxTreeNodes,omitempty"`
}

type Diagnostics struct {
	Level  string `json:"level,omitempty"`
	Format string `json:"format,omitempty"`
}

type Problem struct {
	Source  string `json:"source"`
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationError struct {
	Problems []Problem `json:"problems"`
}

func (e ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid config"
	}
	return fmt.Sprintf("%s: %s", e.Problems[0].Path, e.Problems[0].Message)
}

type Layer struct {
	source string
	value  configLayer
}

type configLayer struct {
	SchemaVersion *string            `json:"schemaVersion,omitempty"`
	App           *appLayer          `json:"app,omitempty"`
	Theme         *themeLayer        `json:"theme,omitempty"`
	Viewport      *viewportLayer     `json:"viewport,omitempty"`
	Capabilities  *capabilitiesLayer `json:"capabilities,omitempty"`
	Keymap        *keymapLayer       `json:"keymap,omitempty"`
	Runtime       *runtimeLayer      `json:"runtime,omitempty"`
	Protocol      *protocolLayer     `json:"protocol,omitempty"`
	Security      *securityLayer     `json:"security,omitempty"`
	Diagnostics   *diagnosticsLayer  `json:"diagnostics,omitempty"`
}

type appLayer struct {
	ID      *string `json:"id,omitempty"`
	Version *string `json:"version,omitempty"`
}

type themeLayer struct {
	ID      *string `json:"id,omitempty"`
	Mode    *string `json:"mode,omitempty"`
	Density *string `json:"density,omitempty"`
}

type viewportLayer struct {
	Width  *int `json:"width,omitempty"`
	Height *int `json:"height,omitempty"`
}

type capabilitiesLayer struct {
	Color           *string `json:"color,omitempty"`
	Unicode         *string `json:"unicode,omitempty"`
	Motion          *string `json:"motion,omitempty"`
	Mouse           *string `json:"mouse,omitempty"`
	AlternateScreen *string `json:"alternateScreen,omitempty"`
}

type keymapLayer struct {
	Profile  *string  `json:"profile,omitempty"`
	Bindings []string `json:"bindings,omitempty"`
}

type runtimeLayer struct {
	Mode           *string `json:"mode,omitempty"`
	InputQueue     *int    `json:"inputQueue,omitempty"`
	ActionQueue    *int    `json:"actionQueue,omitempty"`
	RestoreOnPanic *bool   `json:"restoreOnPanic,omitempty"`
}

type protocolLayer struct {
	Enabled         *bool   `json:"enabled,omitempty"`
	Transport       *string `json:"transport,omitempty"`
	MaxMessageBytes *int    `json:"maxMessageBytes,omitempty"`
}

type securityLayer struct {
	AllowClipboard          *bool   `json:"allowClipboard,omitempty"`
	AllowCoordinateFallback *bool   `json:"allowCoordinateFallback,omitempty"`
	ConfirmationTTL         *string `json:"confirmationTTL,omitempty"`
	MaxTreeNodes            *int    `json:"maxTreeNodes,omitempty"`
}

type diagnosticsLayer struct {
	Level  *string `json:"level,omitempty"`
	Format *string `json:"format,omitempty"`
}

func Defaults() Config {
	return Config{
		SchemaVersion: schemaconfig.Version,
		Theme:         Theme{Mode: "auto", Density: "comfortable"},
		Capabilities:  Capabilities{Color: "auto", Unicode: "auto", Motion: "auto", Mouse: "auto", AlternateScreen: "auto"},
		Runtime:       Runtime{Mode: "auto", InputQueue: 256, ActionQueue: 64, RestoreOnPanic: true},
		Protocol:      Protocol{Enabled: true, Transport: "stdio-jsonl", MaxMessageBytes: 4 << 20},
		Security:      Security{ConfirmationTTL: "60s", MaxTreeNodes: 100000},
		Diagnostics:   Diagnostics{Level: "warn", Format: "text"},
	}
}

func Parse(data []byte) (Config, error) {
	layer, err := ParseLayer(data)
	if err != nil {
		return Config{}, err
	}
	return MergeLayers(Defaults(), layer)
}

func ParseLayer(data []byte) (Layer, error) {
	var raw configLayer
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Layer{}, ValidationError{Problems: []Problem{{Source: "file", Path: "document", Code: "invalid_json", Message: "config JSON must match the v1 schema"}}}
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Layer{}, ValidationError{Problems: []Problem{{Source: "file", Path: "document", Code: "trailing_json", Message: "config JSON must contain exactly one object"}}}
	}
	return Layer{source: "file", value: raw}, nil
}

func LayerFromEnv(env map[string]string) (Layer, error) {
	layer := Layer{source: "env"}
	keys := make([]string, 0, len(env))
	for key := range env {
		if strings.HasPrefix(key, "STAVE_") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := env[key]
		if err := applyKeyValue(&layer.value, "env", key, value); err != nil {
			return Layer{}, err
		}
	}
	return layer, nil
}

func LayerFromFlags(flags map[string]string) (Layer, error) {
	layer := Layer{source: "flag"}
	keys := make([]string, 0, len(flags))
	for key := range flags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := applyKeyValue(&layer.value, "flag", key, flags[key]); err != nil {
			return Layer{}, err
		}
	}
	return layer, nil
}

func Merge(values ...Config) Config {
	if len(values) == 0 {
		return Defaults()
	}
	out := cloneConfig(values[0])
	for _, next := range values[1:] {
		applyConfig(&out, next)
	}
	return out
}

func MergeLayers(explicit Config, layers ...Layer) (Config, error) {
	out := cloneConfig(explicit)
	for _, layer := range layers {
		applyLayer(&out, layer.value)
	}
	if out.SchemaVersion == "" {
		out.SchemaVersion = schemaconfig.Version
	}
	return out, Validate(out)
}

func Validate(c Config) error {
	problems := make([]Problem, 0, 16)
	add := func(path, code, message string) {
		problems = append(problems, Problem{Source: "config", Path: path, Code: code, Message: message})
	}

	if c.SchemaVersion != schemaconfig.Version {
		add("schemaVersion", "unsupported", "schema version must be stave.config/v1")
	}
	oneOf(add, "theme.mode", c.Theme.Mode, "auto", "light", "dark")
	oneOf(add, "theme.density", c.Theme.Density, "compact", "dense", "comfortable")
	oneOf(add, "capabilities.color", c.Capabilities.Color, "auto", "never", "always", "none", "mono", "ansi16", "ansi256", "truecolor")
	oneOf(add, "capabilities.unicode", c.Capabilities.Unicode, "auto", "none", "ascii", "full")
	oneOf(add, "capabilities.motion", c.Capabilities.Motion, "auto", "full", "reduced")
	oneOf(add, "capabilities.mouse", c.Capabilities.Mouse, "auto", "enabled", "disabled")
	oneOf(add, "capabilities.alternateScreen", c.Capabilities.AlternateScreen, "auto", "enabled", "disabled")
	oneOf(add, "runtime.mode", c.Runtime.Mode, "auto", "terminal", "headless")
	oneOf(add, "protocol.transport", c.Protocol.Transport, "stdio-jsonl", "stdio-json", "none")
	oneOf(add, "diagnostics.level", c.Diagnostics.Level, "debug", "info", "warn", "error", "none")
	oneOf(add, "diagnostics.format", c.Diagnostics.Format, "text", "json")

	if c.Viewport.Width < 0 || c.Viewport.Height < 0 {
		add("viewport", "range", "viewport dimensions must be non-negative")
	}
	if (c.Viewport.Width == 0) != (c.Viewport.Height == 0) {
		add("viewport", "cross_field", "viewport width and height must be provided together")
	}
	if c.Runtime.InputQueue <= 0 || c.Runtime.ActionQueue <= 0 {
		add("runtime", "range", "runtime queue sizes must be positive")
	}
	if c.Protocol.MaxMessageBytes <= 0 {
		add("protocol.maxMessageBytes", "range", "protocol max message bytes must be positive")
	}
	if c.Security.MaxTreeNodes <= 0 {
		add("security.maxTreeNodes", "range", "security max tree nodes must be positive")
	}
	if c.Security.ConfirmationTTL != "" {
		duration, err := time.ParseDuration(c.Security.ConfirmationTTL)
		if err != nil || duration <= 0 {
			add("security.confirmationTTL", "duration", "confirmation TTL must be a positive duration")
		}
	}
	if c.Protocol.Enabled && c.Protocol.Transport == "none" {
		add("protocol", "cross_field", "enabled protocols require a transport")
	}
	if c.Runtime.Mode == "headless" && c.Viewport.Width != 0 {
		add("runtime", "cross_field", "headless mode must not pin an interactive viewport")
	}

	if len(problems) > 0 {
		return ValidationError{Problems: problems}
	}
	return nil
}

func CanonicalJSONE(c Config) ([]byte, error) { return itokens.CanonicalJSON(cloneConfig(c)) }
func CanonicalJSON(c Config) []byte {
	b, err := CanonicalJSONE(c)
	if err != nil {
		panic(fmt.Errorf("canonical config: %w", err))
	}
	return b
}

func Hash(c Config) [32]byte {
	sum, err := itokens.CanonicalHash(cloneConfig(c))
	if err != nil {
		panic(fmt.Errorf("hash config: %w", err))
	}
	return sum
}

func cloneConfig(c Config) Config {
	c.Keymap.Bindings = append([]string(nil), c.Keymap.Bindings...)
	return c
}

func applyConfig(dst *Config, source Config) {
	// Config values are complete application/brand layers. Zero values mean
	// “unspecified” except booleans, where an explicitly non-zero containing
	// section is required to participate. JSON/env/flag sparse layers retain
	// exact pointer semantics through MergeLayers.
	if source.SchemaVersion != "" {
		dst.SchemaVersion = source.SchemaVersion
	}
	if source.App.ID != "" {
		dst.App.ID = source.App.ID
	}
	if source.App.Version != "" {
		dst.App.Version = source.App.Version
	}
	if source.Theme.ID != "" {
		dst.Theme.ID = source.Theme.ID
	}
	if source.Theme.Mode != "" {
		dst.Theme.Mode = source.Theme.Mode
	}
	if source.Theme.Density != "" {
		dst.Theme.Density = source.Theme.Density
	}
	if source.Viewport.Width != 0 {
		dst.Viewport.Width = source.Viewport.Width
	}
	if source.Viewport.Height != 0 {
		dst.Viewport.Height = source.Viewport.Height
	}
	if source.Capabilities.Color != "" {
		dst.Capabilities.Color = source.Capabilities.Color
	}
	if source.Capabilities.Unicode != "" {
		dst.Capabilities.Unicode = source.Capabilities.Unicode
	}
	if source.Capabilities.Motion != "" {
		dst.Capabilities.Motion = source.Capabilities.Motion
	}
	if source.Capabilities.Mouse != "" {
		dst.Capabilities.Mouse = source.Capabilities.Mouse
	}
	if source.Capabilities.AlternateScreen != "" {
		dst.Capabilities.AlternateScreen = source.Capabilities.AlternateScreen
	}
	if source.Keymap.Profile != "" {
		dst.Keymap.Profile = source.Keymap.Profile
	}
	if source.Keymap.Bindings != nil {
		dst.Keymap.Bindings = append([]string(nil), source.Keymap.Bindings...)
	}
	if source.Runtime.Mode != "" {
		dst.Runtime.Mode = source.Runtime.Mode
	}
	if source.Runtime.InputQueue != 0 {
		dst.Runtime.InputQueue = source.Runtime.InputQueue
	}
	if source.Runtime.ActionQueue != 0 {
		dst.Runtime.ActionQueue = source.Runtime.ActionQueue
	}
	if source.Runtime.Mode != "" || source.Runtime.InputQueue != 0 || source.Runtime.ActionQueue != 0 || source.Runtime.RestoreOnPanic {
		dst.Runtime.RestoreOnPanic = source.Runtime.RestoreOnPanic
	}
	if source.Protocol.Transport != "" {
		dst.Protocol.Transport = source.Protocol.Transport
	}
	if source.Protocol.MaxMessageBytes != 0 {
		dst.Protocol.MaxMessageBytes = source.Protocol.MaxMessageBytes
	}
	if source.Protocol.Transport != "" || source.Protocol.MaxMessageBytes != 0 || source.Protocol.Enabled {
		dst.Protocol.Enabled = source.Protocol.Enabled
	}
	if source.Security.ConfirmationTTL != "" {
		dst.Security.ConfirmationTTL = source.Security.ConfirmationTTL
	}
	if source.Security.MaxTreeNodes != 0 {
		dst.Security.MaxTreeNodes = source.Security.MaxTreeNodes
	}
	if source.Security.ConfirmationTTL != "" || source.Security.MaxTreeNodes != 0 || source.Security.AllowClipboard || source.Security.AllowCoordinateFallback {
		dst.Security.AllowClipboard = source.Security.AllowClipboard
		dst.Security.AllowCoordinateFallback = source.Security.AllowCoordinateFallback
	}
	if source.Diagnostics.Level != "" {
		dst.Diagnostics.Level = source.Diagnostics.Level
	}
	if source.Diagnostics.Format != "" {
		dst.Diagnostics.Format = source.Diagnostics.Format
	}
}

func HashString(c Config) string {
	h := Hash(c)
	return fmt.Sprintf("%x", h)
}

func CanonicalVersion(c Config) string {
	hash := HashString(c)
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return c.SchemaVersion + "+" + hash
}

func Keys(c Config) []string {
	out := append([]string(nil), schemaconfig.JSONPaths...)
	sort.Strings(out)
	return out
}

func KnownEnvKeys() []string {
	out := append([]string(nil), schemaconfig.EnvKeys...)
	sort.Strings(out)
	return out
}

func KnownFlagKeys() []string {
	out := append([]string(nil), schemaconfig.FlagKeys...)
	sort.Strings(out)
	return out
}

func applyLayer(dst *Config, layer configLayer) {
	if layer.SchemaVersion != nil {
		dst.SchemaVersion = *layer.SchemaVersion
	}
	if layer.App != nil {
		if layer.App.ID != nil {
			dst.App.ID = *layer.App.ID
		}
		if layer.App.Version != nil {
			dst.App.Version = *layer.App.Version
		}
	}
	if layer.Theme != nil {
		if layer.Theme.ID != nil {
			dst.Theme.ID = *layer.Theme.ID
		}
		if layer.Theme.Mode != nil {
			dst.Theme.Mode = *layer.Theme.Mode
		}
		if layer.Theme.Density != nil {
			dst.Theme.Density = *layer.Theme.Density
		}
	}
	if layer.Viewport != nil {
		if layer.Viewport.Width != nil {
			dst.Viewport.Width = *layer.Viewport.Width
		}
		if layer.Viewport.Height != nil {
			dst.Viewport.Height = *layer.Viewport.Height
		}
	}
	if layer.Capabilities != nil {
		if layer.Capabilities.Color != nil {
			dst.Capabilities.Color = *layer.Capabilities.Color
		}
		if layer.Capabilities.Unicode != nil {
			dst.Capabilities.Unicode = *layer.Capabilities.Unicode
		}
		if layer.Capabilities.Motion != nil {
			dst.Capabilities.Motion = *layer.Capabilities.Motion
		}
		if layer.Capabilities.Mouse != nil {
			dst.Capabilities.Mouse = *layer.Capabilities.Mouse
		}
		if layer.Capabilities.AlternateScreen != nil {
			dst.Capabilities.AlternateScreen = *layer.Capabilities.AlternateScreen
		}
	}
	if layer.Keymap != nil {
		if layer.Keymap.Profile != nil {
			dst.Keymap.Profile = *layer.Keymap.Profile
		}
		if layer.Keymap.Bindings != nil {
			dst.Keymap.Bindings = append([]string(nil), layer.Keymap.Bindings...)
		}
	}
	if layer.Runtime != nil {
		if layer.Runtime.Mode != nil {
			dst.Runtime.Mode = *layer.Runtime.Mode
		}
		if layer.Runtime.InputQueue != nil {
			dst.Runtime.InputQueue = *layer.Runtime.InputQueue
		}
		if layer.Runtime.ActionQueue != nil {
			dst.Runtime.ActionQueue = *layer.Runtime.ActionQueue
		}
		if layer.Runtime.RestoreOnPanic != nil {
			dst.Runtime.RestoreOnPanic = *layer.Runtime.RestoreOnPanic
		}
	}
	if layer.Protocol != nil {
		if layer.Protocol.Enabled != nil {
			dst.Protocol.Enabled = *layer.Protocol.Enabled
		}
		if layer.Protocol.Transport != nil {
			dst.Protocol.Transport = *layer.Protocol.Transport
		}
		if layer.Protocol.MaxMessageBytes != nil {
			dst.Protocol.MaxMessageBytes = *layer.Protocol.MaxMessageBytes
		}
	}
	if layer.Security != nil {
		if layer.Security.AllowClipboard != nil {
			dst.Security.AllowClipboard = *layer.Security.AllowClipboard
		}
		if layer.Security.AllowCoordinateFallback != nil {
			dst.Security.AllowCoordinateFallback = *layer.Security.AllowCoordinateFallback
		}
		if layer.Security.ConfirmationTTL != nil {
			dst.Security.ConfirmationTTL = *layer.Security.ConfirmationTTL
		}
		if layer.Security.MaxTreeNodes != nil {
			dst.Security.MaxTreeNodes = *layer.Security.MaxTreeNodes
		}
	}
	if layer.Diagnostics != nil {
		if layer.Diagnostics.Level != nil {
			dst.Diagnostics.Level = *layer.Diagnostics.Level
		}
		if layer.Diagnostics.Format != nil {
			dst.Diagnostics.Format = *layer.Diagnostics.Format
		}
	}
}

func applyKeyValue(layer *configLayer, source, key, value string) error {
	switch source {
	case "env":
		return applyEnvKey(layer, key, value)
	case "flag":
		return applyFlagKey(layer, key, value)
	default:
		return ValidationError{Problems: []Problem{{Source: source, Path: key, Code: "unsupported_source", Message: "config source is not supported"}}}
	}
}

func applyEnvKey(layer *configLayer, key, value string) error {
	switch key {
	case "STAVE_APP_ID":
		ensureApp(layer).ID = stringp(value)
	case "STAVE_APP_VERSION":
		ensureApp(layer).Version = stringp(value)
	case "STAVE_THEME_ID":
		ensureTheme(layer).ID = stringp(value)
	case "STAVE_THEME_MODE":
		ensureTheme(layer).Mode = stringp(value)
	case "STAVE_THEME_DENSITY":
		ensureTheme(layer).Density = stringp(value)
	case "STAVE_VIEWPORT_WIDTH":
		n, err := parseInt("env", key, value)
		if err != nil {
			return err
		}
		ensureViewport(layer).Width = &n
	case "STAVE_VIEWPORT_HEIGHT":
		n, err := parseInt("env", key, value)
		if err != nil {
			return err
		}
		ensureViewport(layer).Height = &n
	case "STAVE_CAPABILITIES_COLOR":
		ensureCapabilities(layer).Color = stringp(value)
	case "STAVE_CAPABILITIES_UNICODE":
		ensureCapabilities(layer).Unicode = stringp(value)
	case "STAVE_CAPABILITIES_MOTION":
		ensureCapabilities(layer).Motion = stringp(value)
	case "STAVE_CAPABILITIES_MOUSE":
		ensureCapabilities(layer).Mouse = stringp(value)
	case "STAVE_CAPABILITIES_ALTERNATE_SCREEN":
		ensureCapabilities(layer).AlternateScreen = stringp(value)
	case "STAVE_KEYMAP_PROFILE":
		ensureKeymap(layer).Profile = stringp(value)
	case "STAVE_RUNTIME_MODE":
		ensureRuntime(layer).Mode = stringp(value)
	case "STAVE_RUNTIME_INPUT_QUEUE":
		n, err := parseInt("env", key, value)
		if err != nil {
			return err
		}
		ensureRuntime(layer).InputQueue = &n
	case "STAVE_RUNTIME_ACTION_QUEUE":
		n, err := parseInt("env", key, value)
		if err != nil {
			return err
		}
		ensureRuntime(layer).ActionQueue = &n
	case "STAVE_RUNTIME_RESTORE_ON_PANIC":
		b, err := parseBool("env", key, value)
		if err != nil {
			return err
		}
		ensureRuntime(layer).RestoreOnPanic = &b
	case "STAVE_PROTOCOL_ENABLED":
		b, err := parseBool("env", key, value)
		if err != nil {
			return err
		}
		ensureProtocol(layer).Enabled = &b
	case "STAVE_PROTOCOL_TRANSPORT":
		ensureProtocol(layer).Transport = stringp(value)
	case "STAVE_PROTOCOL_MAX_MESSAGE_BYTES":
		n, err := parseInt("env", key, value)
		if err != nil {
			return err
		}
		ensureProtocol(layer).MaxMessageBytes = &n
	case "STAVE_SECURITY_ALLOW_CLIPBOARD":
		b, err := parseBool("env", key, value)
		if err != nil {
			return err
		}
		ensureSecurity(layer).AllowClipboard = &b
	case "STAVE_SECURITY_ALLOW_COORDINATE_FALLBACK":
		b, err := parseBool("env", key, value)
		if err != nil {
			return err
		}
		ensureSecurity(layer).AllowCoordinateFallback = &b
	case "STAVE_SECURITY_CONFIRMATION_TTL":
		ensureSecurity(layer).ConfirmationTTL = stringp(value)
	case "STAVE_SECURITY_MAX_TREE_NODES":
		n, err := parseInt("env", key, value)
		if err != nil {
			return err
		}
		ensureSecurity(layer).MaxTreeNodes = &n
	case "STAVE_DIAGNOSTICS_LEVEL":
		ensureDiagnostics(layer).Level = stringp(value)
	case "STAVE_DIAGNOSTICS_FORMAT":
		ensureDiagnostics(layer).Format = stringp(value)
	default:
		return ValidationError{Problems: []Problem{{Source: "env", Path: key, Code: "unknown_key", Message: "unknown config environment key"}}}
	}
	return nil
}

func applyFlagKey(layer *configLayer, key, value string) error {
	switch key {
	case "app.id":
		ensureApp(layer).ID = stringp(value)
	case "app.version":
		ensureApp(layer).Version = stringp(value)
	case "theme.id":
		ensureTheme(layer).ID = stringp(value)
	case "theme.mode":
		ensureTheme(layer).Mode = stringp(value)
	case "theme.density":
		ensureTheme(layer).Density = stringp(value)
	case "viewport.width":
		n, err := parseInt("flag", key, value)
		if err != nil {
			return err
		}
		ensureViewport(layer).Width = &n
	case "viewport.height":
		n, err := parseInt("flag", key, value)
		if err != nil {
			return err
		}
		ensureViewport(layer).Height = &n
	case "capabilities.color":
		ensureCapabilities(layer).Color = stringp(value)
	case "capabilities.unicode":
		ensureCapabilities(layer).Unicode = stringp(value)
	case "capabilities.motion":
		ensureCapabilities(layer).Motion = stringp(value)
	case "capabilities.mouse":
		ensureCapabilities(layer).Mouse = stringp(value)
	case "capabilities.alternateScreen":
		ensureCapabilities(layer).AlternateScreen = stringp(value)
	case "keymap.profile":
		ensureKeymap(layer).Profile = stringp(value)
	case "runtime.mode":
		ensureRuntime(layer).Mode = stringp(value)
	case "runtime.inputQueue":
		n, err := parseInt("flag", key, value)
		if err != nil {
			return err
		}
		ensureRuntime(layer).InputQueue = &n
	case "runtime.actionQueue":
		n, err := parseInt("flag", key, value)
		if err != nil {
			return err
		}
		ensureRuntime(layer).ActionQueue = &n
	case "runtime.restoreOnPanic":
		b, err := parseBool("flag", key, value)
		if err != nil {
			return err
		}
		ensureRuntime(layer).RestoreOnPanic = &b
	case "protocol.enabled":
		b, err := parseBool("flag", key, value)
		if err != nil {
			return err
		}
		ensureProtocol(layer).Enabled = &b
	case "protocol.transport":
		ensureProtocol(layer).Transport = stringp(value)
	case "protocol.maxMessageBytes":
		n, err := parseInt("flag", key, value)
		if err != nil {
			return err
		}
		ensureProtocol(layer).MaxMessageBytes = &n
	case "security.allowClipboard":
		b, err := parseBool("flag", key, value)
		if err != nil {
			return err
		}
		ensureSecurity(layer).AllowClipboard = &b
	case "security.allowCoordinateFallback":
		b, err := parseBool("flag", key, value)
		if err != nil {
			return err
		}
		ensureSecurity(layer).AllowCoordinateFallback = &b
	case "security.confirmationTTL":
		ensureSecurity(layer).ConfirmationTTL = stringp(value)
	case "security.maxTreeNodes":
		n, err := parseInt("flag", key, value)
		if err != nil {
			return err
		}
		ensureSecurity(layer).MaxTreeNodes = &n
	case "diagnostics.level":
		ensureDiagnostics(layer).Level = stringp(value)
	case "diagnostics.format":
		ensureDiagnostics(layer).Format = stringp(value)
	default:
		return ValidationError{Problems: []Problem{{Source: "flag", Path: key, Code: "unknown_key", Message: "unknown config flag key"}}}
	}
	return nil
}

func parseInt(source, key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, ValidationError{Problems: []Problem{{Source: source, Path: key, Code: "invalid_type", Message: "config integer field requires an integer value"}}}
	}
	return n, nil
}

func parseBool(source, key, value string) (bool, error) {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, ValidationError{Problems: []Problem{{Source: source, Path: key, Code: "invalid_type", Message: "config boolean field requires a boolean value"}}}
	}
	return b, nil
}

func oneOf(add func(path, code, message string), path, value string, allowed ...string) {
	if value == "" {
		return
	}
	for _, candidate := range allowed {
		if value == candidate {
			return
		}
	}
	add(path, "invalid_value", "field must use a supported enum value")
}

func ensureApp(layer *configLayer) *appLayer {
	if layer.App == nil {
		layer.App = &appLayer{}
	}
	return layer.App
}

func ensureTheme(layer *configLayer) *themeLayer {
	if layer.Theme == nil {
		layer.Theme = &themeLayer{}
	}
	return layer.Theme
}

func ensureViewport(layer *configLayer) *viewportLayer {
	if layer.Viewport == nil {
		layer.Viewport = &viewportLayer{}
	}
	return layer.Viewport
}

func ensureCapabilities(layer *configLayer) *capabilitiesLayer {
	if layer.Capabilities == nil {
		layer.Capabilities = &capabilitiesLayer{}
	}
	return layer.Capabilities
}

func ensureKeymap(layer *configLayer) *keymapLayer {
	if layer.Keymap == nil {
		layer.Keymap = &keymapLayer{}
	}
	return layer.Keymap
}

func ensureRuntime(layer *configLayer) *runtimeLayer {
	if layer.Runtime == nil {
		layer.Runtime = &runtimeLayer{}
	}
	return layer.Runtime
}

func ensureProtocol(layer *configLayer) *protocolLayer {
	if layer.Protocol == nil {
		layer.Protocol = &protocolLayer{}
	}
	return layer.Protocol
}

func ensureSecurity(layer *configLayer) *securityLayer {
	if layer.Security == nil {
		layer.Security = &securityLayer{}
	}
	return layer.Security
}

func ensureDiagnostics(layer *configLayer) *diagnosticsLayer {
	if layer.Diagnostics == nil {
		layer.Diagnostics = &diagnosticsLayer{}
	}
	return layer.Diagnostics
}

func stringp(v string) *string { return &v }
