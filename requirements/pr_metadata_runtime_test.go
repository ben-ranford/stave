package requirements

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestQueueMeMetadataWorkflowRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required to test metadata workflow scripts")
	}
	command := exec.Command(node, "--test", "pr_metadata_workflow.test.js")
	command.Dir = filepath.Join("..", "scripts")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("metadata workflow runtime tests failed: %v\n%s", err, output)
	}
}
