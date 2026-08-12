# Adapter Incompatibility

- Owner: Adapter owner
- Proof command: `make adapters`

## Trigger

Use this runbook when an optional adapter stops compiling, changes behavior incompatibly, or leaks its dependency contract into core behavior.

## Immediate actions

1. Identify the failing adapter module and the upstream version or API drift.
2. Confirm that the root core module still compiles and tests without the adapter.
3. Disable or pin the adapter rollout path if user-facing behavior is at risk.

## Diagnose

- Determine whether the issue is compile-time drift, semantic drift, capability drift, or runtime restore failure.
- Confirm whether any public core signature or schema was changed to accommodate the adapter.
- Re-run adapter-specific conformance before considering promotion.

## Recovery requirement

- Fix the adapter in its own module where possible.
- Do not patch core contracts to absorb upstream churn unless a new ADR authorizes it.
- Keep the adapter opt-in or rolled back until conformance passes again.

## Exit criteria

- Core import boundaries remain intact.
- Adapter module tests and conformance pass independently.
- Rollout state is explicit: fixed, pinned, or disabled.

## Related

- [Architecture Overview](../architecture.md)
- [Dependency Policy](../dependencies.md)
- [Lopper Migration and Rollback Plan](../lopper-migration.md)
