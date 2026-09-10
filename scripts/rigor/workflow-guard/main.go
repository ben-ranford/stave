package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rhysd/actionlint"
)

func main() {
	directory := filepath.Join(".github", "workflows")
	if len(os.Args) == 2 {
		directory = os.Args[1]
	}
	if err := checkWorkflows(directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkWorkflows(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	workflowCount := 0
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml") {
			continue
		}
		workflowCount++
		path := filepath.Join(directory, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := checkWorkflow(path, source); err != nil {
			return err
		}
	}
	if workflowCount == 0 {
		return fmt.Errorf("workflow directory %q has no YAML workflows", directory)
	}
	return nil
}

func checkWorkflow(path string, source []byte) error {
	workflow, diagnostics := actionlint.Parse(source)
	if len(diagnostics) != 0 {
		return fmt.Errorf("parse %s: %s", path, diagnostics[0])
	}
	for jobID, job := range workflow.Jobs {
		if err := checkJob(path, jobID, job); err != nil {
			return err
		}
	}
	return nil
}

func checkJob(path, jobID string, job *actionlint.Job) error {
	if job.WorkflowCall != nil {
		return fmt.Errorf("%s job %q calls a reusable workflow", path, jobID)
	}
	if err := checkRunner(path, jobID, job.RunsOn); err != nil {
		return err
	}
	for _, step := range job.Steps {
		action, ok := step.Exec.(*actionlint.ExecAction)
		if !ok {
			continue
		}
		if err := checkActionInputs(path, jobID, action); err != nil {
			return err
		}
	}
	return nil
}

func checkRunner(path, jobID string, runner *actionlint.Runner) error {
	if runner == nil || runner.Group != nil || runner.LabelsExpr != nil || len(runner.Labels) != 1 {
		return fmt.Errorf("%s job %q must use one literal ubuntu-24.04 runner label", path, jobID)
	}
	label := runner.Labels[0]
	if label.Value != "ubuntu-24.04" || label.ContainsExpression() {
		return fmt.Errorf("%s job %q must use one literal ubuntu-24.04 runner label", path, jobID)
	}
	return nil
}

func checkActionInputs(path, jobID string, action *actionlint.ExecAction) error {
	if action.Uses == nil {
		return fmt.Errorf("%s job %q has an action step without uses", path, jobID)
	}
	var input string
	switch actionIdentity(action.Uses.Value) {
	case "actions/checkout":
		input = "persist-credentials"
	case "actions/setup-go":
		input = "cache"
	default:
		return nil
	}
	configured, ok := action.Inputs[input]
	if !ok || configured == nil || configured.Value == nil || configured.Value.Value != "false" || configured.Value.ContainsExpression() {
		return fmt.Errorf("%s job %q action %q must set %s: false", path, jobID, action.Uses.Value, input)
	}
	return nil
}

func actionIdentity(uses string) string {
	name, _, found := strings.Cut(uses, "@")
	if !found {
		return ""
	}
	return strings.ToLower(name)
}
