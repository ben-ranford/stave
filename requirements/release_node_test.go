package requirements

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var setupNode = regexp.MustCompile(`(?m)^\s*uses:\s*actions/setup-node@([0-9a-f]{40})(?:\s+#.*)?\n\s*with:\n\s*node-version:\s*"([^"]+)"`)

func TestReleaseWorkflowsProvideNodeForCanonicalCI(t *testing.T) {
	ci := workflowNodeSet(t, "ci.yml")
	if len(ci) != 3 {
		t.Fatalf("canonical CI has %d setup-node installations, want 3", len(ci))
	}
	canonical := ci[0]
	for _, node := range ci[1:] {
		if node != canonical {
			t.Fatalf("canonical CI setup-node installations differ: %q and %q", canonical, node)
		}
	}
	for _, name := range []string{"release.yml", "prerelease-dry-run.yml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			workflow := string(data)
			nodes := setupNode.FindAllStringSubmatchIndex(workflow, -1)
			if len(nodes) != 1 {
				t.Fatalf("release workflow has %d setup-node installations, want 1", len(nodes))
			}
			node := workflow[nodes[0][2]:nodes[0][3]] + "@" + workflow[nodes[0][4]:nodes[0][5]]
			ci := strings.Index(workflow, "make ci")
			if ci < 0 || nodes[0][0] > ci || node != canonical {
				t.Fatalf("canonical CI runs Node-based queue and metadata regressions; install the canonical pinned Node runtime before invoking it (got %q, want %q)", node, canonical)
			}
		})
	}
}

func workflowNodeSet(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	matches := setupNode.FindAllStringSubmatch(string(data), -1)
	nodes := make([]string, 0, len(matches))
	for _, match := range matches {
		nodes = append(nodes, match[1]+"@"+match[2])
	}
	return nodes
}
