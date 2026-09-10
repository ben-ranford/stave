package requirements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowGuardRunsWithActionlintValidation(t *testing.T) {
	checkerPath := filepath.Join("..", "scripts", "rigor", "check-workflows.sh")
	checker, err := os.ReadFile(checkerPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"-config-file \"${repo_root}/.github/actionlint.yaml\"",
		"workflow-guard",
		"go test -mod=readonly ./...",
		"go run -mod=readonly . \"${repo_root}/.github/workflows\"",
	} {
		if !strings.Contains(string(checker), fragment) {
			t.Fatalf("workflow validation must retain %q", fragment)
		}
	}
}
