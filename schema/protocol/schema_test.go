package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedInSchemaIsValid(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("protocol.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v["$id"] != Version {
		t.Fatalf("schema id=%v", v["$id"])
	}
}
