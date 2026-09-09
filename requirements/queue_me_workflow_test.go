package requirements

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueueMeWorkflowContract(t *testing.T) {
	workflow := readQueueMeFile(t, ".github/workflows/queue-me.yml")
	for _, fragment := range []string{
		"pull_request_target:",
		"workflow_dispatch:",
		"push:",
		"schedule:",
		"- cron: \"*/5 * * * *\"",
		"workflow_run:",
		"- ci",
		"- pr metadata",
		"types: [completed]",
		"- main",
		"- labeled",
		"- unlabeled",
		"- synchronize",
		"- auto_merge_enabled",
		"- auto_merge_disabled",
		"cancel-in-progress: false",
		"github.event.workflow_run.conclusion == 'success'",
		"github.event_name == 'schedule'",
		"github.event.workflow_run.event == 'pull_request'",
		"github.event.workflow_run.event == 'workflow_dispatch'",
		"github.event.workflow_run.head_branch == github.event.repository.default_branch",
		"github.event.workflow_run.head_repository.full_name == github.repository",
		"github.event.workflow_run.head_branch != ''",
		"github.event.workflow_run.name != github.workflow",
		"permissions:\n  contents: read",
		"actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1",
		"app-id: ${{ vars.QUEUE_APP_CLIENT_ID }}",
		"permission-contents: write",
		"permission-checks: read",
		"permission-issues: write",
		"permission-pull-requests: write",
		"permission-workflows: write",
		"TRUSTED_CONTROLLER_REF: ${{ github.workflow_sha }}",
		"path: 'scripts/queue_me_controller.js'",
		"flag: 'wx'",
		"QUEUE_LABEL: queue-me",
		"RELEASE_PLEASE_AUTHOR_LOGIN: ${{ vars.RELEASE_PLEASE_AUTHOR_LOGIN }}",
		"require(process.env.QUEUE_CONTROLLER_PATH)",
	} {
		if !strings.Contains(workflow, fragment) {
			t.Fatalf("queue-me workflow missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"actions/checkout@",
		"github.event.pull_request.head",
		"pull_request:\n",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("queue-me workflow contains unsafe fragment %q", forbidden)
		}
	}
}

func TestPRMetadataWorkflowFreshnessContract(t *testing.T) {
	workflow := readQueueMeFile(t, ".github/workflows/pr-metadata.yml")
	for _, fragment := range []string{
		"group: pr-metadata-${{ github.repository }}-${{ github.event_name }}-${{ github.event.pull_request.number || github.event.inputs.pr-number }}",
		"cancel-in-progress: true",
		"github.event_name == 'pull_request_target'",
		"github.event_name == 'workflow_dispatch'",
		"actions: write",
		"github.rest.actions.createWorkflowDispatch",
		"workflow_id: 'pr-metadata.yml'",
		"ref: repository.default_branch",
		"github.ref == format('refs/heads/{0}', github.event.repository.default_branch)",
		"manual validation must run from the trusted default branch workflow ref",
		"RELEASE_PLEASE_AUTHOR_LOGIN: ${{ vars.RELEASE_PLEASE_AUTHOR_LOGIN }}",
		"TRUSTED_VALIDATOR_REF: ${{ github.workflow_sha }}",
		"ref: process.env.TRUSTED_VALIDATOR_REF",
		"metadata validation only supports pull requests targeting the default branch",
		"releasePleaseAuthorLogin: process.env.RELEASE_PLEASE_AUTHOR_LOGIN || ''",
		"github.rest.pulls.get",
		"github.rest.markdown.render",
		"mode: 'gfm'",
		"GitHub did not return rendered Markdown HTML.",
		"details_url: `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`",
		"text: `stave-pr-metadata-run/v1:${context.runId}`",
		"core.setOutput('external_id', metadataExternalID)",
		"attest:",
		"name: metadata/${{ needs.validate.outputs.metadata_external_id }}",
		"external_id: metadataExternalID",
		"PR_METADATA_EXTERNAL_ID",
		"const stale = metadataExternalID !== process.env.PR_METADATA_EXTERNAL_ID",
		"conclusion: success ? 'success' : 'failure'",
	} {
		if !strings.Contains(workflow, fragment) {
			t.Fatalf("pr-metadata workflow missing freshness contract %q", fragment)
		}
	}
	if strings.Contains(workflow, "PR_BASE_SHA") {
		t.Fatal("pr-metadata workflow must not source its validator from the pull request base SHA")
	}
}

func TestQueueMeControllerContract(t *testing.T) {
	controller := readQueueMeFile(t, "scripts/queue_me_controller.js")
	for _, fragment := range []string{
		"compareCommitsWithBasehead",
		"updatePullRequestBranch",
		"updateMethod: REBASE",
		"isMergeConflict",
		"rebaseQueuedPull",
		"hasFollower",
		"disablePullRequestAutoMerge",
		"mergeMethod: SQUASH",
		"mergeIfReady",
		"mergePullRequest",
		"requireStrictBranchRequirements",
		"strict_required_status_checks_policy",
		"required_status_checks.length > 0",
		"left.number - right.number",
		"metadataCheckExternalID",
		"getWorkflowRun",
		"listJobsForWorkflowRun",
		"commitHeadline",
		"commitBody",
		"releasePleaseAuthorLogin",
		"listForRef",
	} {
		if !strings.Contains(controller, fragment) {
			t.Fatalf("queue-me controller missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"enablePullRequestAutoMerge",
		"requestReviews",
		"force-push",
		"process.env.QUEUE_APP_PRIVATE_KEY",
	} {
		if strings.Contains(controller, forbidden) {
			t.Fatalf("queue-me controller contains forbidden fragment %q", forbidden)
		}
	}
}

func TestQueueMeControllerNodeSuite(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required to test the queue-me controller")
	}
	command := exec.Command(node, "--test", "queue_me_controller.test.js")
	command.Dir = filepath.Join("..", "scripts")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("queue-me node tests failed: %v\n%s", err, output)
	}
}

func readQueueMeFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", path))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
