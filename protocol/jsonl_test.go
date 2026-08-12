package protocol

import "testing"

func TestDecodeLineRejectsDuplicateAndTrailing(t *testing.T) {
	for _, in := range []string{`{"jsonrpc":"2.0","id":1,"method":"x","params":{"a":1,"a":2}}`, `{"jsonrpc":"2.0","id":1,"method":"x"} {"x":1}`, `[{}]`, "\xff"} {
		if _, err := DecodeLine([]byte(in), 1024); err == nil {
			t.Errorf("accepted hostile input %q", in)
		}
	}
}

func TestDecodeLineRejectsUnknownEnvelopeFields(t *testing.T) {
	if _, err := DecodeLine([]byte(`{"jsonrpc":"2.0","id":1,"method":"stave.ping","unexpected":true}`), 1024); err == nil {
		t.Fatal("unknown JSON-RPC envelope field was accepted")
	}
}
func TestDecodeLinePreservesExactID(t *testing.T) {
	r, err := DecodeLine([]byte(`{"jsonrpc":"2.0","id":"0007","method":"stave.ping"}`), 1024)
	if err != nil || string(r.ID) != `"0007"` {
		t.Fatalf("id was not preserved: %#v %v", r.ID, err)
	}
}
func TestDecodeLineLimitsDepth(t *testing.T) {
	b := []byte(`{"jsonrpc":"2.0","id":1,"method":"x","params":`)
	for i := 0; i < 70; i++ {
		b = append(b, '{')
		b = append(b, []byte(`"x":`)...)
	}
	b = append(b, []byte(`null`)...)
	for i := 0; i < 70; i++ {
		b = append(b, '}')
	}
	b = append(b, '}')
	if _, err := DecodeLine(b, 100000); err == nil {
		t.Fatal("accepted excessive nesting")
	}
}
