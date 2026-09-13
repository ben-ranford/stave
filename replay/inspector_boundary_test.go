package replay

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeTranscriptRejectsPayloadForPayloadlessEvent(t *testing.T) {
	transcript := mustTranscript(t)
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"kind":"key"`), []byte(`"kind":"focus"`), 1)
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted a payload for a focus event")
	}
}

func TestValidateUniqueJSONKeysBoundsNestedInput(t *testing.T) {
	withinLimit := []byte(strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65))
	if err := validateUniqueJSONKeys(withinLimit, 0); err != nil {
		t.Fatalf("validateUniqueJSONKeys() rejected the exact scalar boundary: %v", err)
	}
	overLimit := []byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))
	if err := validateUniqueJSONKeys(overLimit, 0); err == nil {
		t.Fatal("validateUniqueJSONKeys() accepted excessive nesting")
	}
}

func BenchmarkValidateUniqueJSONKeysNestedScalar(b *testing.B) {
	data := []byte(strings.Repeat("[", 60) + `"` + strings.Repeat("x", 1<<20) + `"` + strings.Repeat("]", 60))
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		if err := validateUniqueJSONKeys(data, 0); err != nil {
			b.Fatal(err)
		}
	}
}
