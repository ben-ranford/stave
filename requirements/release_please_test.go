package requirements

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var releaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.([1-9][0-9]*))?$`)

type releasePleasePackageConfig struct {
	ReleaseType           string   `json:"release-type"`
	IncludeComponentInTag bool     `json:"include-component-in-tag"`
	ExtraFiles            []string `json:"extra-files"`
	ExcludePaths          []string `json:"exclude-paths"`
}

type releasePleaseConfig struct {
	Packages map[string]releasePleasePackageConfig `json:"packages"`
}

func TestRootReleasePleaseConfiguration(t *testing.T) {
	root := rootReleasePleasePackage(t)
	assertRootReleasePleasePackage(t, root)
	assertReleasePleaseUsesDefaultVersioning(t)
	assertReleasePleaseCandidateDocuments(t, root)
	assertReleasePleaseManifestAndChangelog(t)
	assertReleasePleaseWorkflow(t)
}

func assertReleasePleaseUsesDefaultVersioning(t *testing.T) {
	t.Helper()
	rawConfig, err := os.ReadFile("../release-please-config.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Packages map[string]map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"versioning", "prerelease", "prerelease-type"} {
		if _, exists := config.Packages["."][setting]; exists {
			t.Fatalf("root release must use Release Please default versioning; remove %q", setting)
		}
	}
}

func rootReleasePleasePackage(t *testing.T) releasePleasePackageConfig {
	t.Helper()

	const configPath = "../release-please-config.json"
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}
	var config releasePleaseConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		t.Fatalf("decode %s: %v", configPath, err)
	}
	root, ok := config.Packages["."]
	if !ok {
		t.Fatalf("%s must configure the root package", configPath)
	}
	return root
}

func assertRootReleasePleasePackage(t *testing.T, root releasePleasePackageConfig) {
	t.Helper()

	if root.ReleaseType != "go" {
		t.Fatalf("root release must remain a Go package: %+v", root)
	}
	if root.IncludeComponentInTag {
		t.Fatal("root release tags must not include a component")
	}
	if !slices.Contains(root.ExcludePaths, "adapters") {
		t.Fatalf("root release must exclude internal adapter-only commits: %v", root.ExcludePaths)
	}
}

func assertReleasePleaseCandidateDocuments(t *testing.T, root releasePleasePackageConfig) {
	t.Helper()

	for _, path := range []string{"README.md", "docs/client-adoption.md"} {
		if !slices.Contains(root.ExtraFiles, path) {
			t.Fatalf("root release must update candidate reference %q: %v", path, root.ExtraFiles)
		}
		content, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(content), "x-release-please-version") {
			t.Fatalf("%s must annotate its candidate reference for release-please", path)
		}
	}
}

func assertReleasePleaseManifestAndChangelog(t *testing.T) {
	t.Helper()

	const manifestPath = "../.release-please-manifest.json"
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		t.Fatalf("decode %s: %v", manifestPath, err)
	}
	if !releaseVersion.MatchString(manifest["."]) {
		t.Fatalf("root manifest must contain an rc baseline or stable SemVer version, got %q", manifest["."])
	}
	changelogPath := filepath.Join("..", "CHANGELOG.md")
	changelog, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	if !strings.Contains(string(changelog), "## [Unreleased]") || !strings.Contains(string(changelog), "## ["+manifest["."]+"]") {
		t.Fatalf("%s must include Unreleased and manifest candidate %q headings", changelogPath, manifest["."])
	}
}

func assertReleasePleaseWorkflow(t *testing.T) {
	t.Helper()

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

func TestReleaseVersionContract(t *testing.T) {
	for version, want := range map[string]bool{
		"1.0.0-rc.1":         true,
		"2.14.3-rc.42":       true,
		"0.0.0-rc.1":         true,
		"1.1.0":              true,
		"2.14.3":             true,
		"01.0.0-rc.1":        false,
		"1.01.0-rc.1":        false,
		"1.0.01-rc.1":        false,
		"1.0.0-rc.0":         false,
		"1.0.0-rc.01":        false,
		"1.0.0-beta.1":       false,
		"v1.0.0-rc.1":        false,
		"1.0.0-rc.1+build.7": false,
	} {
		t.Run(version, func(t *testing.T) {
			if got := releaseVersion.MatchString(version); got != want {
				t.Fatalf("release version validation for %q = %t, want %t", version, got, want)
			}
		})
	}
}
