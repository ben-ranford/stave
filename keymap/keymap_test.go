package keymap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/input"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func TestCodecEncodeIsDeterministicAndRoundTrips(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	enter, _ := input.ParseKey("enter")
	activate := action.ID(primitive.CanonicalActionID("activate"))
	mappings := []Mapping{
		{Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandFocusNext}, Route: Route{Kind: RouteAction, ActionID: activate}},
		{Binding: Binding{Sequence: []input.KeyChord{enter}, Command: CommandShutdown}, Route: Route{Kind: RouteEvent, EventKind: "shutdown"}},
	}
	first, err := New("portable", mappings)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New("portable", []Mapping{mappings[1], mappings[0]})
	if err != nil {
		t.Fatal(err)
	}
	encodedFirst, err := first.Encode()
	if err != nil {
		t.Fatalf("encode first: %v", err)
	}
	encodedSecond, err := second.Encode()
	if err != nil {
		t.Fatalf("encode second: %v", err)
	}
	if string(encodedFirst) != string(encodedSecond) {
		t.Fatalf("encoding depends on mapping order:\n%s\n%s", encodedFirst, encodedSecond)
	}
	var document codecDocument
	if err := json.Unmarshal(encodedFirst, &document); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if document.Version != CodecVersion {
		t.Fatalf("version=%q, want %q", document.Version, CodecVersion)
	}
	roundTripped, err := Decode(encodedFirst, []action.Definition{{ID: activate}})
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	expected, err := New(document.Profile, document.Mappings)
	if err != nil {
		t.Fatal(err)
	}
	if roundTripped.Profile() != "portable" || !reflect.DeepEqual(roundTripped.Bindings(), expected.Bindings()) {
		t.Fatalf("round trip changed map: %#v", roundTripped)
	}
}

func TestDecodeRejectsInvalidVersionsBindingsAndUnknownFields(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"version":"stave.keymap.v2","profile":"portable","mappings":[]}`),
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":[{"binding":{"command":"stave.activate.v1","sequence":[]},"route":{"kind":"event","eventKind":"shutdown"}}]}`),
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":[],"unexpected":true}`),
	} {
		if _, err := Decode(raw, nil); err == nil {
			t.Fatalf("Decode(%s) succeeded", raw)
		}
	}
}

func TestDecodeRejectsInvalidCodecBoundaries(t *testing.T) {
	invalidUTF8 := append([]byte(`{"version":"stave.keymap.v1","profile":"`), append([]byte{0xff}, []byte(`","mappings":[]}`)...)...)
	for _, raw := range [][]byte{
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":[{"binding":{"command":"stave.activate.v1","sequence":[{}]},"route":{"kind":"event","eventKind":"shutdown"}}]}`),
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":[{"binding":{"command":"stave.activate.v1","sequence":[{"code":"invalid"}]},"route":{"kind":"event","eventKind":"shutdown"}}]}`),
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":[{"binding":{"command":"stave.activate.v1","sequence":[{"code":"tab","mods":128}]},"route":{"kind":"event","eventKind":"shutdown"}}]}`),
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":[{"binding":{"command":"stave.activate.v1","sequence":[{"code":"rune","rune":55296}]},"route":{"kind":"event","eventKind":"shutdown"}}]}`),
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":[{"binding":{"command":"stave.activate.v1","sequence":[{"code":"rune","rune":10}]},"route":{"kind":"event","eventKind":"shutdown"}}]}`),
		invalidUTF8,
		[]byte(`{"version":"stave.keymap.v1","profile":"portable"}`),
		[]byte(`{"version":"stave.keymap.v1","profile":"portable","mappings":null}`),
	} {
		if _, err := Decode(raw, nil); err == nil {
			t.Fatalf("Decode(%q) succeeded", raw)
		}
	}
	if _, err := New("direct", []Mapping{{
		Binding: Binding{Sequence: []input.KeyChord{{}}, Command: CommandActivate},
		Route:   Route{Kind: RouteEvent, EventKind: "shutdown"},
	}}); err != nil {
		t.Fatalf("New changed for direct structured chords: %v", err)
	}
}

func TestCodecRoundTripsStructuredRuneChords(t *testing.T) {
	mappings := []Mapping{
		{Binding: Binding{Sequence: []input.KeyChord{{Code: input.KeyRune, Rune: '+'}}, Command: CommandFocusNext}, Route: Route{Kind: RouteEvent, EventKind: "shutdown"}},
		{Binding: Binding{Sequence: []input.KeyChord{{Code: input.KeyRune, Rune: '+', Mods: input.ModCtrl}}, Command: CommandFocusPrevious}, Route: Route{Kind: RouteEvent, EventKind: "shutdown"}},
		{Binding: Binding{Sequence: []input.KeyChord{{Code: input.KeyRune, Rune: 'é', Mods: input.ModAlt | input.ModMeta}}, Command: CommandActivate}, Route: Route{Kind: RouteEvent, EventKind: "shutdown"}},
	}
	profile, err := New("structured-runes", mappings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := profile.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := Decode(raw, nil)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	encoded, err := decoded.Encode()
	if err != nil {
		t.Fatalf("re-encode error = %v", err)
	}
	if string(encoded) != string(raw) {
		t.Fatalf("structured rune encoding changed:\n%s\n%s", encoded, raw)
	}
}

func TestEncodeRejectsInvalidUTF8(t *testing.T) {
	for _, mapping := range []Mapping{
		{
			Binding: Binding{Sequence: []input.KeyChord{{Code: input.KeyTab}}, Command: CommandID("\xff")},
			Route:   Route{Kind: RouteEvent, EventKind: "shutdown"},
		},
		{
			Binding: Binding{Sequence: []input.KeyChord{{Code: input.KeyTab}}, Command: CommandActivate},
			Route: Route{Kind: RouteAction, ActionID: action.ID(primitive.CanonicalActionID("activate")),
				Arguments: json.RawMessage("{\"value\":\"\xff\"}")},
		},
	} {
		profile, err := New("portable", []Mapping{mapping})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := profile.Encode(); err == nil {
			t.Fatal("Encode accepted invalid UTF-8")
		}
	}
	profile, err := New("\xff", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Encode(); err == nil {
		t.Fatal("Encode accepted invalid UTF-8 profile")
	}
}

func TestDecodeRejectsConflictingBindingsAndActionManifestMismatches(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	conflicting, err := json.Marshal(codecDocument{Version: CodecVersion, Profile: "portable", Mappings: []Mapping{
		{Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandFocusNext}, Route: Route{Kind: RouteEvent, EventKind: "shutdown"}},
		{Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandFocusPrevious}, Route: Route{Kind: RouteEvent, EventKind: "shutdown"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(conflicting, nil); err == nil {
		t.Fatal("expected conflicting bindings to fail")
	}
	activate := action.ID(primitive.CanonicalActionID("activate"))
	profile, err := New("portable", []Mapping{{Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandActivate}, Route: Route{Kind: RouteAction, ActionID: activate}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := profile.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(raw, nil); err == nil || !strings.Contains(err.Error(), "absent from action manifest") {
		t.Fatalf("expected manifest parity error, got %v", err)
	}
	if _, err := Decode(raw, []action.Definition{{ID: activate}}); err != nil {
		t.Fatalf("expected matching manifest to import profile: %v", err)
	}
}

func TestNewRejectsAmbiguousBindings(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	_, err := New("test", []Mapping{
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandFocusNext},
			Route:   Route{Kind: RouteEvent, EventKind: "focus"},
		},
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandFocusPrevious},
			Route:   Route{Kind: RouteEvent, EventKind: "focus"},
		},
	})
	if err == nil {
		t.Fatal("expected ambiguous binding validation error")
	}
}

func TestDispatcherRoutesActivateToTypedAction(t *testing.T) {
	keymap, err := Default()
	if err != nil {
		t.Fatalf("default keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	enter, _ := input.ParseKey("enter")
	target := semantic.Target{NodeID: semantic.NodeID("n1_target"), Generation: 1, ObservedRevision: 9}
	result, err := dispatcher.Feed(enter, "", target, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != StatusMatch || result.Call == nil {
		t.Fatalf("expected matched action route, got %#v", result)
	}
	if result.Call.ActionID != action.ID("stave.primitive.core.activate.v1") {
		t.Fatalf("unexpected action id: %s", result.Call.ActionID)
	}
	if result.Call.Target != target {
		t.Fatal("expected target to flow through action call")
	}
}

func TestDispatcherKeepsAllDefaultBindingsMapped(t *testing.T) {
	keymap, err := Default()
	if err != nil {
		t.Fatalf("default keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	target := semantic.Target{NodeID: semantic.NodeID("n1_target"), Generation: 1, ObservedRevision: 3}
	for _, mapping := range keymap.Bindings() {
		dispatcher.Reset()
		result, err := dispatcher.Feed(mapping.Binding.Sequence[0], mapping.Binding.Scope, target, time.Unix(20, 0))
		if err != nil {
			t.Fatalf("dispatch %s: %v", mapping.Binding.Command, err)
		}
		if result.Status != StatusMatch {
			t.Fatalf("binding %s did not resolve, got %s", mapping.Binding.Command, result.Status)
		}
		if result.Call == nil && result.Event == nil {
			t.Fatalf("binding %s has no typed action or event parity", mapping.Binding.Command)
		}
	}
}

func TestDispatcherReturnsPendingForPrefixSequence(t *testing.T) {
	ctrlX, _ := input.ParseKey("ctrl+x")
	ctrlS, _ := input.ParseKey("ctrl+s")
	keymap, err := New("custom", []Mapping{
		{
			Binding: Binding{
				Sequence: []input.KeyChord{ctrlX, ctrlS},
				Command:  CommandActivate,
			},
			Route: Route{Kind: RouteAction, ActionID: action.ID("stave.save.v1")},
		},
	})
	if err != nil {
		t.Fatalf("new keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	result, err := dispatcher.Feed(ctrlX, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatalf("dispatch prefix: %v", err)
	}
	if result.Status != StatusPending {
		t.Fatalf("expected pending prefix, got %s", result.Status)
	}
	result, err = dispatcher.Feed(ctrlS, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatalf("dispatch completion: %v", err)
	}
	if result.Status != StatusMatch || result.Call == nil {
		t.Fatalf("expected completed action match, got %#v", result)
	}
}

func TestActionArgumentsRemainCanonicalBytes(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	args := json.RawMessage(`{"direction":"next"}`)
	keymap, err := New("custom", []Mapping{
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandActivate},
			Route:   Route{Kind: RouteAction, ActionID: action.ID("stave.focus.move.v1"), Arguments: args},
		},
	})
	if err != nil {
		t.Fatalf("new keymap: %v", err)
	}
	dispatcher := NewDispatcher(keymap)
	result, err := dispatcher.Feed(tab, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(result.Call.Arguments) != string(args) {
		t.Fatalf("expected raw arguments to survive mapping, got %s", result.Call.Arguments)
	}
}

func TestInventoryIsImmutableAndExposesVersionedActions(t *testing.T) {
	m, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	inventory := m.Inventory()
	if len(inventory) == 0 {
		t.Fatal("expected built-in inventory")
	}
	ids := m.ActionIDs()
	if len(ids) == 0 {
		t.Fatalf("action ids=%v", ids)
	}
	foundActivate := false
	for _, id := range ids {
		if id == action.ID("stave.primitive.core.activate.v1") {
			foundActivate = true
		}
	}
	if !foundActivate {
		t.Fatalf("activate action missing from ids=%v", ids)
	}
	inventory[0].Sequence[0] = input.KeyChord{Code: input.KeyDelete}
	if m.Inventory()[0].Sequence[0].Code == input.KeyDelete {
		t.Fatal("inventory leaked mutable sequence")
	}
	for _, item := range inventory {
		if item.Command == "" {
			t.Fatal("unnamed command")
		}
		if item.RouteKind == RouteAction && item.ActionID == "" {
			t.Fatalf("action route missing id: %#v", item)
		}
	}
}

func TestNewRejectsInvalidRouteIDsAndPayloads(t *testing.T) {
	tab, _ := input.ParseKey("tab")
	for _, tc := range []Mapping{
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandActivate},
			Route:   Route{Kind: RouteAction, ActionID: action.ID("invalid")},
		},
		{
			Binding: Binding{Sequence: []input.KeyChord{tab}, Command: CommandShutdown},
			Route:   Route{Kind: RouteEvent, EventKind: "shutdown", Payload: map[string]string{"reason": "nope"}},
		},
	} {
		if _, err := New("invalid", []Mapping{tc}); err == nil {
			t.Fatalf("expected validation error for %#v", tc.Route)
		}
	}
}

func TestDispatcherClonesRouteArgumentsAndPayloadAtIngress(t *testing.T) {
	chord, _ := input.ParseKey("enter")
	args := json.RawMessage(`{"x":1}`)
	payload := map[string]string{"key": "enter"}
	km, err := New("test", []Mapping{{Binding: Binding{Sequence: []input.KeyChord{chord}, Command: CommandActivate}, Route: Route{Kind: RouteAction, ActionID: action.ID(primitive.CanonicalActionID("activate")), Arguments: args}}})
	if err != nil {
		t.Fatal(err)
	}
	args[0] = 'x'
	d := NewDispatcher(km)
	r, err := d.Feed(chord, "", semantic.Target{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Call.Arguments) != `{"x":1}` {
		t.Fatalf("route args were aliased: %s", r.Call.Arguments)
	}
	_ = payload
}

func codecMappings(count, chords int) []Mapping {
	mappings := make([]Mapping, count)
	for i := range mappings {
		sequence := make([]input.KeyChord, chords)
		for j := range sequence {
			sequence[j] = input.KeyChord{Code: input.KeyRune, Rune: 'a'}
		}
		sequence[len(sequence)-1] = input.KeyChord{Code: input.KeyRune, Rune: rune(0x4E00 + i)}
		mappings[i] = Mapping{Binding: Binding{Sequence: sequence, Command: CommandID(fmt.Sprint(i))}, Route: Route{Kind: RouteEvent, EventKind: "shutdown"}}
	}
	return mappings
}

func TestCodecBoundsBytesMappingsAndChords(t *testing.T) {
	const byteLimit = 1 << 20
	encode := func(mappings []Mapping, profile string) []byte {
		t.Helper()
		raw, err := json.Marshal(codecDocument{Version: CodecVersion, Profile: profile, Mappings: mappings})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := encode([]Mapping{}, "")
	profile := strings.Repeat("p", byteLimit-len(raw))
	exact := encode([]Mapping{}, profile)
	if len(exact) != byteLimit {
		t.Fatal("incorrect exact-size fixture")
	}
	if _, err := Decode(exact, nil); err != nil {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
	boundaryMap, err := New(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := boundaryMap.Encode(); err != nil || len(encoded) != byteLimit {
		t.Fatalf("exact byte limit encode: bytes=%d err=%v", len(encoded), err)
	}
	for _, raw := range [][]byte{append(exact, ' '), encode(codecMappings(1025, 1), "test"), encode(codecMappings(1, 17), "test")} {
		if _, err := Decode(raw, nil); err == nil {
			t.Fatal("oversized profile accepted")
		}
	}
	for _, data := range []struct {
		profile  string
		mappings []Mapping
	}{
		{strings.Repeat("p", byteLimit), nil}, {"test", codecMappings(1025, 1)}, {"test", codecMappings(1, 17)},
	} {
		m, err := New(data.profile, data.mappings)
		if err != nil {
			t.Fatalf("in-memory API changed: %v", err)
		}
		if _, err := m.Encode(); err == nil {
			t.Fatal("encoder produced non-importable oversized profile")
		}
	}
	maximum, err := New("maximum", codecMappings(1024, 16))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := maximum.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded, nil)
	if err != nil || len(decoded.Bindings()) != 1024 {
		t.Fatalf("maximum valid profile rejected: %v", err)
	}
}
