# v1 Compatibility, Versioning, and Schema Policy

## Release target

Stave targets `v1.0.0`. The contracts below are frozen by the
`v1.0.0-rc.1` root-module candidate but are not a GA compatibility promise
until the stable `v1.0.0` tag is published after the GA gates pass.

Compatibility is defined at the application-neutral Stave boundaries. Lopper
is the first proving client, while Atlas supplies a contrasting client and
brand shape; compatibility claims must remain valid for both and may not embed
Lopper domain concepts into core APIs.

## Independently versioned surfaces

| Surface | v1 policy |
|---|---|
| Root Go module | SemVer beginning at `v1.0.0` |
| Semantic snapshot schema | `stave.semantic/v1` |
| Action definition schema | `stave.action/v1` |
| Agent protocol | JSON-RPC `2.0` with Stave protocol `1.0` |
| Configuration schema | `stave.config/v1` |
| Node ID algorithm | `stave-node-id-v1` |
| Unicode width policy | Versioned algorithm identifier in snapshot envelopes |
| Theme/token packs | SemVer and canonical content hash |
| Optional adapter modules | Independent SemVer; third-party types never enter root APIs |

## v1 guarantees

- Public root-module signatures contain only Go standard-library and Stave
  types.
- Semantic trees, action definitions, configuration, and protocol payloads
  retain deterministic canonical encodings.
- Target references carry node ID, generation, and observed revision where
  required by the action boundary.
- Truecolor, ANSI-256, ANSI-16, monochrome, and no-colour profiles preserve
  meaning; colour is never the only state signal.
- Human and agent paths use the same semantic/action authority.
- Application themes and assets remain application-owned and interchangeable
  without changing primitives.

## Breaking changes

The following require a breaking version step for the affected surface:

- Removing or renaming a public Go API.
- Changing Node ID derivation or canonical encoding.
- Changing the meaning of an existing action argument, result, safety class,
  or confirmation policy.
- Removing or renaming semantic roles or required theme token roles.
- Changing an existing protocol field's meaning.
- Reassigning an existing token's semantic meaning.

## Additive changes

The following are additive when existing meaning remains intact:

- New optional capabilities or semantic roles.
- New versioned action IDs.
- New optional theme token roles or assets.
- New independently versioned adapters.
- New protocol notifications that v1 clients may ignore.

## Schema and generated-artifact policy

1. Semantic, action, protocol, and config schemas are checked-in source
   artifacts.
2. Runtime reflection may assist development, but compatibility cannot depend
   on reflection-derived schemas.
3. Schema and generated public-API diffs are first-class review artifacts.
4. Canonical encodings must remain stable and hashable.
5. Release notes classify every contract change as breaking, additive, or
   internal-only.

## Deprecation policy

- Deprecate before removal whenever a compatibility alias is practical.
- Keep action and protocol aliases explicit and versioned; never silently
  reinterpret payloads.
- Adapter deprecations do not force a root-module major version unless the
  Stave-facing adapter contract changes.
- Publish migration notes for every release that changes a versioned contract.

## Compatibility gates

| Requirement | Proof command |
|---|---|
| Root public API and dependency boundaries contain no optional framework or Lopper type | `make api-boundary` |
| Checked-in schemas and render traces are fresh | `make schema-freshness` |
| Minimum and current supported Go versions compile and test | `.github/workflows/ci.yml` portability and Go jobs |
| Optional adapters pass in their own modules | `make adapters` |
| Multi-app brand separation and capability degradation remain deterministic | `make atlas-check`, `go run ./cmd/lopper`, and generated render/matrix traces |
| New client integrations use the shared semantic/action/keymap conformance boundary | `go test ./conformance` and application-owned `conformance.CheckClient` suites |
| Lopper proving-client parity and rollback remain intact | Lopper `internal/ui` and `internal/cli` suites |

## Related documents

- [Architecture Overview](architecture.md)
- [Client Adoption Guide](client-adoption.md)
- [Dependency Policy](dependencies.md)
- [Release, Rollout, and Rollback Policy](release.md)
- [ADR-003](adr/ADR-003.md)
- [ADR-011](adr/ADR-011.md)
- [ADR-015](adr/ADR-015.md)
