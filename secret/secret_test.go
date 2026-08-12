package secret

import "testing"

func TestMemoryStoreOpaqueSingleUseAndDestroy(t *testing.T) {
	s := NewMemoryStore()
	h, err := s.Put([]byte("top-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if h.ID == "top-secret" || h.ID == "" {
		t.Fatalf("handle leaked value: %#v", h)
	}
	if err := s.Use(h, func(v []byte) error {
		if string(v) != "top-secret" {
			t.Fatalf("value=%q", v)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Use(h, func([]byte) error { return nil }); err == nil {
		t.Fatal("secret handle was reusable")
	}
	if err := s.Destroy(h); err != nil {
		t.Fatal(err)
	}
}
