package requirements

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRootReleasePleaseConfiguration(t *testing.T) {
	type packageConfig struct {
		ReleaseType           string   `json:"release-type"`
		Versioning            string   `json:"versioning"`
		Prerelease            bool     `json:"prerelease"`
		PrereleaseType        string   `json:"prerelease-type"`
		IncludeComponentInTag bool     `json:"include-component-in-tag"`
		ExcludePaths          []string `json:"exclude-paths"`
	}
	type config struct {
		Packages map[string]packageConfig `json:"packages"`
	}

	configPath := filepath.Join("..", "release-please-config.json")
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}
	var got config
	if err := json.Unmarshal(rawConfig, &got); err != nil {
		t.Fatalf("decode %s: %v", configPath, err)
	}
	root, ok := got.Packages["."]
	if !ok {
		t.Fatalf("%s must configure the root package", configPath)
	}
	if root.ReleaseType != "go" || root.Versioning != "prerelease" || !root.Prerelease || root.PrereleaseType != "rc" {
		t.Fatalf("root release must remain a Go rc prerelease: %+v", root)
	}
	if root.IncludeComponentInTag {
		t.Fatal("root release tags must not include a component")
	}
	if !slices.Contains(root.ExcludePaths, "adapters") {
		t.Fatalf("root release must exclude internal adapter-only commits: %v", root.ExcludePaths)
	}

	manifestPath := filepath.Join("..", ".release-please-manifest.json")
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		t.Fatalf("decode %s: %v", manifestPath, err)
	}
	if manifest["."] != "1.0.0-rc.1" {
		t.Fatalf("root manifest must retain the published candidate version, got %q", manifest["."])
	}

	workflowPath := filepath.Join("..", ".github", "workflows", "release-please.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	for _, setting := range []string{"config-file: release-please-config.json", "manifest-file: .release-please-manifest.json"} {
		if !strings.Contains(string(workflow), setting) {
			t.Fatalf("%s must use %q", workflowPath, setting)
		}
	}
}
