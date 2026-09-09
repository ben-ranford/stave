'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const { metadataCheckExternalID } = require('./queue_me_controller').testables;
const workflow = fs.readFileSync(path.join(__dirname, '../.github/workflows/pr-metadata.yml'), 'utf8');
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;

function stepScript(name) {
  const step = workflow.split(`      - name: ${name}\n`)[1]?.split('\n      - name:')[0];
  assert.ok(step, `missing workflow step ${name}`);
  const script = step.split('          script: |\n')[1];
  assert.ok(script, `missing inline script for ${name}`);
  return script.split('\n  attest:\n')[0].split('\n').map((line) => line.replace(/^            /, '')).join('\n');
}

function harness({
  eventName = 'pull_request_target',
  githubRef = 'refs/heads/main',
  releasePleaseAuthorLogin = '',
  renderedBody = '<p>Current body</p>',
  renderError,
} = {}) {
  let current = { number: 8, title: 'fix: x', body: 'Current body', labels: [{ name: 'bug' }],
    head: { sha: 'abc', ref: 'bug/example', repo: { full_name: 'owner/repo' } },
    base: { sha: 'base', ref: 'main' }, user: { login: 'owner' } };
  const env = {
    RUNNER_TEMP: '/tmp',
    VALIDATION_OUTCOME: 'success',
    GITHUB_REF: githubRef,
    RELEASE_PLEASE_AUTHOR_LOGIN: releasePleaseAuthorLogin,
  };
  const created = [], updated = [], failures = [], files = new Map(), renders = [], outputs = {};
  const context = { eventName, serverUrl: 'https://github.com', runId: 123, repo: { owner: 'owner', repo: 'repo' },
    payload: eventName === 'workflow_dispatch'
      ? { inputs: { 'pr-number': String(current.number) } }
      : { pull_request: { ...current, body: 'Stale event body' } } };
  const github = { rest: {
    pulls: { get: async () => ({ data: structuredClone(current) }) },
    repos: { get: async () => ({ data: { default_branch: 'main' } }) },
    markdown: {
      render: async (input) => {
        renders.push(input);
        if (renderError) throw renderError;
        return { data: renderedBody };
      },
    },
    checks: {
      create: async (check) => { created.push(check); return { data: { id: 123 } }; },
      update: async (check) => { updated.push(check); },
    },
  } };
  const core = {
    exportVariable: (key, value) => { env[key] = value; },
    setFailed: (message) => failures.push(message),
    setOutput: (key, value) => { outputs[key] = value; },
  };
  const load = (name) => name === 'node:fs' ? { writeFileSync: (file, text) => files.set(file, text) } : require(name);
  const run = (name) => new AsyncFunction('require', 'github', 'context', 'core', 'process', stepScript(name))(
    load, github, context, core, { env });
  return { run, env, created, updated, failures, files, renders, outputs, current, edit: (changes) => { current = { ...current, ...changes }; } };
}

test('workflow validates current API metadata and produces the queue fingerprint', async () => {
  const h = harness();
  await h.run('Read pull request metadata');
  assert.equal(h.files.get('/tmp/pr-body.md'), '<p>Current body</p>');
  assert.deepEqual(h.renders, [{ text: 'Current body', mode: 'gfm', context: 'owner/repo' }]);
  assert.equal(h.created[0].external_id, metadataCheckExternalID(h.current));
  assert.equal(h.created[0].details_url, 'https://github.com/owner/repo/actions/runs/123');
  assert.equal(h.outputs.external_id, metadataCheckExternalID(h.current));
  await h.run('Publish PR metadata result');
  assert.equal(h.updated[0].conclusion, 'success');
  assert.deepEqual(h.failures, []);
});

test('a successful validator cannot publish success after metadata changes', async () => {
  const h = harness();
  await h.run('Read pull request metadata');
  h.edit({ body: '' });
  await h.run('Publish PR metadata result');
  assert.equal(h.updated[0].conclusion, 'failure');
  assert.match(h.failures[0], /superseded/);
});

test('a successful validator cannot publish success after release author configuration changes', async () => {
  const h = harness({ releasePleaseAuthorLogin: 'release-bot-old' });
  await h.run('Read pull request metadata');
  h.env.RELEASE_PLEASE_AUTHOR_LOGIN = 'release-bot-new';
  await h.run('Publish PR metadata result');
  assert.equal(h.updated[0].conclusion, 'failure');
  assert.match(h.failures[0], /superseded/);
});

test('Markdown rendering failure keeps the pending check and publisher fails it', async () => {
  const h = harness({ renderError: new Error('renderer unavailable') });
  await assert.rejects(h.run('Read pull request metadata'), /renderer unavailable/);
  assert.equal(h.created.length, 1);
  assert.equal(h.created[0].status, 'in_progress');
  assert.deepEqual(h.updated, []);

  h.env.VALIDATION_OUTCOME = 'skipped';
  await h.run('Publish PR metadata result');
  assert.equal(h.updated[0].conclusion, 'failure');
  assert.doesNotMatch(h.updated[0].conclusion, /success/);
});

test('failed or cancelled validation cannot publish success', async () => {
  for (const outcome of ['failure', 'cancelled', 'skipped']) {
    const h = harness();
    await h.run('Read pull request metadata');
    h.env.VALIDATION_OUTCOME = outcome;
    await h.run('Publish PR metadata result');
    assert.equal(h.updated[0].conclusion, 'failure');
  }
});

test('manual validation uses a trusted default-branch workflow and current PR data', async () => {
  const h = harness({ eventName: 'workflow_dispatch' });
  await h.run('Read pull request metadata');
  assert.equal(h.created[0].head_sha, h.current.head.sha);
  assert.equal(h.files.get('/tmp/pr-body.md'), '<p>Current body</p>');
});

test('manual validation rejects an untrusted workflow ref before creating a check', async () => {
  const h = harness({ eventName: 'workflow_dispatch', githubRef: 'refs/heads/untrusted' });
  await assert.rejects(h.run('Read pull request metadata'), /trusted default branch workflow ref/);
  assert.deepEqual(h.created, []);
});
