# Changelog

All notable changes to this repository are tracked here. The format follows
Keep a Changelog and Stave uses Semantic Versioning.

## [Unreleased]

## [1.0.0-rc.2] - 2026-09-10

### Security

- Bind agent confirmation grants to the authorizing policy ID and epoch, and
  reject grants after either changes. Direct registry users must copy the
  authorized call's policy binding into manually issued grants.
- Bound retained confirmation state, expire old grants, and reject duplicate
  and replayed tokens. Capacity exhaustion returns a typed resource-limit error.
- Scan the root module and every optional adapter module for reachable Go
  vulnerabilities in CI.

### Fixed

- Limit the protocol schema to the implemented `full` and `patch` snapshot
  modes. Previously advertised `hash`, `action`, and `diagnostic` modes were
  never accepted by the server; working clients require no wire migration.
- Complete the MIT license and add a tested public-API quick start.

## [1.0.0-rc.1] - 2026-08-13

### Target

- `v1.0.0-rc.1`; this root-module candidate freezes the public v1 contracts
  while Lopper proving-client evidence remains a GA promotion gate.

### Added

- Application-neutral semantic trees with stable identities, immutable
  snapshots, typed actions, versioned schemas, deterministic replay, and
  explicit effect boundaries.
- Human and agent runtimes sharing one action authority, with JSON-RPC 2.0 over
  JSONL stdio, capability negotiation, bounded queues, cancellation, safe
  diagnostics, and secret exclusion.
- Adaptive layout, surface diffing, headless and terminal rendering, and full
  truecolor, ANSI-256, ANSI-16, monochrome, and no-colour support.
- Semantic themes, density and reduced-motion policies, pluggable glyph/assets,
  and independent Lopper and Atlas brand fixtures.
- Foundational and P1 primitives including tables, forms, tabs, pagination,
  command palette, charts, inspector/master-detail, dialogs, confirmations, and
  progress/state surfaces.
- Optional SSH, Bubble Tea, and Lip Gloss adapter modules with shared fixture
  conformance.
- Deterministic performance fixtures, percentile reports, ADRs, operational
  runbooks, release automation, repository hooks, schemas, and traceability.
- Lopper proving-client migration contracts and local worktree evidence for the
  explicit `stave-tui-preview` feature flag, parity, snapshots,
  consequential-action coverage, and legacy rollback. Published immutable
  Lopper evidence remains a GA promotion requirement.

### Release scope

- Publishes the root `github.com/ben-ranford/stave` module and source evidence.
- Keeps the Bubble Tea, Lip Gloss, and SSH nested modules internal until their
  independent module versions and tags consume a published root release.

### Changed

- Replaced the initial root-package spike with the production `Program` facade
  over the renderer-neutral core packages.
- Set the release candidate and dry-run base version to `v1.0.0`.
- Reduced 2,000-node layout allocation churn with a bounded reusable plan-hash
  buffer and a printable-ASCII measurement fast path, preserving the 8 ms p95
  gate on slower release runners without changing layout semantics.

### Security

- Added final-sink control-sequence sanitization, opaque secret handles,
  redacted events/checkpoints/diagnostics, protocol resource limits, and
  single-use confirmation enforcement.
