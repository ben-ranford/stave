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

func TestMinimumGoCIValidatesWorkflowGuardWithoutToolchainUpgrade(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	minimumStart := strings.Index(string(workflow), "  minimum:\n")
	currentStart := strings.Index(string(workflow), "\n  current:")
	if minimumStart < 0 || currentStart < 0 || currentStart <= minimumStart {
		t.Fatal("minimum Go CI job boundary is missing")
	}
	minimumJob := string(workflow)[minimumStart:currentStart]
	minimumValidationCommand := "run: GOTOOLCHAIN=local make fmt-check vet suppression-check test coverage-threshold workflow-validate"
	if !strings.Contains(minimumJob, minimumValidationCommand) {
		t.Fatalf("minimum Go CI must retain %q", minimumValidationCommand)
	}
}
