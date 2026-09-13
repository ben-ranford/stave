# Compatibility and versioning

Compatibility is defined at Stave's public application boundaries. Applications
remain free to choose their own domain model, language, theme, and host
environment.

## Independently versioned surfaces

| Surface | Policy |
|---|---|
| Root Go module | Semantic versioning |
| Semantic snapshot schema | Schema document ID `stave-semantic-snapshot-v1`; serialized `schemaVersion` `stave-semantic-v1` |
| Action definition schema | Schema document ID `stave-action-definition-v1`; each action carries its own version |
| Agent protocol | JSON-RPC `2.0` with Stave protocol `1.0` |
| Configuration schema | Schema document ID and serialized `schemaVersion` `stave.config/v1` |
| Node ID algorithm | `stave-node-id-v1` |
| Unicode width policy | Versioned algorithm identifier in snapshot envelopes |
| Theme/token packs | Semantic versioning and canonical content hash |
| Optional adapter modules | Independently versioned; third-party types stay out of root APIs |

Schema document IDs identify their JSON Schema artifacts. A serialized version
discriminant is only shown where the artifact defines one: configuration uses
the same identifier for both, while semantic snapshots use the shipped
hyphenated `stave-semantic-v1` value. Action definitions intentionally use
their per-action `version` field instead of a shared serialized schema version.

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

## Related guides

- [Adopt Stave in an application](client-adoption.md)
- [Security contract](security.md)
