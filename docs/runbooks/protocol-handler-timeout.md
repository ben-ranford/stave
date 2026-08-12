# Protocol or Handler Timeout

- Owner: Runtime owner
- Proof command: `go test ./runtime/agent ./protocol ./effect`

## Trigger

Use this runbook when a protocol call or action handler exceeds its timeout class, starves the queue, or stops making bounded progress.

## Immediate actions

1. Capture the action ID, target reference, timeout class, capability manifest, and queue depth.
2. Determine whether the timeout is in validation, reducer, effect execution, or protocol delivery.
3. If user-visible behavior is degraded, stop further rollout of the affected path.

## Diagnose

- Check whether the handler violated the pure reducer or effect boundary.
- Check whether the effect executor is hung, uncancellable, or over parallelism limits.
- Check whether progress reporting is missing for long-running but expected actions.

## Recovery requirement

- Timeouts must return typed errors, not hang indefinitely.
- Cancellable work must accept cancellation.
- Queue saturation must coalesce safe events or reject remote calls explicitly rather than silently dropping consequential work.

## Exit criteria

- Timeout root cause is classified as validation, effect, queue, or protocol issue.
- Progress, cancellation, or tighter resource bounds are in place.
- Timeout proof command is green.

## Related

- [Performance Budgets](../performance.md)
- [Security and Threat Model](../security.md)
