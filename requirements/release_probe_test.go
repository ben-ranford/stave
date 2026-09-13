package requirements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedReleaseProbeIsAnonymousAndWorkflowGated(t *testing.T) {
	probePath := filepath.Join("..", "scripts", "rigor", "verify-published-release.sh")
	probe, err := os.ReadFile(probePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"env -i \"${go_environment[@]}\" go get",
		"env -i \"${curl_environment[@]}\" curl",
		"GOWORK=off",
		"GOPROXY=https://proxy.golang.org",
		"GOSUMDB=sum.golang.org",
		"GOPRIVATE=",
		"GONOPROXY=",
		"GONOSUMDB=",
		"curl -q --connect-timeout",
		"RELEASE_PROBE_REQUEST_TIMEOUT_SECONDS",
		"RELEASE_PROBE_GO_TIMEOUT_SECONDS",
		"RELEASE_PROBE_TERMINATE_GRACE_SECONDS",
		"set -m",
		"kill -TERM -- \"-${command_pid}\"",
		"kill -KILL -- \"-${command_pid}\"",
		"wait_for_release_metadata",
		"release metadata incomplete after %s attempts",
		"retry_command \"resolving github.com/ben-ranford/stave@${tag}",
		"retry_command \"downloading github.com/ben-ranford/stave@${module_version}",
		"retry_command \"verifying selected github.com/ben-ranford/stave@${module_version}",
		"provenance_go_environment",
		"GOMODCACHE=${workdir}/provenance-modcache",
		"selected module provenance mismatch",
		".Origin.Hash // empty",
		"release probe failed after %s attempts",
		"CHANGELOG.md LICENSE report.json",
		"tag_object_sha",
		"source_sha",
		"module_sum",
		"module_origin_sha",
		"requested_module_version",
		"go list -m -json github.com/ben-ranford/stave",
	} {
		if !strings.Contains(string(probe), fragment) {
			t.Fatalf("published release probe must retain %q", fragment)
		}
	}
	for _, forbidden := range []string{"gh api ", "GITHUB_TOKEN", "Authorization:"} {
		if strings.Contains(string(probe), forbidden) {
			t.Fatalf("published release probe must not use credentials (%q)", forbidden)
		}
	}

	workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobStart := strings.Index(string(workflow), "  verify-public-consumer:\n")
	if jobStart < 0 {
		t.Fatal("release workflow must define verify-public-consumer")
	}
	publicConsumerJob := string(workflow)[jobStart:]
	for _, fragment := range []string{
		"needs: publish",
		"name: release / public consumer",
		"persist-credentials: false",
		"RELEASE_PROBE_ATTEMPTS: \"3\"",
		"RELEASE_PROBE_GO_TIMEOUT_SECONDS: \"60\"",
		"./scripts/rigor/verify-published-release.sh \"${{ github.ref_name }}\"",
	} {
		if !strings.Contains(publicConsumerJob, fragment) {
			t.Fatalf("release workflow must retain %q", fragment)
		}
	}
}

func TestReleaseRunbookMakesRepositoryPublicBeforeAnonymousProbe(t *testing.T) {
	runbook, err := os.ReadFile(filepath.Join("..", "docs", "releasing.md"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(runbook)
	position := func(fragment string) int {
		t.Helper()
		index := strings.Index(content, fragment)
		if index < 0 {
			t.Fatalf("release runbook must retain %q", fragment)
		}
		return index
	}
	schedulerProof := position("scheduler boundary")
	sourceCI := position("fresh successful required-check run")
	visibility := position("Obtain the authorized visibility transition")
	tag := position("Derive the release tag")
	probe := position("release / public consumer")
	if schedulerProof >= sourceCI || sourceCI >= visibility || visibility >= tag || tag >= probe {
		t.Fatal("release runbook must require scheduler proof, source CI, and authorized public visibility before tagging and the anonymous probe")
	}
	for _, fragment := range []string{"pseudo-version", "requested-tag resolution", "fresh module cache"} {
		position(fragment)
	}
}
