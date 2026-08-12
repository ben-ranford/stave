# Broken Terminal and Restoration

- Owner: Runtime owner
- Proof command: `go test ./runtime/human ./surface ./render/...`

## Trigger

Use this runbook when a Stave session leaves the terminal in a corrupted state: bad echo mode, hidden cursor, alternate screen leak, broken line discipline, or unreadable control output.

## Immediate actions

1. Stop the affected Stave process.
2. Capture the exact command, terminal type, and whether the failure followed panic, cancellation, resize storm, or adapter use.
3. Restore the terminal using the repository-approved restore procedure once it exists.

## Diagnose

- Determine whether the fault came from renderer output, panic recovery, cancellation, or adapter drift.
- Confirm whether control bytes originated from renderer code or leaked application content.
- Check whether the incident also affected machine stdout separation.

## Recovery requirement

- Terminal must return to a readable, input-safe state without requiring a full shell restart when the platform supports recovery.
- If recovery is incomplete, the process must exit clearly and document the remaining manual step.

## Exit criteria

- Root cause is classified as renderer, runtime, or adapter failure.
- A reproducible fixture or regression test exists or is queued.
- Terminal restore proof command is green before release resumes.

## Related

- [Security and Threat Model](../security.md)
- [Release, Rollout, and Rollback Policy](../release.md)
