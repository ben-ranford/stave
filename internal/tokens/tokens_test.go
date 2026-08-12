package tokens

import (
	"bytes"
	"testing"
)

func TestStableSortsKeys(t *testing.T) {
	got := Stable(map[string]any{"b": 2, "a": 1})
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestCanonicalJSONStable(t *testing.T) {
	a, err := CanonicalJSON(map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("canonical JSON changed across map order: %s != %s", a, b)
	}
}
