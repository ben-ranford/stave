package testfixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ben-ranford/stave/conformance"
)

func TestPrimitiveManifestUsesRealConstructors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "primitive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Fixtures []string `json:"fixtures"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, name := range manifest.Fixtures {
		t.Run(name, func(t *testing.T) {
			node, err := Primitive(name, "manifest")
			if err != nil {
				t.Fatal(err)
			}
			if failures := conformance.ValidateTree(node); len(failures) > 0 {
				t.Fatalf("conformance failures: %+v", failures)
			}
		})
	}
}

func TestPrimitiveManifestRejectsUnknownFixture(t *testing.T) {
	if _, err := Primitive("not-a-fixture", "manifest"); err == nil {
		t.Fatal("unknown fixture must fail closed")
	}
}
