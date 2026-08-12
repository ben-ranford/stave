# Replay Divergence

- Owner: Session owner
- Proof command: `go test ./replay ./session ./semantic`

## Trigger

Use this runbook when replay does not reproduce the expected target, snapshot hash, or action outcome for the same versioned inputs.

## Immediate actions

1. Freeze the failing snapshot, capability manifest, theme identity, event transcript, and recorded effect outcomes.
2. Record the first event sequence where the divergence appears.
3. Block rollout if the divergence affects a released contract.

## Diagnose

- Check Node ID algorithm version and width-policy version.
- Check whether the failure is stale-target handling, effect ordering, mutation after publish, or environment leakage.
- Compare expected and actual semantic hashes before inspecting rendered output.

## Recovery requirement

- Replay must stop at the first mismatch and emit a structured diff.
- Do not remap targets heuristically.
- If the cause is a compatibility break, publish a migration note or version bump before resuming rollout.

## Exit criteria

- Divergence is reduced to one reproducible fixture.
- A deterministic fix or versioned incompatibility decision is recorded.
- Replay proof command is green.

## Related

- [Compatibility, Versioning, and Schema Policy](../compatibility.md)
- [Performance Budgets](../performance.md)
