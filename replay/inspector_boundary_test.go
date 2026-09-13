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

func TestValidateUniqueJSONKeysBoundsRootRecordsBeforeRawDecoding(t *testing.T) {
	if err := validateUniqueJSONKeys(compactRecordsJSON(MaxTranscriptRecords), 0); err != nil {
		t.Fatalf("validateUniqueJSONKeys() rejected exact record boundary: %v", err)
	}
	overLimit := compactRecordsJSON(MaxTranscriptRecords + 1)
	if _, err := DecodeTranscript(overLimit); err == nil || !strings.Contains(err.Error(), "record limit") {
		t.Fatalf("DecodeTranscript() error = %v, want early record limit", err)
	}
}

func TestDecodeTranscriptAcceptsCanonicalEmptyRecords(t *testing.T) {
	transcript := mustTranscript(t)
	transcript.Records = nil
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTranscript(data)
	if err != nil {
		t.Fatalf("DecodeTranscript() rejected canonical empty records: %v", err)
	}
	if len(decoded.Records) != 0 {
		t.Fatalf("decoded records = %d, want zero", len(decoded.Records))
	}
}

func TestDecodeTranscriptRejectsCaseFoldedRootRecordsBeforeRawDecoding(t *testing.T) {
	data := compactRecordsJSON(MaxTranscriptRecords + 1)
	original := append([]byte(nil), data...)
	data = bytes.Replace(data, []byte(`{"records":`), []byte(`{"Records":`), 1)
	if bytes.Equal(data, original) {
		t.Fatal("test fixture did not case-fold root records")
	}
	if _, err := DecodeTranscript(data); err == nil || !strings.Contains(err.Error(), "noncanonical") {
		t.Fatalf("DecodeTranscript() error = %v, want early canonical records key rejection", err)
	}
}

func TestValidateUniqueJSONKeysDoesNotCapOpaqueArrays(t *testing.T) {
	data := []byte(`{"model":` + string(compactEmptyObjects(MaxTranscriptRecords+1)) + `}`)
	if err := validateUniqueJSONKeys(data, 0); err != nil {
		t.Fatalf("validateUniqueJSONKeys() capped opaque application array: %v", err)
	}
}

func TestDecodeTranscriptDuplicateKeyDoesNotExposeKeyName(t *testing.T) {
	const secretKey = "fuzz-secret-key"
	data := []byte(`{"` + secretKey + `":1,"` + secretKey + `":2}`)
	if _, err := DecodeTranscript(data); err == nil {
		t.Fatal("DecodeTranscript() accepted duplicate key")
	} else if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("DecodeTranscript() exposed duplicate key: %v", err)
	}
}

func compactRecordsJSON(count int) []byte {
	data := make([]byte, 0, len(`{"records":}`)+count*3)
	data = append(data, `{"records":[`...)
	data = append(data, compactEmptyObjects(count)[1:]...)
	return append(data, '}')
}

func compactEmptyObjects(count int) []byte {
	data := make([]byte, 0, 2+count*3)
	data = append(data, '[')
	for index := 0; index < count; index++ {
		if index > 0 {
			data = append(data, ',')
		}
		data = append(data, '{', '}')
	}
	return append(data, ']')
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
