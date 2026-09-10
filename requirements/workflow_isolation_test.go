package requirements

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowTrustBoundaryContract(t *testing.T) {
	workflowDirectory := filepath.Join("..", ".github", "workflows")
	names, err := workflowFileNames(workflowDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			workflow, err := os.ReadFile(filepath.Join(workflowDirectory, name))
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

func TestWorkflowFileNamesIncludesYAML(t *testing.T) {
	workflowDirectory := t.TempDir()
	for _, name := range []string{"current.yml", "future.yaml", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(workflowDirectory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	names, err := workflowFileNames(workflowDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "current.yml,future.yaml"; got != want {
		t.Fatalf("workflow files = %q, want %q", got, want)
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
	if err := validateWorkflowTrustBoundary(strings.Replace(valid, "runs-on:", "runs-on :", 1)); err != nil {
		t.Fatalf("valid spaced runner key rejected: %v", err)
	}
	spacedCheckout := strings.Replace(valid, "uses: actions/checkout@", "uses : actions/checkout@", 1)
	if err := validateWorkflowTrustBoundary(spacedCheckout); err != nil {
		t.Fatalf("valid spaced checkout key rejected: %v", err)
	}
	inlineCheckout := strings.Replace(valid, "      - name: Check out repository\n        uses:", "      - uses:", 1)
	if err := validateWorkflowTrustBoundary(inlineCheckout); err != nil {
		t.Fatalf("valid inline checkout step rejected: %v", err)
	}
	for name, workflow := range map[string]string{
		"flow-style self-hosted job": valid + "\n  unsafe: {runs-on: stave-arc, steps: []}\n",
		"flow-style checkout step":   strings.Replace(valid, "      - name: Check out repository\n        uses:", "      - { uses:", 1),
		"spaced checkout key":        strings.Replace(spacedCheckout, "persist-credentials: false", "persist-credentials: true", 1),
		"inline checkout step":       strings.Replace(inlineCheckout, "persist-credentials: false", "persist-credentials: true", 1),
		"self-hosted runner":         strings.Replace(valid, "ubuntu-24.04", "stave-arc", 1),
		"spaced self-hosted runner":  strings.Replace(valid, "runs-on: ubuntu-24.04", "runs-on : stave-arc", 1),
		"array runner":               strings.Replace(valid, "ubuntu-24.04", "[ubuntu-24.04]", 1),
		"multiline runner":           strings.Replace(valid, "runs-on: ubuntu-24.04", "runs-on:\n      - ubuntu-24.04", 1),
		"persisted token":            strings.Replace(valid, "persist-credentials: false", "persist-credentials: true", 1),
		"missing token setting":      strings.Replace(valid, "\n          persist-credentials: false", "", 1),
		"duplicate token setting":    strings.Replace(valid, "persist-credentials: false", "persist-credentials: false\n          persist-credentials: true", 1),
		"nested token setting":       strings.Replace(valid, "persist-credentials: false", "nested:\n            persist-credentials: false", 1),
		"Go cache":                   strings.Replace(valid, "cache: false", "cache: true", 1),
		"missing Go cache":           strings.Replace(valid, "\n          cache: false", "", 1),
		"duplicate Go cache":         strings.Replace(valid, "cache: false", "cache: false\n          cache: true", 1),
		"environment cache":          strings.Replace(valid, "cache: false", "cache-key: disabled\n        env:\n          cache: false", 1),
		"shell cache":                strings.Replace(valid, "cache: false", "cache-key: disabled\n        run: |\n          cache: false", 1),
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
	if hasUnsupportedFlowStyleTrustBoundary(lines) {
		return fmt.Errorf("workflow uses unsupported flow-style runner or action mapping")
	}
	runnerCount := 0
	for _, line := range lines {
		key, runner, found := yamlKeyValue(line)
		if !found || key != "runs-on" {
			continue
		}
		runnerCount++
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

func workflowFileNames(workflowDirectory string) ([]string, error) {
	entries, err := os.ReadDir(workflowDirectory)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := filepath.Ext(entry.Name())
		if extension == ".yml" || extension == ".yaml" {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("workflow directory has no YAML files")
	}
	return names, nil
}

func workflowStepsHaveExactlySetting(lines []string, action, setting string) bool {
	settingKey, settingValue, found := yamlKeyValue(setting)
	if !found {
		return false
	}
	for index, line := range lines {
		if !actionUses(line, action) {
			continue
		}
		stepIndent, found := actionStepIndent(lines, index)
		if !found {
			return false
		}
		if !actionWithHasExactlySetting(lines, index, stepIndent, actionPropertyIndent(lines[index]), settingKey, settingValue) {
			return false
		}
	}
	return true
}

func hasUnsupportedFlowStyleTrustBoundary(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		_, value, found := yamlKeyValue(line)
		if (found && strings.HasPrefix(value, "{")) || strings.HasPrefix(trimmed, "- {") {
			if strings.Contains(line, "runs-on") || strings.Contains(line, "uses") {
				return true
			}
		}
		if strings.HasPrefix(trimmed, "{") && (strings.Contains(line, "runs-on") || strings.Contains(line, "uses")) {
			return true
		}
	}
	return false
}

func actionUses(line, action string) bool {
	key, value, found := yamlKeyValue(actionProperty(line))
	return found && key == "uses" && strings.HasPrefix(value, action)
}

func actionStepIndent(lines []string, index int) (int, bool) {
	if isStep(lines[index]) {
		return indentation(lines[index]), true
	}
	return enclosingStepIndent(lines, index)
}

func actionPropertyIndent(line string) int {
	indent := indentation(line)
	if isStep(line) {
		return indent + 2
	}
	return indent
}

func actionProperty(line string) string {
	trimmed := strings.TrimSpace(line)
	if isStep(line) {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	}
	return trimmed
}

func actionWithHasExactlySetting(lines []string, usesIndex, stepIndent, propertyIndent int, settingKey, settingValue string) bool {
	withIndex, found := actionWithIndex(lines, usesIndex, stepIndent, propertyIndent)
	if !found {
		return false
	}
	settingCount, correctSettingCount := directWithSettingCounts(lines, withIndex, settingKey, settingValue)
	return settingCount == 1 && correctSettingCount == 1
}

func actionWithIndex(lines []string, usesIndex, stepIndent, propertyIndent int) (int, bool) {
	withIndex := -1
	for index := usesIndex + 1; index < len(lines); index++ {
		line := lines[index]
		if isStep(line) && indentation(line) <= stepIndent {
			break
		}
		key, _, found := yamlKeyValue(line)
		if indentation(line) != propertyIndent || !found || key != "with" {
			continue
		}
		if withIndex >= 0 {
			return 0, false
		}
		withIndex = index
	}
	return withIndex, withIndex >= 0
}

func directWithSettingCounts(lines []string, withIndex int, settingKey, settingValue string) (int, int) {
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
		key, value, found := yamlKeyValue(trimmed)
		if lineIndent == directChildIndent && found && key == settingKey {
			settingCount++
			if value == settingValue {
				correctSettingCount++
			}
		}
	}
	return settingCount, correctSettingCount
}

func yamlKeyValue(line string) (string, string, bool) {
	key, value, found := strings.Cut(strings.TrimSpace(line), ":")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
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
