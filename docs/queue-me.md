# Pull Request Queue

Stave uses the repository-hosted `queue-me` workflow to serialize pull
requests targeting `main` without relying on GitHub's organization-level merge
queue feature.

Apply the `queue-me` label to an open, non-draft pull request. The controller
processes queued pull requests in ascending PR-number order, rebases only the
current leader onto the exact `main` tip, and directly squash-merges only when
GitHub reports every repository requirement clean. The repository ruleset
remains authoritative for required checks, reviews, and other merge
requirements; queued pull requests are never left with auto-merge armed.

Removing `queue-me` disables any queue-managed automatic merge for that pull
request and advances the remaining queue. A draft leader, stale fork, or
non-conflict rebase failure
pauses the queue with one managed status comment. When GitHub reports a rebase
conflict, the controller leaves that PR queued, records the conflict, and
continues evaluating the next queued PR; the blocked PR is retried after its
branch next updates.

Queue evaluation also follows successful `ci` and `pr metadata` workflow
completions. It accepts only pull-request runs from this repository, with a
non-empty branch, and ignores the queue workflow itself. Failed checks and
fork-originated runs cannot advance the queue.

Before a direct merge, the controller requires a successful trusted
`pr-metadata` check whose recorded fingerprint matches the pull request's
current title, body, labels, and head SHA. Editing those fields pauses the
queue until current metadata validation completes; a superseded validation
cannot advance the pull request.

The workflow runs from `pull_request_target` but never checks out PR code. It
downloads the controller from the exact trusted workflow revision into runner
temporary storage before executing it. The workflow is inert until its
repository configuration is supplied. A trusted five-minute schedule also
rechecks queued pull requests after review or third-party check completion.

## Repository setup

Enable **Allow squash merging** in repository Settings > General. Disable merge
commits and rebase merges, and use the pull request title as the squash commit
title.

Install a GitHub App on this repository with Contents, Issues, Pull requests,
and Workflows write permissions. Set repository variable `QUEUE_APP_CLIENT_ID`
to the App identifier and secret `QUEUE_APP_PRIVATE_KEY` to its private key.
The App must not bypass the repository ruleset.

For Release Please PRs, set `RELEASE_PLEASE_AUTHOR_LOGIN` to the trusted author
login used by `RELEASE_PLEASE_TOKEN`. Only same-repository PRs from that author
on a Release Please branch receive the generated-body exemption; stable and
prerelease titles are supported. Other PRs need at least one label and completed
Summary, Validation, and Release Notes sections.

Validate the queue locally with:

```sh
make queue-me-check
make workflow-validate
```
