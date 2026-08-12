# Ruleset Lockout

- Owner: Release owner
- Proof command: `make workflow-validate`

## Trigger

Use this runbook when repository rulesets or required checks block merge, release tagging, or rollback without a legitimate product-quality failure.

## Immediate actions

1. Record the exact blocked operation, ruleset name, branch or tag target, and missing status context.
2. Confirm whether the failure is expected and protecting a real unmet gate.
3. Re-read the current workflow names and completed status contexts before changing any remote setting.

## Diagnose

- Determine whether the block comes from renamed workflow jobs, missing required contexts, stale branch patterns, or an incomplete rollout of governance changes.
- Verify that local and CI commands still match the documented gate names.
- Confirm whether rollback or hotfix paths are also blocked.

## Recovery requirement

- Prefer correcting the workflow or ruleset mapping over bypassing checks.
- Do not guess status context names.
- Any temporary bypass must be explicitly approved outside this document and removed immediately after recovery.

## Exit criteria

- Required checks and ruleset configuration align again.
- Release and rollback paths are both exercisable.
- Governance proof command is green and remote settings have been re-read.

## Related

- [Release, Rollout, and Rollback Policy](../release.md)
- [Compatibility, Versioning, and Schema Policy](../compatibility.md)
