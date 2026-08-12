# Lopper Migration and Rollback Plan

## Boundary

The reusable Stave core must stay application-neutral. Lopper owns:

- Analysis and report domain types
- `ActionRunner` and concrete codemod or baseline effects
- Command vocabulary and compatibility aliases
- Report-format compatibility
- Lopper theme values and assets
- Authorization, dirty-worktree policy, confirmation wording, filesystem paths, and baseline resolution

Stave owns:

- Semantic tree construction
- Stable dependency and control IDs
- Pure filter, sort, page, and selection reducer state
- Focus and master-detail behavior
- Capability-aware rendering
- Headless semantic snapshots
- Typed agent actions and replay

## Recommended identity contract

Use the following dependency-row key shape:

```text
AppNamespace = "lopper"
View         = "summary"
Kind         = "dependency"
Entity       = canonical language + NUL + normalized dependency identity
Slot         = "row"
```

Child control slots should remain explicit, for example `open`, `apply-codemod`, `copy-command`, and `evidence`.

## Migration stages

1. Freeze current live, snapshot, and command behavior with regression tests.
2. Add a Lopper-owned adapter package under Lopper, not under Stave core.
3. Map summary and detail view structs to a Stave application model.
4. Register Lopper actions through the existing `ActionRunner`.
5. Add semantic snapshots while preserving the legacy formatter path.
6. Run legacy and Stave reducers in shadow tests for filter, sort, page, and selection parity.
7. Expose Stave snapshot mode behind an explicit feature flag.
8. Expose interactive Stave mode as opt-in.
9. Verify non-TTY, TTY refresh, sanitization, confirmation, baseline, and codemod parity.
10. Make Stave default only after acceptance gates pass.
11. Keep rollback capability for at least one release after default-on.

## Rollback triggers

Rollback must be available if any of the following regress:

- Command grammar
- Confirmation semantics for codemod or baseline actions
- Snapshot or non-interactive output
- TTY refresh behavior
- Sanitization and terminal safety
- Filter, sort, page, or selection determinism

## Rollback method

- Disable the Stave-backed Lopper path via explicit feature or adapter inversion.
- Preserve the Stave semantic contract; do not hot-edit schema meaning during rollback.
- Retain shadow and parity fixtures until the rollback window has passed.

## Migration controls

The current Stave repository includes Atlas and Lopper command render fixtures in
`scripts/rigor/generated/atlas.render.txt` and
`scripts/rigor/generated/lopper.render.txt`. They are evidence for core brand
independence only. The actual Lopper parity proofs stay in an external
worktree and are referenced through portable `external://` citations until the
worktree is published; application parity remains a Lopper-worktree release
gate.

| Requirement | Owner | Proof command |
|---|---|---|
| Core remains free of Lopper imports | Architecture owner | `go run ./scripts/rigor/cmd/rigor boundary-check` |
| Lopper parity fixtures cover command grammar and state transitions | Lopper owner | External portable Lopper worktree parity suite (release blocker until published) |
| Non-TTY and snapshot behavior remain compatible | Lopper owner | External portable Lopper feature-flag smoke (release blocker until published) |
| Rollback switch remains available for one release after default-on | Release owner | Lopper flag/rollback test (required before default-on) |

## Related documents

- [Architecture Overview](architecture.md)
- [Release, Rollout, and Rollback Policy](release.md)
- [Adapter Incompatibility Runbook](runbooks/adapter-incompatibility.md)
- [ADR-014](adr/ADR-014.md)
