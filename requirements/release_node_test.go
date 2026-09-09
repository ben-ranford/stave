package requirements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseWorkflowsProvideNodeForCanonicalCI(t *testing.T) {
	for _, name := range []string{"release.yml", "prerelease-dry-run.yml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			workflow := string(data)
			node := strings.Index(workflow, "uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e")
			ci := strings.Index(workflow, "make ci")
			if node < 0 || ci < 0 || node > ci || !strings.Contains(workflow[node:ci], `node-version: "22"`) {
				t.Fatal("canonical CI runs Node-based queue and metadata regressions; install the pinned Node runtime before invoking it")
			}
		})
	}
}
