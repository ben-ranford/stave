'use strict';

const crypto = require('node:crypto');

const COMMENT_MARKER = '<!-- queue-me-controller -->';
const DEFAULT_QUEUE_LABEL = 'queue-me';
const METADATA_CHECK_NAME = 'pr-metadata';
const METADATA_CHECK_EXTERNAL_ID_PREFIX = 'stave-pr-metadata/v1:';
const METADATA_CHECK_APP_SLUG = 'github-actions';

function labelName(label) {
  return typeof label === 'string' ? label : label?.name;
}

function hasLabel(pull, queueLabel) {
  return (pull.labels || []).some((label) => labelName(label) === queueLabel);
}

function sortQueuedPulls(pulls) {
  return [...pulls].sort((left, right) => left.number - right.number);
}

function isBranchCurrent(comparisonStatus) {
  return comparisonStatus === 'ahead' || comparisonStatus === 'identical';
}

function mergePolicyViolations(repository) {
  const checks = [
    [repository.allow_squash_merge, true, 'Repository must allow squash merges'],
    [repository.allow_merge_commit, false, 'Repository must disable merge commits'],
    [repository.allow_rebase_merge, false, 'Repository must disable rebase merges'],
    [repository.squash_merge_commit_title, 'PR_TITLE', 'Repository squash merge titles must default to the PR title'],
  ];
  return checks
    .filter(([actual, expected]) => actual !== expected)
    .map(([, , message]) => message);
}

function requireQueueMergePolicy(repository) {
  const violations = mergePolicyViolations(repository);
  if (violations.length > 0) {
    throw new Error(`Queue requires the configured squash merge policy: ${violations.join('; ')}.`);
  }
}

function hasStrictRequiredStatusChecks(rules) {
  return Array.isArray(rules) && rules.some(
    (rule) =>
      rule?.type === 'required_status_checks' &&
      rule.parameters?.strict_required_status_checks_policy === true &&
      Array.isArray(rule.parameters.required_status_checks) &&
      rule.parameters.required_status_checks.length > 0,
  );
}

async function requireStrictBranchRequirements(github, owner, repo, branch) {
  const { data: rules } = await github.request(
    'GET /repos/{owner}/{repo}/rules/branches/{branch}',
    { owner, repo, branch },
  );
  if (!hasStrictRequiredStatusChecks(rules)) {
    throw new Error(
      `Queue requires effective strict, non-empty required status checks on ${branch}.`,
    );
  }
}

function shortSHA(sha) {
  return typeof sha === 'string' ? sha.slice(0, 10) : 'unknown';
}

function metadataCheckExternalID(
  pull,
  releasePleaseAuthorLogin = process.env.RELEASE_PLEASE_AUTHOR_LOGIN || '',
) {
  const metadata = {
    title: pull.title || '',
    body: pull.body || '',
    labels: (pull.labels || []).map(labelName).filter(Boolean).sort(),
    headSHA: pull.head?.sha || '',
    number: pull.number || 0,
    headRef: pull.head?.ref || '',
    headRepoFullName: pull.head?.repo?.full_name || '',
    authorLogin: pull.user?.login || '',
    baseRefName: pull.base?.ref || '',
    releasePleaseAuthorLogin,
  };
  const fingerprint = crypto.createHash('sha256').update(JSON.stringify(metadata)).digest('hex');
  return `${METADATA_CHECK_EXTERNAL_ID_PREFIX}${fingerprint}`;
}

async function getCurrentPull(github, owner, repo, number) {
  const { data: pull } = await github.rest.pulls.get({
    owner,
    repo,
    pull_number: number,
  });
  return pull;
}

async function hasCurrentMetadataValidation(github, owner, repo, pull) {
  const checks = await github.paginate(github.rest.checks.listForRef, {
    owner,
    repo,
    ref: pull.head.sha,
    check_name: METADATA_CHECK_NAME,
    filter: 'all',
    per_page: 100,
  });
  const expectedExternalID = metadataCheckExternalID(pull);
  return checks.some(
    (check) =>
      check.name === METADATA_CHECK_NAME &&
      check.conclusion === 'success' &&
      check.external_id === expectedExternalID &&
      check.app?.slug === METADATA_CHECK_APP_SLUG,
  );
}

function safeError(error) {
  const message = error instanceof Error ? error.message : String(error);
  return message.replace(/[\r\n]+/g, ' ').replaceAll('`', "'").slice(0, 1200);
}

function isMergeConflict(error) {
  return /\bconflict(?:s|ed|ing)?\b|\bpull request is not mergeable\b/i.test(safeError(error));
}

async function ensureQueueLabel(github, owner, repo, queueLabel) {
  try {
    await github.rest.issues.getLabel({ owner, repo, name: queueLabel });
  } catch (error) {
    if (error?.status !== 404) {
      throw error;
    }
    await github.rest.issues.createLabel({
      owner,
      repo,
      name: queueLabel,
      color: '1D76DB',
      description: 'Rebase and squash-merge automatically in deterministic PR order',
    });
  }
}

async function pullState(github, owner, repo, number) {
  const result = await github.graphql(
    `query QueuePullState($owner: String!, $repo: String!, $number: Int!) {
      repository(owner: $owner, name: $repo) {
        pullRequest(number: $number) {
          id
          number
          baseRefName
          baseRefOid
          headRefOid
          isDraft
          mergeable
          mergeStateStatus
          autoMergeRequest {
            enabledAt
            mergeMethod
            enabledBy { login }
          }
        }
      }
    }`,
    { owner, repo, number },
  );
  return result.repository.pullRequest;
}

function assertExpectedBaseState(state, expectedBaseRefName, expectedBaseRefOid) {
  if (state.baseRefName !== expectedBaseRefName) {
    throw new Error(
      `Pull request base changed from ${expectedBaseRefName} to ${state.baseRefName || 'unknown'} while advancing the queue.`,
    );
  }
  if (state.baseRefOid !== expectedBaseRefOid) {
    throw new Error(
      `Pull request base ${expectedBaseRefName} moved from ${shortSHA(expectedBaseRefOid)} to ${shortSHA(state.baseRefOid)} while advancing the queue.`,
    );
  }
}

async function syncStatusComment(
  github,
  owner,
  repo,
  number,
  body,
  { createIfMissing = true } = {},
) {
  const comments = await github.paginate(github.rest.issues.listComments, {
    owner,
    repo,
    issue_number: number,
    per_page: 100,
  });
  const existing = comments.find(
    (comment) =>
      comment.user?.type === 'Bot' &&
      typeof comment.body === 'string' &&
      comment.body.includes(COMMENT_MARKER),
  );
  const nextBody = `${COMMENT_MARKER}\n${body}`;
  if (existing?.body === nextBody) {
    return;
  }
  if (existing) {
    await github.rest.issues.updateComment({
      owner,
      repo,
      comment_id: existing.id,
      body: nextBody,
    });
    return;
  }
  if (!createIfMissing) {
    return;
  }
  await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: number,
    body: nextBody,
  });
}

async function disableAutoMerge(github, owner, repo, number) {
  const state = await pullState(github, owner, repo, number);
  if (!state?.autoMergeRequest) {
    return;
  }
  await github.graphql(
    `mutation DisableQueueAutoMerge($pullRequestId: ID!) {
      disablePullRequestAutoMerge(input: { pullRequestId: $pullRequestId }) {
        pullRequest { number }
      }
    }`,
    { pullRequestId: state.id },
  );
}

async function disableManagedAutoMerge(github, owner, repo, number, queueAppSlug) {
  const state = await pullState(github, owner, repo, number);
  if (
    !queueAppSlug ||
    state?.autoMergeRequest?.enabledBy?.login !== `${queueAppSlug}[bot]`
  ) {
    return false;
  }
  await github.graphql(
    `mutation DisableQueueAutoMerge($pullRequestId: ID!) {
      disablePullRequestAutoMerge(input: { pullRequestId: $pullRequestId }) {
        pullRequest { number }
      }
    }`,
    { pullRequestId: state.id },
  );
  return true;
}

async function rebaseOntoDefault(
  github,
  pull,
  defaultBranchSHA,
  { canUpdateBranch = true } = {},
) {
  const { data: comparison } = await github.rest.repos.compareCommitsWithBasehead({
    owner: pull.base.repo.owner.login,
    repo: pull.base.repo.name,
    basehead: `${defaultBranchSHA}...${pull.head.sha}`,
  });
  if (isBranchCurrent(comparison.status)) {
    return { headSHA: pull.head.sha, rebased: false };
  }
  if (!canUpdateBranch) {
    return { headSHA: pull.head.sha, rebased: false, needsManualRebase: true };
  }
  const result = await github.graphql(
    `mutation RebaseQueuedPull($pullRequestId: ID!, $expectedHeadOid: GitObjectID!) {
      updatePullRequestBranch(input: {
        pullRequestId: $pullRequestId
        expectedHeadOid: $expectedHeadOid
        updateMethod: REBASE
      }) {
        pullRequest {
          headRefOid
          number
        }
      }
    }`,
    {
      pullRequestId: pull.node_id,
      expectedHeadOid: pull.head.sha,
    },
  );
  return {
    headSHA: result.updatePullRequestBranch.pullRequest.headRefOid,
    rebased: true,
  };
}

async function mergeNow(github, pullRequestId, expectedHeadOid) {
  return github.graphql(
    `mutation MergeQueuedPull($pullRequestId: ID!, $expectedHeadOid: GitObjectID!) {
      mergePullRequest(input: {
        pullRequestId: $pullRequestId
        expectedHeadOid: $expectedHeadOid
        mergeMethod: SQUASH
      }) {
        pullRequest { number merged mergedAt }
      }
    }`,
    { pullRequestId, expectedHeadOid },
  );
}

async function mergeIfReady(github, owner, repo, number, state, { expectedBaseRefName, expectedBaseRefOid }) {
  assertExpectedBaseState(state, expectedBaseRefName, expectedBaseRefOid);
  if (state.autoMergeRequest) {
    await disableAutoMerge(github, owner, repo, number);
    return 'auto-merge-disabled';
  }
  if (state.mergeable === 'MERGEABLE' && state.mergeStateStatus === 'CLEAN') {
    await mergeNow(github, state.id, state.headRefOid);
    return 'merged';
  }
  return 'waiting';
}

async function reconcileEventPull({
  github,
  context,
  owner,
  repo,
  queueLabel,
  defaultBranch,
  eventPull,
  queueAppSlug,
}) {
  if (!eventPull || context.eventName !== 'pull_request_target') {
    return;
  }
  if (
    context.payload.action === 'unlabeled' &&
    context.payload.label?.name === queueLabel
  ) {
    if (!(await disableManagedAutoMerge(github, owner, repo, eventPull.number, queueAppSlug))) {
      return;
    }
    await syncStatusComment(
      github,
      owner,
      repo,
      eventPull.number,
      `## Queue status\n\nRemoved from \`${queueLabel}\`; automatic merge is disabled.`,
    );
    return;
  }
  if (!hasLabel(eventPull, queueLabel) || eventPull.base?.ref === defaultBranch) {
    return;
  }
  if (!(await disableManagedAutoMerge(github, owner, repo, eventPull.number, queueAppSlug))) {
    return;
  }
  await syncStatusComment(
    github,
    owner,
    repo,
    eventPull.number,
    `## Queue status\n\nQueue paused: the base changed to \`${eventPull.base?.ref || 'unknown'}\`. Automatic merge is disabled because \`${queueLabel}\` pull requests must target \`${defaultBranch}\`.`,
  );
}

async function reconcileManagedOpenPulls({
  github,
  owner,
  repo,
  queueLabel,
  defaultBranch,
  pulls,
  queueAppSlug,
}) {
  for (const pull of pulls) {
    if (hasLabel(pull, queueLabel) && pull.base?.ref === defaultBranch) {
      continue;
    }
    if (!(await disableManagedAutoMerge(github, owner, repo, pull.number, queueAppSlug))) {
      continue;
    }
    const status = hasLabel(pull, queueLabel)
      ? `Queue paused: the base changed to \`${pull.base?.ref || 'unknown'}\`. Automatic merge is disabled because \`${queueLabel}\` pull requests must target \`${defaultBranch}\`.`
      : `Removed from \`${queueLabel}\`; automatic merge is disabled.`;
    await syncStatusComment(
      github,
      owner,
      repo,
      pull.number,
      `## Queue status\n\n${status}`,
    );
  }
}

async function rebaseQueuedPull({
  github,
  owner,
  repo,
  candidate,
  defaultBranch,
  defaultBranchSHA,
  canUpdateBranch,
  hasFollower,
}) {
  try {
    return await rebaseOntoDefault(github, candidate, defaultBranchSHA, { canUpdateBranch });
  } catch (error) {
    if (!isMergeConflict(error)) {
      await syncStatusComment(
        github,
        owner,
        repo,
        candidate.number,
        `## Queue status\n\nQueue paused: GitHub could not rebase this pull request onto \`${defaultBranch}\`.\n\n\`${safeError(error)}\``,
      );
      throw error;
    }
    const retrySummary = hasFollower
      ? 'The queue will continue with the next queued pull request. This pull request will be retried after its branch is updated.'
      : 'This pull request will be retried after its branch is updated.';
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      `## Queue status\n\nGitHub could not rebase this pull request onto \`${defaultBranch}\` because of merge conflicts. ${retrySummary}\n\n\`${safeError(error)}\``,
    );
    return null;
  }
}

async function mergeQueuedPull({
  github,
  owner,
  repo,
  candidate,
  defaultBranch,
  defaultBranchSHA,
  update,
  queueLabel,
}) {
  try {
    const { data: latestBranch } = await github.rest.repos.getBranch({
      owner,
      repo,
      branch: defaultBranch,
    });
    if (latestBranch.commit.sha !== defaultBranchSHA) {
      throw new Error(
        `Default branch ${defaultBranch} moved from ${shortSHA(defaultBranchSHA)} to ${shortSHA(latestBranch.commit.sha)} while advancing the queue.`,
      );
    }
    const current = await getCurrentPull(github, owner, repo, candidate.number);
    if (
      !hasLabel(current, queueLabel) ||
      current.base?.ref !== defaultBranch ||
      current.head?.sha !== update.headSHA
    ) {
      throw new Error(
        'Pull request changed while completing the queue advance.',
      );
    }
    if (!(await hasCurrentMetadataValidation(github, owner, repo, current))) {
      await syncStatusComment(
        github,
        owner,
        repo,
        candidate.number,
        '## Queue status\n\nQueue paused: waiting for a successful current `pr-metadata` validation.',
      );
      return;
    }
    const state = await pullState(github, owner, repo, candidate.number);
    if (state.headRefOid !== update.headSHA) {
      throw new Error(
        `Pull request head moved from ${shortSHA(update.headSHA)} to ${shortSHA(state.headRefOid)} while advancing the queue.`,
      );
    }
    const { data: repository } = await github.rest.repos.get({ owner, repo });
    requireQueueMergePolicy(repository);
    const result = await mergeIfReady(github, owner, repo, candidate.number, state, {
      expectedBaseRefName: defaultBranch,
      expectedBaseRefOid: defaultBranchSHA,
    });
    const rebaseSummary = update.rebased
      ? `Rebased \`${shortSHA(candidate.head.sha)}\` to \`${shortSHA(update.headSHA)}\` on current \`${defaultBranch}\`.`
      : `Head \`${shortSHA(update.headSHA)}\` already contains current \`${defaultBranch}\`.`;
    const mergeSummary = result === 'merged'
      ? 'All repository requirements were satisfied, so GitHub squash-merged it.'
      : result === 'auto-merge-disabled'
        ? 'An existing automatic merge request was disabled; the queue will retry after repository requirements are clean.'
        : 'Automatic merge remains disabled while GitHub repository requirements are pending.';
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      `## Queue status\n\n${rebaseSummary}\n\n${mergeSummary}`,
    );
  } catch (error) {
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      `## Queue status\n\nQueue paused while completing a direct squash merge.\n\n\`${safeError(error)}\``,
    );
    throw error;
  }
}

async function advanceQueuedPull({
  github,
  owner,
  repo,
  candidate,
  defaultBranch,
  defaultBranchSHA,
  canUpdateBranch,
  isFirst,
  hasFollower,
  queueAppSlug,
  queueLabel,
}) {
  const current = await getCurrentPull(github, owner, repo, candidate.number);
  if (!hasLabel(current, queueLabel) || current.base?.ref !== defaultBranch) {
    await disableManagedAutoMerge(github, owner, repo, candidate.number, queueAppSlug);
    return false;
  }
  if (isFirst) {
    await disableAutoMerge(github, owner, repo, candidate.number);
  }
  if (current.head?.sha !== candidate.head.sha) {
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      '## Queue status\n\nQueue paused: this pull request changed while the queue was advancing. It will be retried after current metadata validation succeeds.',
    );
    return false;
  }
  if (!(await hasCurrentMetadataValidation(github, owner, repo, current))) {
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      '## Queue status\n\nQueue paused: waiting for a successful current `pr-metadata` validation.',
    );
    return false;
  }
  if (current.draft) {
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      '## Queue status\n\nQueue paused: the oldest eligible queued pull request is still a draft.',
    );
    return false;
  }
  const update = await rebaseQueuedPull({
    github,
    owner,
    repo,
    candidate,
    defaultBranch,
    defaultBranchSHA,
    canUpdateBranch,
    hasFollower,
  });
  if (!update) {
    return true;
  }
  if (update.rebased) {
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      '## Queue status\n\nRebased branch; the queue will resume after a successful current `pr-metadata` validation.',
    );
    return false;
  }
  if (update.needsManualRebase) {
    await syncStatusComment(
      github,
      owner,
      repo,
      candidate.number,
      `## Queue status\n\nQueue paused: this fork branch does not contain current \`${defaultBranch}\`, and the repository-scoped queue App cannot update it. Rebase the fork branch manually; the queue will retry after the push.`,
    );
    return false;
  }
  await mergeQueuedPull({
    github,
    owner,
    repo,
    candidate,
    defaultBranch,
    defaultBranchSHA,
    update,
    queueLabel,
  });
  return false;
}

async function runController({
  github,
  context,
  core,
  queueAppSlug = process.env.QUEUE_APP_SLUG,
}) {
  const queueLabel = process.env.QUEUE_LABEL || DEFAULT_QUEUE_LABEL;
  const { owner, repo } = context.repo;
  const { data: repository } = await github.rest.repos.get({ owner, repo });
  const defaultBranch = repository.default_branch;
  await requireStrictBranchRequirements(github, owner, repo, defaultBranch);
  await ensureQueueLabel(github, owner, repo, queueLabel);
  const eventPull = context.payload.pull_request;
  await reconcileEventPull({
    github,
    context,
    owner,
    repo,
    queueLabel,
    defaultBranch,
    eventPull,
    queueAppSlug,
  });

  const openPulls = await github.paginate(github.rest.pulls.list, {
    owner,
    repo,
    state: 'open',
    sort: 'created',
    direction: 'asc',
    per_page: 100,
  });
  await reconcileManagedOpenPulls({
    github,
    owner,
    repo,
    queueLabel,
    defaultBranch,
    pulls: openPulls,
    queueAppSlug,
  });
  const queued = sortQueuedPulls(
    openPulls.filter(
      (pull) => hasLabel(pull, queueLabel) && pull.base?.ref === defaultBranch,
    ),
  );
  if (queued.length === 0) {
    core.notice(`No open ${defaultBranch} pull requests carry the ${queueLabel} label.`);
    return;
  }

  const leader = queued[0];
  const eventQueueEntry = eventPull && queued.find((pull) => pull.number === eventPull.number);
  for (const follower of queued.slice(1)) {
    await disableAutoMerge(github, owner, repo, follower.number);
    await syncStatusComment(
      github,
      owner,
      repo,
      follower.number,
      `## Queue status\n\nQueued behind #${leader.number}. Pull requests advance in ascending number order.`,
      {
        createIfMissing:
          eventQueueEntry?.number === follower.number && context.payload.action === 'labeled',
      },
    );
  }
  const { data: branch } = await github.rest.repos.getBranch({
    owner,
    repo,
    branch: defaultBranch,
  });
  for (const [index, candidate] of queued.entries()) {
    const shouldAdvance = await advanceQueuedPull({
      github,
      owner,
      repo,
      candidate,
      defaultBranch,
      defaultBranchSHA: branch.commit.sha,
      canUpdateBranch: candidate.head.repo?.full_name === repository.full_name,
      isFirst: index === 0,
      hasFollower: index + 1 < queued.length,
      queueAppSlug,
      queueLabel,
    });
    if (!shouldAdvance) {
      return;
    }
  }

  core.notice('Every queued pull request is waiting for a branch update after a rebase conflict.');
}

module.exports = runController;
module.exports.testables = {
  hasLabel,
  isBranchCurrent,
  isMergeConflict,
  hasStrictRequiredStatusChecks,
  mergePolicyViolations,
  labelName,
  metadataCheckExternalID,
  safeError,
  shortSHA,
  sortQueuedPulls,
};
