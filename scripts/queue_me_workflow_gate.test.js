'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const workflow = fs.readFileSync(path.join(__dirname, '../.github/workflows/queue-me.yml'), 'utf8');
const condition = workflow.match(/^    if: >-\n([\s\S]*?)^    runs-on:/m)?.[1];
assert.ok(condition, 'queue advance job condition is missing');

function evaluate(eventName, ref, event = {}) {
  const github = {
    event_name: eventName,
    ref,
    repository: 'octo/stave',
    workflow: 'queue me',
    event: {
      repository: { default_branch: 'main' },
      ...event,
    },
  };
  const expression = condition
    .replaceAll("format('refs/heads/{0}', github.event.repository.default_branch)", "`refs/heads/${github.event.repository.default_branch}`")
    .replaceAll('github.event.pull_request.labels.*.name', 'github.event.pull_request.labels.map((label) => label.name)');
  return Function('github', 'contains', `return Boolean(${expression});`)(github, (values, value) => values.includes(value));
}

for (const [eventName, event] of [
  ['workflow_dispatch', {}],
  ['schedule', {}],
  ['push', {}],
  ['pull_request_target', { action: 'labeled', label: { name: 'queue-me' }, pull_request: { labels: [] } }],
  ['workflow_run', { workflow_run: { conclusion: 'success', event: 'pull_request', head_repository: { full_name: 'octo/stave' }, head_branch: 'feature', name: 'ci' } }],
]) {
  test(`advance queue-me permits trusted ${eventName} on the default branch`, () => {
    assert.equal(evaluate(eventName, 'refs/heads/main', event), true);
  });
  test(`advance queue-me rejects ${eventName} from a non-default workflow ref`, () => {
    assert.equal(evaluate(eventName, 'refs/heads/feature/untrusted', event), false);
  });
}
