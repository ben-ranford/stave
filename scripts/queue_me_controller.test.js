'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');

const runController = require('./queue_me_controller.js');
const { testables } = runController;

function makePull(number, overrides = {}) {
  return {
    number,
    node_id: `PR_${number}`,
    title: `feat: pull ${number}`,
    body: '## Summary\n\nValid metadata.',
    user: { login: `author-${number}` },
    labels: [{ name: 'queue-me' }],
    draft: false,
    maintainer_can_modify: true,
    base: {
      ref: 'main',
      repo: {
        name: 'stave',
        owner: { login: 'octo' },
      },
    },
    head: {
      sha: `head-${number}`,
      ref: `branch-${number}`,
      repo: { full_name: 'octo/stave' },
    },
    ...overrides,
  };
}

function makeHarness(options = {}) {
  const pulls = options.pulls || [];
  const eventPull = options.eventPull;
  const branchSHAs = options.branchSHAs || ['base-sha'];
  const openPulls = options.openPulls || pulls;
  const allPulls = [...new Map(
    [...pulls, ...openPulls, ...(eventPull ? [eventPull] : [])]
      .map((pull) => [pull.number, pull]),
  ).values()];
  const states = new Map(
    allPulls.map((pull) => [
      pull.number,
      {
        id: pull.node_id,
        number: pull.number,
        baseRefName: pull.base.ref,
        baseRefOid: branchSHAs[0],
        headRefOid: pull.head.sha,
        isDraft: pull.draft,
        mergeable: 'MERGEABLE',
        mergeStateStatus: 'BLOCKED',
        autoMergeRequest: null,
        ...(options.initialStates?.[pull.number] || {}),
      },
    ]),
  );
  const metadataChecks = options.metadataChecks || allPulls.map((pull) => ({
    name: 'pr-metadata',
    conclusion: 'success',
    external_id: testables.metadataCheckExternalID(pull),
    app: { slug: 'github-actions' },
    head_sha: pull.head.sha,
    details_url: 'https://github.com/octo/stave/actions/runs/123',
  }));
  const workflowJobs = options.workflowJobs || metadataChecks.map((check) => ({
    name: `metadata/${check.external_id}`,
    conclusion: 'success',
  }));
  const comments = new Map();
  const branchRules = options.branchRules ?? [{
    type: 'required_status_checks',
    parameters: {
      strict_required_status_checks_policy: true,
      required_status_checks: [{ context: 'ci / contracts' }],
    },
  }];
  const calls = {
    branchReads: [],
    comments: [],
    createdLabels: [],
    disabled: [],
    mergeInputs: [],
    merged: [],
    notices: [],
    rebased: [],
    repositoryReads: [],
    rules: [],
    workflowRuns: [],
  };
  const repository = {
    default_branch: 'main',
    full_name: 'octo/stave',
    allow_squash_merge: true,
    allow_merge_commit: false,
    allow_rebase_merge: false,
    squash_merge_commit_title: 'PR_TITLE',
  };
  const repositoryResponses = options.repositoryResponses || [repository];

  const github = {
    rest: {
      issues: {
        getLabel: async () => {
          if (options.labelMissing) {
            const error = new Error('label missing');
            error.status = 404;
            throw error;
          }
        },
        createLabel: async (input) => {
          calls.createdLabels.push(input.name);
        },
        listComments: async () => {},
        createComment: async (input) => {
          const comment = { id: calls.comments.length + 1, body: input.body, user: { type: 'Bot' } };
          comments.set(input.issue_number, [comment]);
          calls.comments.push({ number: input.issue_number, body: input.body });
        },
        updateComment: async (input) => {
          for (const [number, issueComments] of comments) {
            const existing = issueComments.find((comment) => comment.id === input.comment_id);
            if (existing) {
              existing.body = input.body;
              calls.comments.push({ number, body: input.body });
              return;
            }
          }
          throw new Error(`unknown comment ${input.comment_id}`);
        },
      },
      pulls: {
        get: async ({ pull_number }) => {
          const pull = options.currentPulls?.[pull_number] || allPulls.find(
            (candidate) => candidate.number === pull_number,
          );
          if (!pull) {
            throw new Error(`unknown pull request ${pull_number}`);
          }
          return { data: pull };
        },
        list: async () => {},
      },
      checks: {
        listForRef: async () => {},
      },
      actions: {
        getWorkflowRun: async ({ run_id }) => {
          calls.workflowRuns.push(run_id);
          return { data: options.workflowRun || {
            conclusion: 'success',
            event: 'pull_request_target',
            path: '.github/workflows/pr-metadata.yml@main',
            head_branch: 'main',
            pull_requests: allPulls.map((pull) => ({
              number: pull.number,
              base: { ref: pull.base.ref, repo: { full_name: 'octo/stave' } },
            })),
          } };
        },
        listJobsForWorkflowRun: async () => {},
      },
      repos: {
        get: async () => {
          const response = repositoryResponses[
            Math.min(calls.repositoryReads.length, repositoryResponses.length - 1)
          ];
          calls.repositoryReads.push(response);
          return { data: response };
        },
        getBranch: async () => {
          const sha = branchSHAs[Math.min(calls.branchReads.length, branchSHAs.length - 1)];
          calls.branchReads.push(sha);
          return { data: { commit: { sha } } };
        },
        compareCommitsWithBasehead: async ({ basehead }) => {
          const pull = allPulls.find((candidate) => basehead.endsWith(`...${candidate.head.sha}`));
          return {
            data: {
              status: options.comparisonStatuses?.[pull?.number] || options.comparisonStatus || 'ahead',
            },
          };
        },
      },
    },
    paginate: async (_method, input) => {
      if (input.issue_number) {
        return comments.get(input.issue_number) || [];
      }
      if (_method === github.rest.checks.listForRef) {
        return metadataChecks.filter((check) => check.head_sha === input.ref);
      }
      if (_method === github.rest.actions.listJobsForWorkflowRun) {
        return workflowJobs;
      }
      return input.base
        ? openPulls.filter((pull) => pull.base?.ref === input.base)
        : openPulls;
    },
    request: async (route, input) => {
      calls.rules.push({ route, input });
      if (options.rulesError) {
        throw options.rulesError;
      }
      return { data: branchRules };
    },
    graphql: async (query, variables) => {
      if (query.includes('QueuePullState($owner')) {
        const state = states.get(variables.number);
        if (calls.branchReads.length >= 2 && options.stateAfterFinalBranchRead?.[variables.number]) {
          Object.assign(state, options.stateAfterFinalBranchRead[variables.number]);
        }
        return { repository: { pullRequest: { ...state } } };
      }
      if (query.includes('QueuePullStateByID')) {
        const state = [...states.values()].find((value) => value.id === variables.pullRequestId);
        return {
          node: { ...state },
        };
      }
      if (query.includes('DisableQueueAutoMerge')) {
        const state = [...states.values()].find((value) => value.id === variables.pullRequestId);
        state.autoMergeRequest = null;
        calls.disabled.push(state.number);
        return { disablePullRequestAutoMerge: { pullRequest: { number: state.number } } };
      }
      if (query.includes('RebaseQueuedPull')) {
        const state = [...states.values()].find((value) => value.id === variables.pullRequestId);
        const rebaseError = options.rebaseErrors?.[state.number] || options.rebaseError;
        if (rebaseError) {
          throw rebaseError;
        }
        state.headRefOid = options.rebasedHead || `rebased-${state.number}`;
        calls.rebased.push(state.number);
        return { updatePullRequestBranch: { pullRequest: state } };
      }
      if (query.includes('MergeQueuedPull')) {
        const state = [...states.values()].find((value) => value.id === variables.pullRequestId);
        calls.mergeInputs.push(variables);
        calls.merged.push(state.number);
        return { mergePullRequest: { pullRequest: { number: state.number, merged: true } } };
      }
      throw new Error(`unexpected GraphQL operation: ${query}`);
    },
  };

  const payload = eventPull
    ? {
        action: options.action || 'labeled',
        label: { name: 'queue-me' },
        pull_request: eventPull,
        sender: options.sender || { login: 'octocat', type: 'User' },
      }
    : {};
  return {
    args: {
      github,
      context: {
        repo: { owner: 'octo', repo: 'stave' },
        eventName: eventPull ? 'pull_request_target' : 'workflow_dispatch',
        payload,
      },
      core: {
        notice: (message) => calls.notices.push(message),
      },
      queueAppSlug: options.queueAppSlug,
    },
    calls,
    pulls,
    states,
  };
}

function commentsFor(harness, number) {
  return harness.calls.comments
    .filter((comment) => comment.number === number)
    .map((comment) => comment.body)
    .at(-1) || '';
}

function queueAppAutoMergeRequest() {
  return {
    enabledAt: 'before',
    mergeMethod: 'SQUASH',
    enabledBy: { login: 'queue-app[bot]' },
  };
}

async function withReleasePleaseAuthorLogin(value, run) {
  const previous = process.env.RELEASE_PLEASE_AUTHOR_LOGIN;
  process.env.RELEASE_PLEASE_AUTHOR_LOGIN = value;
  try {
    return await run();
  } finally {
    if (previous === undefined) {
      delete process.env.RELEASE_PLEASE_AUTHOR_LOGIN;
    } else {
      process.env.RELEASE_PLEASE_AUTHOR_LOGIN = previous;
    }
  }
}

test('sortQueuedPulls uses deterministic ascending PR numbers', () => {
  const sorted = testables.sortQueuedPulls([{ number: 42 }, { number: 7 }, { number: 19 }]);
  assert.deepEqual(sorted.map((pull) => pull.number), [7, 19, 42]);
});

test('hasLabel accepts REST label objects and string labels', () => {
  assert.equal(testables.hasLabel({ labels: [{ name: 'queue-me' }] }, 'queue-me'), true);
  assert.equal(testables.hasLabel({ labels: ['queue-me'] }, 'queue-me'), true);
  assert.equal(testables.hasLabel({ labels: [{ name: 'other' }] }, 'queue-me'), false);
  assert.equal(testables.hasLabel({}, 'queue-me'), false);
});

test('isBranchCurrent accepts only ancestor-preserving compare states', () => {
  assert.equal(testables.isBranchCurrent('ahead'), true);
  assert.equal(testables.isBranchCurrent('identical'), true);
  assert.equal(testables.isBranchCurrent('behind'), false);
  assert.equal(testables.isBranchCurrent('diverged'), false);
});

test('trusted metadata workflow path accepts only the default workflow forms', () => {
  assert.equal(testables.isTrustedMetadataWorkflowPath('.github/workflows/pr-metadata.yml', 'main'), true);
  assert.equal(testables.isTrustedMetadataWorkflowPath('.github/workflows/pr-metadata.yml@main', 'main'), true);
  assert.equal(testables.isTrustedMetadataWorkflowPath('.github/workflows/pr-metadata.yml@feature', 'main'), false);
  assert.equal(testables.isTrustedMetadataWorkflowPath('.github/workflows/other.yml', 'main'), false);
});

test('strict status-check policy requires at least one required context', () => {
  assert.equal(testables.hasStrictRequiredStatusChecks([{ type: 'required_status_checks', parameters: {
    strict_required_status_checks_policy: true,
    required_status_checks: [{ context: 'ci / contracts' }],
  } }]), true);
  assert.equal(testables.hasStrictRequiredStatusChecks([{ type: 'required_status_checks', parameters: {
    strict_required_status_checks_policy: false,
    required_status_checks: [{ context: 'ci / contracts' }],
  } }]), false);
  assert.equal(testables.hasStrictRequiredStatusChecks([{ type: 'required_status_checks', parameters: {
    strict_required_status_checks_policy: true,
    required_status_checks: [],
  } }]), false);
  assert.equal(testables.hasStrictRequiredStatusChecks([]), false);
});

test('queue merge policy matches the trusted metadata validator', () => {
  const policy = {
    allow_squash_merge: true,
    allow_merge_commit: false,
    allow_rebase_merge: false,
    squash_merge_commit_title: 'PR_TITLE',
  };
  assert.deepEqual(testables.mergePolicyViolations(policy), []);
  for (const [field, value, message] of [
    ['allow_squash_merge', false, /allow squash merges/],
    ['allow_merge_commit', true, /disable merge commits/],
    ['allow_rebase_merge', true, /disable rebase merges/],
    ['squash_merge_commit_title', 'COMMIT_OR_PR_TITLE', /default to the PR title/],
  ]) {
    assert.match(testables.mergePolicyViolations({ ...policy, [field]: value }).join(' '), message);
  }
});

test('queue fails closed without effective strict required checks', async (t) => {
  for (const rules of [
    [],
    [{ type: 'required_status_checks', parameters: {
      strict_required_status_checks_policy: false,
      required_status_checks: [{ context: 'ci / contracts' }],
    } }],
  ]) {
    await t.test(JSON.stringify(rules), async () => {
      const harness = makeHarness({
        pulls: [makePull(10)],
        branchRules: rules,
        labelMissing: true,
      });
      await assert.rejects(runController(harness.args), /effective strict, non-empty required status checks/);
      assert.deepEqual(harness.calls.merged, []);
      assert.deepEqual(harness.calls.createdLabels, []);
      assert.deepEqual(harness.calls.rules, [{
        route: 'GET /repos/{owner}/{repo}/rules/branches/{branch}',
        input: { owner: 'octo', repo: 'stave', branch: 'main' },
      }]);
    });
  }
});

test('status helpers bound untrusted API text', () => {
  assert.equal(testables.shortSHA('1234567890abcdef'), '1234567890');
  assert.equal(testables.shortSHA(undefined), 'unknown');
  const sanitized = testables.safeError(new Error('bad `branch`\r\ntry again'));
  assert.equal(sanitized, "bad 'branch' try again");
  assert.equal(testables.safeError('x'.repeat(1300)).length, 1200);
  assert.equal(testables.isMergeConflict(new Error('merge conflict')), true);
  assert.equal(testables.isMergeConflict(new Error('Pull Request is not mergeable')), true);
  assert.equal(testables.isMergeConflict(new Error('rate limited')), false);
});

test('controller creates the queue label and exits cleanly for an empty queue', async () => {
  const harness = makeHarness({ labelMissing: true });

  await runController(harness.args);

  assert.deepEqual(harness.calls.createdLabels, ['queue-me']);
  assert.equal(harness.calls.notices.length, 1);
  assert.match(harness.calls.notices[0], /No open main pull requests/);
});

test('controller disables follower automation and leaves a waiting leader unarmed', async () => {
  const leader = makePull(10);
  const follower = makePull(20);
  const harness = makeHarness({
    pulls: [follower, leader],
    eventPull: follower,
    initialStates: {
      20: { autoMergeRequest: { enabledAt: 'before', mergeMethod: 'SQUASH' } },
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [20]);
  assert.deepEqual(harness.calls.merged, []);
  assert.match(
    harness.calls.comments.find((comment) => comment.number === 20).body,
    /Queued behind #10/,
  );
  assert.match(
    commentsFor(harness, 10),
    /Automatic merge remains disabled while GitHub repository requirements are pending/,
  );
});

test('queue refresh updates a stale follower position after the leader advances', async () => {
  const formerLeader = makePull(3);
  const currentLeader = makePull(5);
  const follower = makePull(8);
  const harness = makeHarness({
    pulls: [formerLeader, currentLeader, follower],
    eventPull: follower,
  });

  await runController(harness.args);
  assert.match(commentsFor(harness, 8), /Queued behind #3/);
  assert.equal(commentsFor(harness, 5), '');

  harness.pulls.splice(0, harness.pulls.length, currentLeader, follower);
  harness.args.context.eventName = 'push';
  harness.args.context.payload = {};

  await runController(harness.args);

  assert.match(commentsFor(harness, 8), /Queued behind #5/);
  assert.doesNotMatch(commentsFor(harness, 8), /Queued behind #3/);
});

test('controller rebases a stale leader and waits for metadata validation on the new head', async () => {
  const leader = makePull(10);
  const harness = makeHarness({
    pulls: [leader],
    comparisonStatus: 'behind',
    initialStates: {
      10: { mergeStateStatus: 'CLEAN' },
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.rebased, [10]);
  assert.deepEqual(harness.calls.merged, []);
  assert.match(harness.calls.comments[0].body, /Rebased/);
  assert.match(commentsFor(harness, 10), /will resume after a successful current `pr-metadata` validation/);
});

test('queue pauses a title edit and remains paused on an unrelated wakeup when metadata is stale', async () => {
  const pull = makePull(10, { title: 'feat: current title' });
  const stalePull = { ...pull, title: 'feat: previous title' };
  const harness = makeHarness({
    pulls: [pull],
    queueAppSlug: 'queue-app',
    initialStates: { 10: { autoMergeRequest: queueAppAutoMergeRequest(), mergeStateStatus: 'CLEAN' } },
    metadataChecks: [{
      name: 'pr-metadata',
      conclusion: 'success',
      external_id: testables.metadataCheckExternalID(stalePull),
      app: { slug: 'github-actions' },
      head_sha: pull.head.sha,
    }],
  });

  harness.args.context.payload = {
    ...harness.args.context.payload,
    action: 'edited',
    changes: { title: { from: 'feat: previous title' } },
  };
  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [10]);
  assert.deepEqual(harness.calls.merged, []);
  assert.equal(harness.states.get(10).autoMergeRequest, null);
  assert.match(commentsFor(harness, 10), /waiting for a successful current `pr-metadata` validation/);

  harness.args.context = { ...harness.args.context, eventName: 'push', payload: {} };
  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [10]);
});

test('queue leaves a waiting pull unarmed, then directly merges after fresh validation and clean requirements', async () => {
  const pull = makePull(10);
  const harness = makeHarness({
    pulls: [pull],
    queueAppSlug: 'queue-app',
    initialStates: {
      10: { autoMergeRequest: queueAppAutoMergeRequest(), mergeStateStatus: 'BLOCKED' },
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.merged, []);
  assert.equal(harness.states.get(10).autoMergeRequest, null);
  assert.match(commentsFor(harness, 10), /Automatic merge remains disabled/);

  harness.states.get(10).mergeStateStatus = 'CLEAN';
  harness.args.context = { ...harness.args.context, eventName: 'schedule', payload: {} };
  await runController(harness.args);

  assert.deepEqual(harness.calls.merged, [10]);
  assert.deepEqual(harness.calls.mergeInputs[0], {
    pullRequestId: 'PR_10',
    expectedHeadOid: 'head-10',
    commitHeadline: pull.title,
    commitBody: pull.body,
  });
  assert.equal(harness.states.get(10).autoMergeRequest, null);
  assert.match(commentsFor(harness, 10), /GitHub squash-merged it/);
});

test('queue refuses a direct merge when the repository merge policy changes', async () => {
  const pull = makePull(10);
  const harness = makeHarness({
    pulls: [pull],
    initialStates: { 10: { mergeStateStatus: 'CLEAN' } },
    repositoryResponses: [
      {
        default_branch: 'main',
        full_name: 'octo/stave',
        allow_squash_merge: true,
        allow_merge_commit: false,
        allow_rebase_merge: false,
        squash_merge_commit_title: 'PR_TITLE',
      },
      {
        default_branch: 'main',
        full_name: 'octo/stave',
        allow_squash_merge: true,
        allow_merge_commit: false,
        allow_rebase_merge: false,
        squash_merge_commit_title: 'COMMIT_OR_PR_TITLE',
      },
    ],
  });

  await assert.rejects(runController(harness.args), /Repository squash merge titles must default to the PR title/);

  assert.deepEqual(harness.calls.merged, []);
  assert.equal(harness.calls.repositoryReads.length, 2);
  assert.match(commentsFor(harness, 10), /configured squash merge policy/);
});

test('queue rejects a matching metadata check not produced by GitHub Actions', async () => {
  const pull = makePull(10);
  const harness = makeHarness({
    pulls: [pull],
    metadataChecks: [{
      name: 'pr-metadata',
      conclusion: 'success',
      external_id: testables.metadataCheckExternalID(pull),
      app: { slug: 'untrusted-app' },
      head_sha: pull.head.sha,
    }],
  });

  await runController(harness.args);

  assert.match(commentsFor(harness, 10), /waiting for a successful current `pr-metadata` validation/);
});

test('queue rejects a forged GitHub Actions check without a trusted metadata job', async () => {
  const pull = makePull(10);
  const harness = makeHarness({
    pulls: [pull],
    workflowRun: {
      conclusion: 'success',
      event: 'pull_request_target',
      path: '.github/workflows/pr-metadata.yml@main',
      head_branch: 'main',
      pull_requests: [],
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.merged, []);
  assert.match(commentsFor(harness, 10), /waiting for a successful current `pr-metadata` validation/);
});

test('queue rejects metadata validated under a previous release author configuration', async () => {
  const pull = makePull(10);
  const checks = [{
    name: 'pr-metadata',
    conclusion: 'success',
    external_id: testables.metadataCheckExternalID(pull, 'release-bot-old'),
    app: { slug: 'github-actions' },
    head_sha: pull.head.sha,
    details_url: 'https://github.com/octo/stave/actions/runs/123',
  }];
  const jobs = [{ name: `metadata/${checks[0].external_id}`, conclusion: 'success' }];
  const harness = makeHarness({
    pulls: [pull],
    metadataChecks: checks,
    workflowJobs: jobs,
    initialStates: { 10: { mergeStateStatus: 'CLEAN' } },
  });

  await withReleasePleaseAuthorLogin('release-bot-new', async () => {
    await runController(harness.args);
    assert.deepEqual(harness.calls.merged, []);
    assert.match(commentsFor(harness, 10), /waiting for a successful current `pr-metadata` validation/);

    checks[0].external_id = testables.metadataCheckExternalID(pull, 'release-bot-new');
    jobs[0].name = `metadata/${checks[0].external_id}`;
    await runController(harness.args);
  });

  assert.deepEqual(harness.calls.merged, [10]);
});

test('queue rejects a copied metadata success from another pull request and branch', async () => {
  const target = makePull(20, {
    title: 'chore: release',
    body: '## Summary\n\nRelease metadata.',
    user: { login: 'contributor' },
    head: { sha: 'shared-head', ref: 'feature/contributor', repo: { full_name: 'octo/stave' } },
  });
  const source = makePull(10, {
    title: target.title,
    body: target.body,
    labels: target.labels,
    user: { login: 'release-bot' },
    head: { sha: 'shared-head', ref: 'release-please--branches--main', repo: { full_name: 'octo/stave' } },
  });
  const harness = makeHarness({
    pulls: [target],
    metadataChecks: [{
      name: 'pr-metadata',
      conclusion: 'success',
      external_id: testables.metadataCheckExternalID(source),
      app: { slug: 'github-actions' },
      head_sha: target.head.sha,
    }],
  });

  await runController(harness.args);

  assert.match(commentsFor(harness, 20), /waiting for a successful current `pr-metadata` validation/);
});

test('stale queue eligibility leaves manual auto-merge untouched', async () => {
  const listed = makePull(10);
  const current = makePull(10, { labels: [] });
  const harness = makeHarness({
    pulls: [listed],
    currentPulls: { 10: current },
    queueAppSlug: 'queue-app',
    initialStates: {
      10: { autoMergeRequest: { enabledAt: 'manual', mergeMethod: 'SQUASH' } },
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, []);
});

test('removing queue-me disables auto-merge and leaves an empty queue green', async () => {
  const pull = makePull(10, { labels: [] });
  const harness = makeHarness({
    eventPull: pull,
    action: 'unlabeled',
    initialStates: {
      10: { autoMergeRequest: queueAppAutoMergeRequest() },
    },
    queueAppSlug: 'queue-app',
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [10]);
  assert.match(commentsFor(harness, 10), /automatic merge is disabled/);
  assert.equal(harness.calls.notices.length, 1);
});

test('reconciles a controller-managed pull after its cleanup event is displaced', async () => {
  const removed = makePull(10, { labels: [] });
  const current = makePull(20);
  const harness = makeHarness({
    pulls: [current],
    openPulls: [removed, current],
    eventPull: current,
    initialStates: {
      10: { autoMergeRequest: queueAppAutoMergeRequest() },
    },
    queueAppSlug: 'queue-app',
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [10]);
  assert.match(commentsFor(harness, 10), /Removed from `queue-me`/);
});

test('disables a legacy queue-App auto-merge request while requirements are pending', async () => {
  const current = makePull(20);
  const harness = makeHarness({
    pulls: [current],
    queueAppSlug: 'queue-app',
    initialStates: {
      20: { autoMergeRequest: queueAppAutoMergeRequest() },
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [20]);
  assert.equal(harness.states.get(20).autoMergeRequest, null);
  assert.match(commentsFor(harness, 20), /Automatic merge remains disabled/);
});

test('reconciliation preserves unmarked manual auto-merge after queue removal', async () => {
  const removed = makePull(10, { labels: [] });
  const current = makePull(20);
  const harness = makeHarness({
    pulls: [current],
    openPulls: [removed, current],
    eventPull: current,
    initialStates: {
      10: { autoMergeRequest: { enabledAt: 'manual', mergeMethod: 'SQUASH' } },
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, []);
  assert.equal(commentsFor(harness, 10), '');
});

test('drafts and stale fork branches pause before rebase or auto-merge', async (t) => {
  const cases = [
    { name: 'draft', pull: makePull(10, { draft: true }), message: /still a draft/ },
    {
      name: 'stale fork',
      pull: makePull(10, {
        head: { sha: 'fork-head', repo: { full_name: 'contributor/stave' } },
      }),
      options: { comparisonStatus: 'behind' },
      message: /queue App cannot update it/,
    },
  ];

  for (const scenario of cases) {
    await t.test(scenario.name, async () => {
      const harness = makeHarness({ pulls: [scenario.pull], ...scenario.options });
      await runController(harness.args);
      assert.deepEqual(harness.calls.rebased, []);
      assert.match(harness.calls.comments[0].body, scenario.message);
    });
  }
});

test('a current fork branch remains unarmed while requirements are pending', async () => {
  const fork = makePull(10, {
    head: { sha: 'fork-head', repo: { full_name: 'contributor/stave' } },
  });
  const harness = makeHarness({ pulls: [fork], comparisonStatus: 'ahead' });

  await runController(harness.args);

  assert.deepEqual(harness.calls.rebased, []);
  assert.match(commentsFor(harness, 10), /Automatic merge remains disabled/);
});

test('a rebase conflict advances the queue and retries the blocked pull request after an update', async () => {
  const leader = makePull(10);
  const follower = makePull(20);
  const harness = makeHarness({
    pulls: [leader, follower],
    comparisonStatuses: { 10: 'behind', 20: 'ahead' },
    rebaseErrors: { 10: new Error('Pull Request is not mergeable') },
  });

  await runController(harness.args);

  assert.match(commentsFor(harness, 10), /because of merge conflicts/);
  assert.match(commentsFor(harness, 10), /retried after its branch is updated/);
  assert.match(commentsFor(harness, 10), /Pull Request is not mergeable/);
  assert.match(commentsFor(harness, 20), /Automatic merge remains disabled/);

  const updatedLeader = makePull(10, { head: { sha: 'updated-head-10', repo: { full_name: 'octo/stave' } } });
  const retry = makeHarness({
    pulls: [updatedLeader, follower],
    eventPull: updatedLeader,
    action: 'synchronize',
    comparisonStatuses: { 10: 'ahead' },
  });

  await runController(retry.args);

});

test('controller pauses when the default branch moves before direct merge', async () => {
  const harness = makeHarness({
    pulls: [makePull(10)],
    branchSHAs: ['base-sha', 'new-base-sha'],
  });

  await assert.rejects(runController(harness.args), /Default branch main moved/);

  assert.deepEqual(harness.calls.branchReads, ['base-sha', 'new-base-sha']);
  assert.deepEqual(harness.calls.merged, []);
  assert.match(harness.calls.comments[0].body, /Default branch main moved/);
});

test('controller revalidates baseRefName and baseRefOid immediately before direct merge', async (t) => {
  const cases = [
    {
      name: 'retargeted base pauses before direct merge',
      harness: makeHarness({
        pulls: [makePull(10)],
        stateAfterFinalBranchRead: {
          10: { baseRefName: 'release' },
        },
      }),
      message: /Pull request base changed from main to release/,
    },
    {
      name: 'base tip drift pauses before merge',
      harness: makeHarness({
        pulls: [makePull(10)],
        stateAfterFinalBranchRead: {
          10: { baseRefOid: '1234567890abcdef', mergeStateStatus: 'CLEAN' },
        },
      }),
      message: /Pull request base main moved from base-sha to 1234567890/,
    },
  ];

  for (const scenario of cases) {
    await t.test(scenario.name, async () => {
      await assert.rejects(runController(scenario.harness.args), scenario.message);

      assert.deepEqual(scenario.harness.calls.merged, []);
      assert.match(commentsFor(scenario.harness, 10), scenario.message);
    });
  }
});

test('changing a queued pull request away from main disables auto-merge', async () => {
  const pull = makePull(10);
  pull.base.ref = 'release';
  const harness = makeHarness({
    eventPull: pull,
    action: 'edited',
    initialStates: {
      10: { autoMergeRequest: queueAppAutoMergeRequest() },
    },
    queueAppSlug: 'queue-app',
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [10]);
  assert.match(commentsFor(harness, 10), /base changed to `release`/);
  assert.equal(harness.calls.notices.length, 1);
});

test('non-default-base queue events disable auto-merge', async (t) => {
  for (const action of ['labeled', 'auto_merge_enabled']) {
    await t.test(action, async () => {
      const pull = makePull(10);
      pull.base.ref = 'release';
      const harness = makeHarness({
        eventPull: pull,
        action,
        initialStates: {
          10: { autoMergeRequest: queueAppAutoMergeRequest() },
        },
        queueAppSlug: 'queue-app',
      });

      await runController(harness.args);

      assert.deepEqual(harness.calls.disabled, [10]);
      assert.match(commentsFor(harness, 10), /base changed to `release`/);
      assert.equal(harness.calls.notices.length, 1);
    });
  }
});

test('a non-default-base pause comment is not replaced by a queue position', async () => {
  const leader = makePull(10);
  const releasePull = makePull(20);
  releasePull.base.ref = 'release';
  const harness = makeHarness({
    pulls: [leader],
    eventPull: releasePull,
    action: 'labeled',
    initialStates: {
      20: { autoMergeRequest: queueAppAutoMergeRequest() },
    },
    queueAppSlug: 'queue-app',
  });

  await runController(harness.args);

  const eventComments = harness.calls.comments.filter(
    (comment) => comment.number === 20 || comment.number === undefined,
  );
  assert.deepEqual(harness.calls.disabled, [20]);
  assert.equal(eventComments.length, 1);
  assert.match(eventComments[0].body, /base changed to `release`/);
  assert.doesNotMatch(eventComments[0].body, /Queued behind/);
});

test('manually enabling auto-merge on a follower restores queue ordering', async () => {
  const leader = makePull(10);
  const follower = makePull(20);
  const harness = makeHarness({
    pulls: [leader, follower],
    eventPull: follower,
    action: 'auto_merge_enabled',
    initialStates: {
      20: { autoMergeRequest: { enabledAt: 'manual', mergeMethod: 'SQUASH' } },
    },
  });

  await runController(harness.args);

  assert.deepEqual(harness.calls.disabled, [20]);
});
