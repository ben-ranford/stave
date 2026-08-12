# Architecture Overview

## Purpose

Stave v1 is a renderer-independent Go UI primitives framework. The canonical contract is semantic state plus typed actions, not terminal bytes, coordinates, or framework widget state.

Current repo status: `v1.0.0 release candidate`. The production package split, root composition facade, strict DTO boundaries, bounded session/runtime paths, semantic rendering, adapters, and conformance fixtures are present. Publication still requires remote CI and release review for the exact commit.

## Architecture invariants

| ID | Rule |
|---|---|
| AI-01 | Brand identity is application-owned data. Core primitives do not hard-code Lopper terms, colours, glyphs, or assets. |
| AI-02 | Core and public Stave packages do not import Lopper. |
| AI-03 | Lopper mapping, auth, and output-compatibility shims remain in Lopper. |
| AI-04 | State, event transcript, capabilities, theme, configuration, and effect outcomes determine replay and render results. |
| AI-05 | Reducers, tree derivation, layout, and rendering are testable without a live TTY. |
| AI-06 | Human and agent commands resolve through the same typed action surface. |
| AI-07 | Agents target semantic nodes and typed actions by default, not coordinates. |
| AI-08 | Colour, Unicode, motion, viewport, interactivity, and secure-input behavior are explicitly negotiated. |
| AI-09 | Drivers, renderers, clocks, observers, and effect executors are replaceable edges. |
| AI-10 | Public APIs expose no Bubble Tea, Lip Gloss, Bubbles, tcell, or Lopper types and use no mutable package-global session/theme state. |
| AI-11 | Published semantic trees and snapshots are immutable to callers. |
| AI-12 | Reducers and view functions do no I/O; all I/O crosses an effect boundary. |
| AI-13 | Rendered content never becomes a control-plane instruction. |
| AI-14 | Secret values never enter semantic trees, snapshots, diagnostics, or replay artifacts. |
| AI-15 | Only a renderer may emit terminal control sequences. |

## Dependency direction

Allowed flow:

```text
application/brand
    -> application adapter
        -> session + action + semantic + capability + theme + config
            -> layout + surface
                -> render/terminal or render/headless
                    -> runtime/human or runtime/agent

optional adapter modules
    -> public Stave packages only
```

Forbidden flow:

- Core or public packages importing Lopper.
- Core or public packages importing Bubble Tea, Lip Gloss, Bubbles, tcell, or other adapter dependencies.
- Renderers or runtimes receiving application business objects directly.
- Machine protocol relying on human-only rendered bytes.

## Target package layout

The v1 target package set is:

| Package | Responsibility | Status |
|---|---|---|
| `semantic` | Immutable nodes, roles, states, relations, IDs, trees | Implemented; root `Program` validates action references against its registry |
| `action` | Typed registry, schemas, invocation, results, errors | Implemented |
| `event` | Normalized human/system/effect events | Implemented |
| `session` | Deterministic reducer loop, transcript, replay | Implemented with bounded effect delivery and fail-closed replay validation |
| `capability` | Offers, policies, negotiation, terminal profiles | Implemented |
| `theme` | Semantic tokens, density, motion, glyphs | Implemented with immutable resolved themes and contrast validation |
| `layout` | Measure and arrange plans | Implemented |
| `surface` | Cells, styles, diffs, patches | Implemented |
| `render/terminal` | ANSI/plain renderer and terminal driver | Implemented; full-colour contrast evidence is fixture-backed, not a platform guarantee |
| `render/headless` | Semantic JSON, plain text, surface snapshots | Implemented |
| `runtime/human` | Input loop, lifecycle, focus, backpressure, restore | Implemented with a dependency-free line/non-TTY driver and optional full-screen adapters |
| `runtime/agent` | JSON-RPC/JSONL session bridge | Implemented with strict envelopes, application identity, bounded concurrency, cancellation, and typed errors |
| `primitive` | Contracted building blocks such as table, form, overlay | Implemented P0/P1 catalog with multi-mode conformance coverage |
| `protocol` | Versioned wire types and handshake/snapshot/action methods | Implemented; protocol schema is checked in and freshness-gated |
| `config` | Configuration schema, merge, validation | Implemented |
| `diag` | Typed errors, diagnostics, observer contracts | Implemented |
| `conformance` | Renderer/runtime/action/primitive conformance suite | Implemented for current adapters and P0 primitives |
| `internal` | Canonical encoding, width policy, guards, generated tables | Implemented |

Optional adapters must live in nested modules, for example `adapter/bubbletea` and `adapter/lipgloss`, so root consumers do not inherit their dependency graphs.

## Boundary rules

1. Application domain types may enter application adapters only.
2. Adapters produce Stave models, events, nodes, actions, themes, and assets.
3. Renderers accept resolved semantic/layout/surface inputs, not business objects.
4. Protocol clients receive redacted snapshots, actions, capabilities, and diagnostics.
5. Effect executors receive typed, validated action inputs only after schema, target, revision, capability, and confirmation checks.

## Current repo evidence

- Root facade: [`stave.go`](../stave.go)
- Semantic node and ID proof: [`semantic/semantic.go`](../semantic/semantic.go)
- Capability negotiation proof: [`capability/capability.go`](../capability/capability.go)
- Determinism and replay proof: [`stave_test.go`](../stave_test.go)
- Application migration and rollback: [`lopper-migration.md`](lopper-migration.md)
- Independent client proof matrix: [`atlas-regression-rig.md`](atlas-regression-rig.md), [`internal/atlasrig`](../internal/atlasrig), and [`cmd/atlas`](../cmd/atlas)
- Reusable client adoption and conformance contract: [`client-adoption.md`](client-adoption.md) and `conformance.CheckClient`

## Architecture control table

| Control | Owner | Proof command |
|---|---|---|
| Core import boundary excludes Lopper and optional adapters | Architecture owner | `go run ./scripts/rigor/cmd/rigor boundary-check` |
| Public package graph matches the target split | Architecture owner | `go list ./...` and `make api-boundary` |
| Deterministic replay and snapshot contract remains stable | Session owner | `go test ./replay ./session ./semantic` |
| Headless verification remains possible without a live TTY | Runtime owner | `go test ./render/... ./runtime/agent ./session` |
| New applications can verify their own fixtures without Lopper dependencies | Client owner | `make atlas-check`, `go test ./conformance`, and application-owned `conformance.CheckClient` suites |

## Related documents

- [Compatibility, Versioning, and Schema Policy](compatibility.md)
- [Dependency Policy](dependencies.md)
- [Primitive Contract Checklist](primitives.md)
- [ADR Index](adr/README.md)
