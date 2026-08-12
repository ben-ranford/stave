package input

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	staveevent "github.com/ben-ranford/stave/event"
	coresecret "github.com/ben-ranford/stave/secret"
)

type KeyCode string
type Mod uint8
type TextSource string

const (
	ModShift Mod = 1 << iota
	ModCtrl
	ModAlt
	ModMeta
)

const (
	KeyRune      KeyCode = "rune"
	KeyEnter     KeyCode = "enter"
	KeyEscape    KeyCode = "escape"
	KeyTab       KeyCode = "tab"
	KeyBackspace KeyCode = "backspace"
	KeyDelete    KeyCode = "delete"
	KeySpace     KeyCode = "space"
	KeyUp        KeyCode = "up"
	KeyDown      KeyCode = "down"
	KeyLeft      KeyCode = "left"
	KeyRight     KeyCode = "right"
	KeyHome      KeyCode = "home"
	KeyEnd       KeyCode = "end"
	KeyPageUp    KeyCode = "pageup"
	KeyPageDown  KeyCode = "pagedown"
)

const (
	SourceTyped TextSource = "typed"
	SourcePaste TextSource = "paste"
)

const (
	DefaultMaxTextBytes  = 4096
	DefaultMaxPasteBytes = 65536
)

const SecretEventKind staveevent.Kind = staveevent.SecureInput

var ErrSecretNotFound = coresecret.ErrUnavailable

type KeyChord struct {
	Code KeyCode `json:"code"`
	Rune rune    `json:"rune,omitempty"`
	Mods Mod     `json:"mods,omitempty"`
}

type Text struct {
	Value     string     `json:"value"`
	Source    TextSource `json:"source"`
	Truncated bool       `json:"truncated,omitempty"`
}

type Resize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Signal struct {
	Name string `json:"name"`
}

type EOF struct{}

type SecretPrompt struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type SecretHandle = coresecret.Handle
type SecretStore = coresecret.Store

type SecureProvider interface {
	ReadSecret(context.Context, SecretPrompt) (SecretHandle, error)
}

type Diagnostic struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type MemorySecretStore = coresecret.MemoryStore

func (k KeyChord) Normalize() KeyChord {
	out := KeyChord{Code: k.Code, Rune: k.Rune, Mods: k.Mods}
	switch out.Code {
	case "", KeyRune:
		if out.Rune == 0 {
			return KeyChord{}
		}
		out.Code = KeyRune
		// Printable rune identity is semantic data; preserve case.
		if out.Rune == ' ' {
			out.Code = KeySpace
			out.Rune = 0
		}
	case KeySpace:
		out.Rune = 0
	}
	if out.Code != KeyRune {
		out.Rune = 0
	}
	return out
}

func ParseKey(raw string) (KeyChord, error) {
	parts := strings.Split(strings.TrimSpace(raw), "+")
	if len(parts) == 0 || parts[0] == "" {
		return KeyChord{}, errors.New("empty key")
	}
	var chord KeyChord
	last := parts[len(parts)-1]
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "shift":
			chord.Mods |= ModShift
		case "ctrl", "control":
			chord.Mods |= ModCtrl
		case "alt":
			chord.Mods |= ModAlt
		case "meta", "cmd", "command":
			chord.Mods |= ModMeta
		default:
			return KeyChord{}, errors.New("unsupported modifier")
		}
	}
	switch strings.ToLower(strings.TrimSpace(last)) {
	case "enter", "return":
		chord.Code = KeyEnter
	case "esc", "escape":
		chord.Code = KeyEscape
	case "tab":
		chord.Code = KeyTab
	case "backspace":
		chord.Code = KeyBackspace
	case "delete", "del":
		chord.Code = KeyDelete
	case "space":
		chord.Code = KeySpace
	case "up":
		chord.Code = KeyUp
	case "down":
		chord.Code = KeyDown
	case "left":
		chord.Code = KeyLeft
	case "right":
		chord.Code = KeyRight
	case "home":
		chord.Code = KeyHome
	case "end":
		chord.Code = KeyEnd
	case "pageup", "pgup":
		chord.Code = KeyPageUp
	case "pagedown", "pgdn":
		chord.Code = KeyPageDown
	default:
		runes := []rune(last)
		if len(runes) != 1 {
			return KeyChord{}, errors.New("unsupported key")
		}
		chord.Code = KeyRune
		chord.Rune = runes[0]
	}
	return chord.Normalize(), nil
}

func NormalizeText(raw string, maxBytes int) (Text, []Diagnostic, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxTextBytes
	}
	value, diagnostics := sanitizeString(raw, maxBytes)
	return Text{Value: value, Source: SourceTyped, Truncated: hasCode(diagnostics, "INPUT_TRUNCATED")}, diagnostics, nil
}

func NormalizePaste(raw []byte, maxBytes int) (Text, []Diagnostic, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxPasteBytes
	}
	valid := bytes.ToValidUTF8(raw, []byte("\uFFFD"))
	value, diagnostics := sanitizeString(string(valid), maxBytes)
	return Text{Value: value, Source: SourcePaste, Truncated: hasCode(diagnostics, "INPUT_TRUNCATED")}, diagnostics, nil
}

func KeyEvent(chord KeyChord) staveevent.Event {
	chord = chord.Normalize()
	return mustEvent(staveevent.Key, staveevent.KeyPayload{
		Key:       string(chord.Code),
		Rune:      chord.Rune,
		Modifiers: modifierNames(chord.Mods),
	})
}

func TextEvent(text Text) staveevent.Event {
	return mustEvent(staveevent.Text, staveevent.TextPayload{
		Text:      text.Value,
		Committed: true,
	})
}

func ResizeEvent(width, height int) staveevent.Event {
	return mustEvent(staveevent.Resize, staveevent.ResizePayload{Width: width, Height: height})
}

func SignalEvent(name string) staveevent.Event {
	return mustEvent(staveevent.Shutdown, nil)
}

func EOFEvent() staveevent.Event {
	return mustEvent(staveevent.Shutdown, nil)
}

func SecretInputEvent(handle SecretHandle) staveevent.Event {
	return mustEvent(staveevent.SecureInput, staveevent.SecureInputPayload{Handle: handle})
}

func NewMemorySecretStore() *MemorySecretStore {
	return coresecret.NewMemoryStore()
}

func sanitizeString(raw string, maxBytes int) (string, []Diagnostic) {
	if !utf8.ValidString(raw) {
		raw = strings.ToValidUTF8(raw, "\uFFFD")
	}
	var diagnostics []Diagnostic
	var builder strings.Builder
	for _, r := range raw {
		switch {
		case r == '\r':
			builder.WriteByte('\n')
			diagnostics = append(diagnostics, Diagnostic{Code: "INPUT_NORMALIZED", Message: "carriage returns normalized"})
		case r == '\n' || r == '\t':
			builder.WriteRune(r)
		case r == 0x7f || (unicode.IsControl(r) && r != '\n' && r != '\t'):
			diagnostics = append(diagnostics, Diagnostic{Code: "INPUT_CONTROL_REMOVED", Message: "control bytes removed"})
		default:
			builder.WriteRune(r)
		}
	}
	value := builder.String()
	if len(value) > maxBytes {
		diagnostics = append(diagnostics, Diagnostic{
			Code:    "INPUT_TRUNCATED",
			Message: "input truncated to configured byte limit",
		})
		value = truncateBytes(value, maxBytes)
	}
	return value, collapseDiagnostics(diagnostics)
}

func collapseDiagnostics(in []Diagnostic) []Diagnostic {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]Diagnostic, 0, len(in))
	for _, diagnostic := range in {
		if seen[diagnostic.Code] {
			continue
		}
		seen[diagnostic.Code] = true
		out = append(out, diagnostic)
	}
	return out
}

func truncateBytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	cut := 0
	for index := range value {
		if index > maxBytes {
			break
		}
		if index == maxBytes {
			cut = index
			break
		}
		cut = index
	}
	if cut == 0 {
		return ""
	}
	return value[:cut]
}

func hasCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func mustEvent(kind staveevent.Kind, payload any) staveevent.Event {
	ev, err := staveevent.New(kind, payload)
	if err != nil {
		panic(err)
	}
	return ev
}

func modifierNames(mods Mod) []string {
	names := make([]string, 0, 4)
	if mods&ModShift != 0 {
		names = append(names, "shift")
	}
	if mods&ModCtrl != 0 {
		names = append(names, "ctrl")
	}
	if mods&ModAlt != 0 {
		names = append(names, "alt")
	}
	if mods&ModMeta != 0 {
		names = append(names, "meta")
	}
	return names
}
