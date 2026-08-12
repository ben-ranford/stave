# Primitive Contract Checklist

This checklist records the minimum v1 primitive surface and the current repo evidence level. `Implemented` means package tests and conformance fixtures exercise the production contract. `Partial` means the seam exists but release evidence is incomplete. `Planned` means the requirement is not yet implemented.

## P0 primitives

| Primitive | Minimum v1 contract | Current repo status | Owner | Proof command |
|---|---|---|---|---|
| Text | Stable ID, semantic role, value text, plain render | Implemented | Primitive owner | `go test ./primitive ./semantic ./render/...` |
| Stack or row or grid layout | Explicit layout primitives and deterministic narrow behavior | Implemented | Layout owner | `go test ./layout ./primitive` |
| Table or list | Stable row keys, headers, sorting, selection, narrow fallback | Implemented (P0 fixture) | Primitive owner | `go test ./primitive ./conformance` |
| Status | Named status semantics separate from raw styling | Implemented | Primitive owner | `go test ./primitive ./theme ./render/...` |
| Focus | Focusable semantics, visible focus, restoration rules | Implemented | Runtime owner | `go test ./focus ./runtime/human` |
| Disclosure | Expanded or collapsed state with stable actions | Implemented (P0 fixture) | Primitive owner | `go test ./primitive ./conformance` |
| Input | Typed input with validation and semantic value mapping | Implemented | Input owner | `go test ./input ./runtime/human` |
| Viewport | Capability-aware width policy and offscreen semantics | Implemented | Layout owner | `go test ./layout ./surface ./render/...` |
| Terminal frame | Terminal-safe render frame and restore path | Partial: platform soak pending | Runtime owner | `go test ./runtime/human ./surface` |
| Empty, loading, and error states | First-class semantic states and render fallbacks | Implemented | Primitive owner | `go test ./primitive ./render/...` |

## P1 detail requirements

| Area | Requirement |
|---|---|
| Tables | Sticky or header semantics, numeric alignment, selection, and non-modal disclosure |
| Progress | Determinate and indeterminate states with machine-readable progress |
| Overlay | Help overlay, popover, modal dialog, and command palette semantics |
| Master-detail | Wide and narrow modes with deterministic focus restoration |
| Secret fields | Presence and validation semantics without content exposure |

## Primitive-specific contracts

### Table

- Role path is `table -> rowgroup -> row -> cell`.
- Headers use `columnheader`.
- Sorting exposes current direction and typed sort actions.
- Narrow fallback keeps row IDs and actions stable.
- Large tables expose bounded windows plus total count and typed range actions.

### Master-detail

- Selection is keyed by stable entity identity.
- Narrow mode uses explicit `open_detail` and `back_to_master` actions.
- Focus restoration returns to the originating master row.

### Form

- Supported field families: text, multiline, number, select or combobox, checkbox, radio group, secret, and read-only value.
- Each field has stable ID, label, validation state, and error relation.
- Submission validates all fields before dispatch.

### Progress

- Determinate progress must expose current and total.
- Indeterminate progress must be explicit.
- Non-TTY output must emit bounded milestone events rather than animation frames.

### Overlay

- Modal focus is confined but always has a close or cancel path unless policy explicitly documents otherwise.
- Background nodes remain discoverable but inert while the overlay is active.

### Foundational primitives

The v1 foundational set is `Text`, `Heading`, `Status`, `Alert`, `Section`, `List`, `CodeBlock`, `Button`, `Link`, `Input`, `Spacer`, `Divider`, `Tabs`, and `Viewport`.

## Related documents

- [Accessibility and Agent Action Parity](accessibility-agent-parity.md)
- [Performance Budgets](performance.md)
- [ADR-007](adr/ADR-007.md)
- [ADR-016](adr/ADR-016.md)
