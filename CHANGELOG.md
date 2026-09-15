# Changelog

All notable changes to this repository are tracked here. The format follows
Keep a Changelog and Stave uses Semantic Versioning.

## [1.0.0-rc.3](https://github.com/ben-ranford/stave/compare/v1.0.0-rc.2...v1.0.0-rc.3) (2026-09-15)


### Features

* **agent:** project validated config limits ([#74](https://github.com/ben-ranford/stave/issues/74)) ([c755dd0](https://github.com/ben-ranford/stave/commit/c755dd08a005f6d7ea0a637e59d598366a0eb88d))
* **focus:** add modal focus lifecycle helpers ([#83](https://github.com/ben-ranford/stave/issues/83)) ([9d4c162](https://github.com/ben-ranford/stave/commit/9d4c1629399bbe7e5a7e9d530465b31981af6b6e))
* **release:** verify anonymous release consumers ([#68](https://github.com/ben-ranford/stave/issues/68)) ([d07d074](https://github.com/ben-ranford/stave/commit/d07d074430d59e9f81aedf347f36cf0d33781998))
* **render:** select output products ([#72](https://github.com/ben-ranford/stave/issues/72)) ([e205262](https://github.com/ben-ranford/stave/commit/e20526212fdc8bec3d5aba596cbf008ecccd5305))
* **semantic:** add bounded tree queries ([#82](https://github.com/ben-ranford/stave/issues/82)) ([eb09cfe](https://github.com/ben-ranford/stave/commit/eb09cfe043a2c8f5ad9875d5e2db26e915c76337))
* **semantic:** add negotiated patch detail ([#81](https://github.com/ben-ranford/stave/issues/81)) ([2d86b5c](https://github.com/ben-ranford/stave/commit/2d86b5ca3a0bab3d0677fa4820b690391d06c1c7))
* **session:** add publication waits ([#78](https://github.com/ben-ranford/stave/issues/78)) ([e807bd0](https://github.com/ben-ranford/stave/commit/e807bd06ec93e1486a4534c8c01474da8e066b57))
* **session:** decouple effect admission ([#73](https://github.com/ben-ranford/stave/issues/73)) ([d906271](https://github.com/ben-ranford/stave/commit/d906271a5a6c9fc169d8c494f6286b85cc522a51))


### Bug Fixes

* **docs:** correct semantic snapshot version ([#69](https://github.com/ben-ranford/stave/issues/69)) ([4dde3e6](https://github.com/ben-ranford/stave/commit/4dde3e61756a545ab477b584b310aa4291b2edeb))
* **human:** reject cancelled line driver open ([#71](https://github.com/ben-ranford/stave/issues/71)) ([e255f7e](https://github.com/ben-ranford/stave/commit/e255f7e794341f74da2e8eca177d400f8a3604f8))
* **rigor:** fail fuzz discovery errors ([#66](https://github.com/ben-ranford/stave/issues/66)) ([ab9370f](https://github.com/ben-ranford/stave/commit/ab9370f5109bbc1f0af5397dd8442e5ddaa90259))
* **rigor:** inventory exported value semantics ([#67](https://github.com/ben-ranford/stave/issues/67)) ([0298e57](https://github.com/ben-ranford/stave/commit/0298e57ede65a10959e00e86ddd7b9800e7f1dc7))
* **surface:** reject truncated wide cells ([#70](https://github.com/ben-ranford/stave/issues/70)) ([f51a24b](https://github.com/ben-ranford/stave/commit/f51a24baf7ebbd74d07f0dc9c99de7b2c7dd9c9f))

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
- Bound the terminal cleanup context after a capability protocol mismatch.

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
