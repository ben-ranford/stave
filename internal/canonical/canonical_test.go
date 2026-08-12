package canonical

import "testing"

func TestJSONNumbers(t *testing.T) {
	a, _ := JSON([]byte(`9007199254740992`))
	b, _ := JSON([]byte(`9007199254740993`))
	if string(a) == string(b) {
		t.Fatal(a, b)
	}
	for _, x := range []string{`1e2`, `100.0`, `-0`, `1 `} {
		if _, e := JSON([]byte(x)); e != nil {
			t.Fatal(e)
		}
	}
	a, _ = JSON([]byte(`1`))
	b, _ = JSON([]byte(`1.0`))
	if string(a) != string(b) {
		t.Fatal(a, b)
	}
	a, _ = JSON([]byte(`1e-101`))
	b, _ = JSON([]byte(`0`))
	if string(a) == string(b) {
		t.Fatal("precision collision")
	}
	if _, e := JSON([]byte(`1 2`)); e == nil {
		t.Fatal("trailing accepted")
	}
}
