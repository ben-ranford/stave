package requirements

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestQueueMeWorkflowRuntimeGate(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required to test queue-me workflow gates")
	}
	command := exec.Command(node, "--test", "queue_me_workflow_gate.test.js")
	command.Dir = filepath.Join("..", "scripts")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("queue-me workflow gate tests failed: %v\n%s", err, output)
	}
}
