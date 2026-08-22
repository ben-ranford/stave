# v1.0.0 Release, Rollout, and Rollback Policy

## Release posture

The target GA release is `v1.0.0`. The first publishable root-module candidate
is `v1.0.0-rc.1`; it uses the candidate contract and remains a prerelease until
every normative traceability entry and GA gate below is green.
`requirements/traceability.json` is authoritative for requirement status.

Stave remains application-neutral. Lopper is the first proving client and owns
its integration, command grammar, domain effects, and rollout flag in the
Lopper repository. Atlas is the contrasting second-client regression rig;
the optional Bubble Tea and Lip Gloss modules also consume one shared
adapter-neutral surface fixture.

## Release stages

| Stage | Meaning |
|---|---|
| `v1.0.0-rc.1` | Immutable root-module candidate; candidate gates and exact-tag CI are required, while external proving-client evidence may remain planned. |
| `Lopper pilot` | Lopper's Stave path is explicitly enabled with `stave-tui-preview`; the legacy path remains the default rollback. |
| `v1.0.0 GA` | All normative traceability entries, fresh rigor gates, performance evidence, release automation, and rollback proof are green. |

## Mandatory release gates

| Gate | Owner | Proof command |
|---|---|---|
| Formatting, lint, vet, tests, race, coverage, fuzz smoke, benchmarks, performance, dependency and licence inventories, and adapter checks | Build owner | `make verify` |
| Vulnerability and workflow validation | Release owner | `make ci` |
| Semantic, action, config, and protocol schemas and command render traces are fresh | Schema owner | `make schema-freshness` |
| Atlas's 63-case client proof matrix, typed actions, replay, and conformance remain deterministic | Client owner | `make atlas-check` and `make schema-freshness` |
| Candidate evidence is internally consistent and no implemented normative entry relies on unpublished proof | Release owner | `make release-contract` |
| Every P0, P1, and acceptance-criterion entry is implemented with published evidence | Release owner | `make release-ga-contract` |
| Optional dependencies remain isolated from the root module | Architecture owner | `make adapters` and `make api-boundary` |
| Terminal restoration, cancellation, and final-sink sanitization pass | Runtime owner | `go test ./runtime/human ./session ./render ./internal/terminal` |
| Secret and confirmation boundaries pass | Security owner | `go test ./secret ./state ./event ./action ./runtime/agent` |
| Every performance target is measured, or a miss/gap has an accepted ADR | Performance owner | `make verify-performance` plus `docs/adr/ADR-017.md` review |
| Lopper parity, snapshot, consequential-action, feature-flag, and rollback tests pass | Lopper owner | `go test ./internal/ui ./internal/cli` in the Lopper proving-client worktree |
| The candidate tag and source metadata are valid without publishing | Release owner | `make release-dry-run` |

## Current evidence

The 2026-08-12 local release audit is green. The 101-sample deterministic
report records every proposed local target, allocation, idle CPU, resize
recovery, terminal restoration, and zero-byte deterministic render variance.
The representative loopback SSH PTY test records a live input-to-output p95 of
`11.312541 ms` over 21 samples against the `75 ms` budget.

`make release-contract` is the pull-request and candidate gate. It permits
explicitly planned proving-client work, but it rejects implemented normative
entries that rely on unpublished evidence. `make release-ga-contract` is the
tag/publication gate and rejects every remaining planned normative entry or
unpublished external proof. Publication also requires reviewed remote CI and
release workflow evidence for the exact commit being tagged.

The tag workflow selects the gate from the tag: SemVer prereleases run the
candidate contract and are marked as GitHub prereleases; a stable tag runs the
GA contract. Manual workflow dispatch is inert unless it targets a `v*` tag.
The root candidate does not publish the independently versioned adapter
modules, whose local development replacements are not valid release metadata.

## Immutable publication sequence

1. On each merge to `main`, Release Please creates or updates its release pull
   request and changelog. Its `RELEASE_PLEASE_TOKEN` must be a narrowly scoped
   GitHub App token or fine-grained PAT so normal pull-request CI runs; the
   default workflow token does not trigger subsequent workflows. The
   self-hosted `stave-arc` runner must support the action's Node 24 runtime.
2. Review and merge the release pull request only after its remote CI is green.
   Release Please prepares the changelog but does not create a tag or GitHub
   release.
3. Rerun `make ci`,
   `make release-contract`, and `make release-dry-run` on the final `main`
   commit because its identity differs from the reviewed branch commit.
4. Create the annotated `v1.0.0-rc.1` tag at that verified `main` commit and
   push only the tag.
5. Verify the tag-triggered release workflow, GitHub prerelease metadata,
   release assets, and a clean private-module consumer download.

Stable `v1.0.0` publication follows the same sequence but additionally requires
`make release-ga-contract`. Nested adapter modules require their own consumable
root dependency, independent module tags, and release verification; the root
candidate does not publish them.

## Rollout policy

1. Publish the application-neutral core and conformance contracts before any
   proving client becomes default-on.
2. Keep Lopper's Stave path opt-in through its v2 pilot; enabling the feature
   must not alter the core semantic contract.
3. Preserve Lopper's commands, snapshots, baseline/codemod authorization,
   non-TTY behavior, terminal sanitization, and legacy rollback path.
4. Require the Atlas proof matrix and shared adapter fixture to stay green so compatibility
   claims never depend on Lopper alone.
5. Keep the legacy Lopper path for at least one release after any default-on
   change.

## Rollback policy

- Disable `stave-tui-preview` or invert the Lopper-owned adapter selection; do
  not mutate Stave schema meaning during rollback.
- Disable a stale optional adapter without changing root-module contracts.
- Preserve parity fixtures and the feature flag throughout the rollback window.
- Treat CI or ruleset lockouts through the operational runbook, never an admin
  merge or ad hoc bypass.

## Operational readiness

| Requirement | Proof command |
|---|---|
| Structured, redacted diagnostics and safe protocol errors | `go test ./session ./runtime/agent ./event` |
| Bounded event/effect/protocol queues | `go test ./runtime/human ./session ./effect ./runtime/agent` |
| Graceful cancellation and terminal restoration | `go test ./runtime/human ./session` |
| Reproducible, redacted checkpoints and replay | `go test ./replay ./session ./state ./secret` |
| Full truecolor, ANSI-256, ANSI-16, monochrome, and no-colour ladder | `go test ./capability ./theme ./render` |

## Related documents

- [Traceability](../requirements/TRACEABILITY.md)
- [Compatibility, Versioning, and Schema Policy](compatibility.md)
- [Performance Budgets](performance.md)
- [Lopper Migration and Rollback Plan](lopper-migration.md)
- [Operational Runbooks](runbooks/README.md)
