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

`make release-baseline` regenerates the candidate public API inventory from
source and compares it with the newest annotated stable `v1.x.y` tag. It
records the baseline tag, immutable commit, and both Go floors. A regenerated
candidate inventory cannot waive a removed declaration, changed signature,
interface method addition, or exported variable type change.

Adding an exported function or a field while retaining existing keyed fields is
accepted. Adding a field can still break consumers using unkeyed composite
literals, so those consumers should use keyed literals and maintainers must
call out that caveat during compatibility review.

There is currently no stable v1 tag. Until issue #2 provides one,
`make release-baseline` fails deliberately and a GA release cannot pass it.
`make release-baseline-development` is an opt-in, explicitly labelled
comparison with `v1.0.0-rc.2`; it is development evidence only and cannot
serve as a GA baseline.

## Related guides

- [Adopt Stave in an application](client-adoption.md)
- [Security contract](security.md)
