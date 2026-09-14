# Compatibility and versioning

Compatibility is defined at Stave's public application boundaries. Applications
remain free to choose their own domain model, language, theme, and host
environment.

## Independently versioned surfaces

| Surface | Policy |
|---|---|
| Root Go module | Semantic versioning |
| Semantic snapshot schema | `stave.semantic/v1` |
| Action definition schema | `stave.action/v1` |
| Agent protocol | JSON-RPC `2.0` with Stave protocol `1.0` |
| Configuration schema | `stave.config/v1` |
| Node ID algorithm | `stave-node-id-v1` |
| Unicode width policy | Versioned algorithm identifier in snapshot envelopes |
| Theme/token packs | Semantic versioning and canonical content hash |
| Optional adapter modules | Independently versioned; third-party types stay out of root APIs |

## Compatibility guarantees

- Public root-module signatures contain only Go standard-library and Stave
  types.
- Semantic trees, action definitions, configuration, and protocol payloads
  retain deterministic canonical encodings.
- Target references carry node ID, generation, and observed revision where
  required by the action boundary.
- Truecolor, ANSI-256, ANSI-16, monochrome, and no-colour profiles preserve
  meaning; colour is never the only state signal.
- Human and agent paths use the same semantic and action authority.
- Application themes and assets remain interchangeable without changing
  primitive meaning.

## Breaking changes

The following require a breaking version step for the affected surface:

- Removing or renaming a public Go API.
- Changing node ID derivation or canonical encoding.
- Changing the meaning of an existing action argument, result, safety class,
  or confirmation policy.
- Removing or renaming semantic roles or required theme token roles.
- Changing the meaning of an existing protocol field.
- Reassigning a token's semantic meaning.

## Additive changes

The following are additive when existing meaning remains intact:

- New optional capabilities or semantic roles.
- New versioned action IDs.
- New optional theme token roles or assets.
- New independently versioned adapters.
- New protocol notifications that v1 clients may ignore.

## Deprecation policy

- Deprecate before removal whenever a compatibility alias is practical.
- Keep action and protocol aliases explicit and versioned; never silently
  reinterpret payloads.
- Optional adapters have their own versions; a root-module major version is
  required only when the Stave-facing adapter contract changes.

## Minor-release baseline gate

`make release-baseline` runs the focused consumer fixture matrix and compares
the native build with its inherited cgo setting. It then sets `CGO_ENABLED=0`
for each supported public build target (`linux/amd64`, `darwin/amd64`,
`darwin/arm64`, and `windows/amd64`). For each comparison, it regenerates the
candidate public API inventory from source and compares it with the newest
annotated stable `v1.x.y` tag on a strict
ancestor of the candidate. Tags on the candidate itself or unrelated/future
commits cannot serve as its baseline. It
records the baseline tag, immutable commit, and both Go floors. A regenerated
candidate inventory cannot waive a removed declaration, changed signature,
interface method addition, or exported variable type change.

Adding an exported function or a field to a defined struct while retaining
existing keyed fields and struct comparability is accepted. Field additions to
an alias of an anonymous struct are rejected because they change type identity.
Adding a field can still break consumers using unkeyed composite literals, so
those consumers should use keyed literals
and maintainers must call out that caveat during compatibility review. The
baseline rejects every field addition to a struct that already embeds a field,
as well as embedded-field additions to a plain struct, because either can
change promoted selectors. The source inventory also fails closed when selected
public packages or their root-local source dependencies use cgo; cgo-aware
inventory support is tracked separately in issue #109.

The gate is a bounded declaration and consumer regression check, not a complete
analysis of Go source compatibility. Its current inventory does not follow
public aliases into non-public root-local packages ([#112](https://github.com/ben-ranford/stave/issues/112)),
collect sealed-method requirements from hidden interfaces or hidden concrete
result types exposed through public signatures ([#113](https://github.com/ben-ranford/stave/issues/113)), detect
promoted-selector ambiguity caused by newly added methods on embedded types
([#114](https://github.com/ben-ranford/stave/issues/114)), or track private methods
promoted from hidden embedded receivers into exported concrete method sets
([#119](https://github.com/ben-ranford/stave/issues/119)). Changes involving these
cases require explicit compatibility review and consumer compilation evidence
before release; a passing inventory comparison alone is insufficient.

Generic sealed-method matching can also omit an implementation method when
the interface and receiver use different type-parameter names
([#107](https://github.com/ben-ranford/stave/issues/107)). Removing that method
can break consumer assignments without changing the inventory. These generic
relationships require explicit compatibility review and consumer compilation
until scoped type matching is supported.

Equivalent built-in alias spellings such as `byte`/`uint8` and `rune`/`int32`
can still produce different inventory text ([#110](https://github.com/ben-ranford/stave/issues/110)).
Preserve the existing spelling until that normalization is supported; review
unexpected differences before release.

`make release-baseline` requires an earlier stable v1 tag. When only
prereleases exist, it fails deliberately; until issue #2 provides a stable
baseline, a GA release cannot pass it. `make release-baseline-development` is
an opt-in, explicitly labelled comparison with `v1.0.0-rc.2`; it is
development evidence only and cannot serve as a GA baseline.

The baseline applies to v1 GA minor releases only. A future major release must
introduce its own major-version compatibility policy rather than reuse v1.

## Related guides

- [Adopt Stave in an application](client-adoption.md)
- [Security contract](security.md)
