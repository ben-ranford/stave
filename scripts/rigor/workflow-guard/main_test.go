package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckWorkflowAcceptsSupportedSyntax(t *testing.T) {
	workflow := `on: push
jobs:
  normal:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@0123456789012345678901234567890123456789
        with:
          persist-credentials: false
      - "uses": "actions/setup-go@0123456789012345678901234567890123456789"
        with: { "cache": false }
  flow: { runs-on: [ubuntu-24.04], steps: [{ uses: actions/checkout@0123456789012345678901234567890123456789, with: { persist-credentials: false } }] }
`
	if err := checkWorkflow("workflow.yml", []byte(workflow)); err != nil {
		t.Fatalf("supported workflow rejected: %v", err)
	}
	mixedCaseWorkflow := strings.NewReplacer(
		"actions/checkout@", "Actions/Checkout@",
		"actions/setup-go@", "ACTIONS/SETUP-GO@",
	).Replace(workflow)
	if err := checkWorkflow("workflow.yml", []byte(mixedCaseWorkflow)); err != nil {
		t.Fatalf("supported mixed-case actions rejected: %v", err)
	}
}

func TestCheckWorkflowRejectsUnsafeTrustBoundaries(t *testing.T) {
	workflowPrefix := "on: push\njobs:\n"
	validJob := `
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@0123456789012345678901234567890123456789
        with:
          persist-credentials: false
      - uses: actions/setup-go@0123456789012345678901234567890123456789
        with:
          cache: false
`
	for name, workflow := range map[string]string{
		"reusable workflow":                     workflowPrefix + "  call:\n    uses: ./.github/workflows/reusable.yml\n",
		"missing runner":                        workflowPrefix + "  missing:\n    steps:\n      - run: true\n",
		"runner group":                          workflowPrefix + "  grouped:\n    runs-on: { group: trusted, labels: ubuntu-24.04 }\n    steps:\n      - run: true\n",
		"runner expression":                     workflowPrefix + "  dynamic:\n    runs-on: ${{ github.event.inputs.runner }}\n    steps:\n      - run: true\n",
		"runner array":                          workflowPrefix + "  multiple:\n    runs-on: [ubuntu-24.04, self-hosted]\n    steps:\n      - run: true\n",
		"wrong runner":                          workflowPrefix + "  unsafe:\n    runs-on: stave-arc\n    steps:\n      - run: true\n",
		"local composite action":                workflowPrefix + "  unsafe:\n    runs-on: ubuntu-24.04\n    steps:\n      - uses: ./.github/actions/bootstrap\n",
		"missing checkout credential setting":   workflowPrefix + "  unsafe:" + strings.Replace(validJob, "\n          persist-credentials: false", "", 1),
		"duplicate checkout credential setting": workflowPrefix + "  unsafe:" + strings.Replace(validJob, "persist-credentials: false", "persist-credentials: false\n          persist-credentials: false", 1),
		"cached Go":                             workflowPrefix + "  unsafe:" + strings.Replace(validJob, "cache: false", "cache: true", 1),
		"credential expression":                 workflowPrefix + "  unsafe:" + strings.Replace(validJob, "persist-credentials: false", "persist-credentials: ${{ false }}", 1),
		"mixed-case checkout":                   workflowPrefix + "  unsafe:" + strings.Replace(strings.Replace(validJob, "actions/checkout@", "Actions/Checkout@", 1), "persist-credentials: false", "persist-credentials: true", 1),
		"mixed-case setup-go":                   workflowPrefix + "  unsafe:" + strings.Replace(strings.Replace(validJob, "actions/setup-go@", "ACTIONS/SETUP-GO@", 1), "cache: false", "cache: true", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkWorkflow("workflow.yml", []byte(workflow)); err == nil {
				t.Fatal("unsafe workflow accepted")
			}
		})
	}
}

func TestCheckWorkflowsFindsYAMLFiles(t *testing.T) {
	directory := t.TempDir()
	workflow := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: true\n")
	for _, name := range []string{"current.yml", "future.yaml"} {
		if err := os.WriteFile(filepath.Join(directory, name), workflow, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkWorkflows(directory); err != nil {
		t.Fatal(err)
	}
}
