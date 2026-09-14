package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

const maxProtocolFuzzBytes = 4 << 10

func FuzzDecodeLineBounded(f *testing.F) {
	for _, seed := range protocolFuzzSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maxProtocolFuzzBytes {
			if _, err := DecodeLine(input, maxProtocolFuzzBytes); err == nil {
				t.Fatal("oversized protocol input was accepted")
			}
			return
		}
		request, err := DecodeLine(input, maxProtocolFuzzBytes)
		if err != nil {
			if strings.Contains(err.Error(), "fuzz-secret") {
				t.Fatalf("protocol parser echoed a secret: %v", err)
			}
			return
		}
		canonical, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("accepted request did not marshal: %v", err)
		}
		roundTrip, err := DecodeLine(canonical, len(canonical))
		if err != nil {
			t.Fatalf("accepted request did not round trip: %v", err)
		}
		if !semanticJSONEqual(request.ID, roundTrip.ID) || request.JSONRPC != roundTrip.JSONRPC || request.Method != roundTrip.Method || !semanticJSONEqual(request.Params, roundTrip.Params) {
			t.Fatalf("accepted request changed during round trip")
		}
	})
}

func protocolFuzzSeeds() [][]byte {
	valid := []byte(`{"jsonrpc":"2.0","id":"0007","method":"stave.ping","params":{"value":true}}`)
	limit := append(append([]byte(nil), valid...), bytes.Repeat([]byte(" "), maxProtocolFuzzBytes-len(valid))...)
	return [][]byte{
		valid,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"` + strings.Repeat("&", 680) + `"}`),
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"stave.ping","unexpected":true}`),
		[]byte(`{"jsonrpc":"2.0","id":1,"fuzz-secret":1,"fuzz-secret":2,"method":"stave.ping"}`),
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"stave.ping"} {}`),
		[]byte(`{"jsonrpc":"2.0", "id": "0007", "method": "stave.ping", "params": { "value": true } }`),
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"stave.ping","params":{"token":"fuzz-secret"},"unexpected":true}`),
		[]byte(`null`),
		[]byte("\xff{"),
		protocolNestedSeed(64),
		protocolNestedSeed(65),
		limit,
		append(append([]byte(nil), limit...), 'x'),
	}
}

func semanticJSONEqual(left, right []byte) bool {
	leftValue, leftOK := decodeJSONValue(left)
	rightValue, rightOK := decodeJSONValue(right)
	return leftOK && rightOK && reflect.DeepEqual(leftValue, rightValue)
}

func decodeJSONValue(value []byte) (any, bool) {
	if len(value) == 0 {
		return nil, true
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, false
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return decoded, true
}

func protocolNestedSeed(depth int) []byte {
	output := []byte(`{"jsonrpc":"2.0","id":1,"method":"stave.ping","params":`)
	for range depth {
		output = append(output, []byte(`{"nested":`)...)
	}
	output = append(output, []byte(`null`)...)
	for range depth {
		output = append(output, '}')
	}
	return append(output, '}')
}

func TestProtocolFuzzNestingBoundarySeeds(t *testing.T) {
	if _, err := DecodeLine(protocolNestedSeed(64), maxProtocolFuzzBytes); err != nil {
		t.Fatalf("accepted boundary seed rejected: %v", err)
	}
	if _, err := DecodeLine(protocolNestedSeed(65), maxProtocolFuzzBytes); err == nil {
		t.Fatal("over-boundary seed accepted")
	}
}
