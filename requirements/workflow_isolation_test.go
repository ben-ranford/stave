package requirements

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowTrustBoundaryContract(t *testing.T) {
	for _, name := range []string{
		"ci.yml",
		"pr-metadata.yml",
		"prerelease-dry-run.yml",
		"queue-me.yml",
		"release-please.yml",
		"release.yml",
	} {
		t.Run(name, func(t *testing.T) {
			workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			if err := validateWorkflowTrustBoundary(string(workflow)); err != nil {
				t.Fatal(err)
			}
		})
	}

	config, err := os.ReadFile(filepath.Join("..", ".github", "actionlint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "stave-arc") {
		t.Fatal("actionlint configuration must not retain the retired self-hosted runner label")
	}

	checker, err := os.ReadFile(filepath.Join("..", "scripts", "rigor", "check-workflows.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(checker), "-config-file \"${repo_root}/.github/actionlint.yaml\"") {
		t.Fatal("workflow validation must continue to use the repository actionlint configuration")
	}
}

func TestValidateWorkflowTrustBoundaryRejectsUnsafeSettings(t *testing.T) {
	valid := `jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - name: Check out repository
        uses: actions/checkout@0123456789012345678901234567890123456789
        with:
          persist-credentials: false
      - name: Set up Go
        uses: actions/setup-go@0123456789012345678901234567890123456789
        with:
          cache: false
`
	if err := validateWorkflowTrustBoundary(valid); err != nil {
		t.Fatalf("valid workflow rejected: %v", err)
	}
	for name, workflow := range map[string]string{
		"self-hosted runner":      strings.Replace(valid, "ubuntu-24.04", "stave-arc", 1),
		"array runner":            strings.Replace(valid, "ubuntu-24.04", "[ubuntu-24.04]", 1),
		"multiline runner":        strings.Replace(valid, "runs-on: ubuntu-24.04", "runs-on:\n      - ubuntu-24.04", 1),
		"persisted token":         strings.Replace(valid, "persist-credentials: false", "persist-credentials: true", 1),
		"missing token setting":   strings.Replace(valid, "\n          persist-credentials: false", "", 1),
		"duplicate token setting": strings.Replace(valid, "persist-credentials: false", "persist-credentials: false\n          persist-credentials: true", 1),
		"nested token setting":    strings.Replace(valid, "persist-credentials: false", "nested:\n            persist-credentials: false", 1),
		"Go cache":                strings.Replace(valid, "cache: false", "cache: true", 1),
		"missing Go cache":        strings.Replace(valid, "\n          cache: false", "", 1),
		"duplicate Go cache":      strings.Replace(valid, "cache: false", "cache: false\n          cache: true", 1),
		"environment cache":       strings.Replace(valid, "cache: false", "cache-key: disabled\n        env:\n          cache: false", 1),
		"shell cache":             strings.Replace(valid, "cache: false", "cache-key: disabled\n        run: |\n          cache: false", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateWorkflowTrustBoundary(workflow); err == nil {
				t.Fatal("unsafe workflow was accepted")
			}
		})
	}
}

func validateWorkflowTrustBoundary(workflow string) error {
	lines := strings.Split(workflow, "\n")
	runnerCount := 0
	for _, line := range lines {
		setting := strings.TrimSpace(line)
		if !strings.HasPrefix(setting, "runs-on:") {
			continue
		}
		runnerCount++
		runner := strings.TrimSpace(strings.TrimPrefix(setting, "runs-on:"))
		if runner != "ubuntu-24.04" {
			return fmt.Errorf("workflow runner %q is not the hosted ubuntu-24.04 boundary", runner)
		}
	}
	if runnerCount == 0 {
		return fmt.Errorf("workflow has no jobs with a runner")
	}
	if !workflowStepsHaveExactlySetting(lines, "actions/checkout@", "persist-credentials: false") {
		return fmt.Errorf("checkout step retains credentials")
	}
	if !workflowStepsHaveExactlySetting(lines, "actions/setup-go@", "cache: false") {
		return fmt.Errorf("setup-go cache must remain disabled across workflow trust boundaries")
	}
	return nil
}

func workflowStepsHaveExactlySetting(lines []string, action, setting string) bool {
	settingKey := strings.SplitN(setting, ":", 2)[0] + ":"
	for index, line := range lines {
		if !strings.Contains(line, "uses: "+action) {
			continue
		}
		stepIndent, found := enclosingStepIndent(lines, index)
		if !found {
			return false
		}
		if !actionWithHasExactlySetting(lines, index, stepIndent, settingKey, setting) {
			return false
		}
	}
	return true
}

func actionWithHasExactlySetting(lines []string, usesIndex, stepIndent int, settingKey, setting string) bool {
	withIndex, found := actionWithIndex(lines, usesIndex, stepIndent)
	if !found {
		return false
	}
	settingCount, correctSettingCount := directWithSettingCounts(lines, withIndex, settingKey, setting)
	return settingCount == 1 && correctSettingCount == 1
}

func actionWithIndex(lines []string, usesIndex, stepIndent int) (int, bool) {
	usesIndent := indentation(lines[usesIndex])
	withIndex := -1
	for index := usesIndex + 1; index < len(lines); index++ {
		line := lines[index]
		if isStep(line) && indentation(line) <= stepIndent {
			break
		}
		if indentation(line) != usesIndent || strings.TrimSpace(line) != "with:" {
			continue
		}
		if withIndex >= 0 {
			return 0, false
		}
		withIndex = index
	}
	return withIndex, withIndex >= 0
}

func directWithSettingCounts(lines []string, withIndex int, settingKey, setting string) (int, int) {
	withIndent := indentation(lines[withIndex])
	directChildIndent := -1
	settingCount := 0
	correctSettingCount := 0
	for index := withIndex + 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lineIndent := indentation(line)
		if lineIndent <= withIndent {
			break
		}
		if directChildIndent < 0 {
			directChildIndent = lineIndent
		}
		if lineIndent == directChildIndent && strings.HasPrefix(trimmed, settingKey) {
			settingCount++
			if trimmed == setting {
				correctSettingCount++
			}
		}
	}
	return settingCount, correctSettingCount
}

func enclosingStepIndent(lines []string, index int) (int, bool) {
	usesIndent := indentation(lines[index])
	for previous := index - 1; previous >= 0; previous-- {
		if isStep(lines[previous]) && indentation(lines[previous]) < usesIndent {
			return indentation(lines[previous]), true
		}
	}
	return 0, false
}

func isStep(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "- ")
}

func indentation(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
