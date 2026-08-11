# Stave

Stave is an architecture spike for adaptive, branded Go UI primitives for human and agent interfaces (BEN-107). The core is intentionally standard-library-only and keeps semantic content separate from rendering.

## Proof points

- Immutable-by-contract semantic `Node`/`Tree` values keep provisional logical IDs stable while generations change; the provisional `spike-v0` algorithm deliberately does not claim production `stave-node-id-v1` compatibility.
- Semantic roles are separate from application-owned style intents. Resolved themes are immutable, and two brands can style the same semantic tree without changing its meaning.
- One protocol dispatcher binds human and agent invocations to the current tree revision, target generation, node action references, and validated typed arguments.
- Capability-aware plain rendering actively degrades for narrow, ASCII, and no-color output. Because this renderer has no input loop or animation, its non-interactive and reduced-motion behavior is inherently identical.
- Snapshots are canonical JSON bytes, hostile content remains inert, and a minimal reducer transcript proves deterministic replay.
- `go run ./cmd/lopper` shows a Lopper-like master-detail brand; `go run ./cmd/atlas` shows a contrasting dashboard using the same primitives.

See the Go package documentation and tests for the Stave design proof. This spike deliberately does not depend on Bubble Tea, Lip Gloss, tcell, or Lopper.

## Deliberate limits

This is not the v0.1 runtime or the M0/M1 milestone exit. It does not yet include the production NodeKey and `stave-node-id-v1` normalization contract, lifecycle tombstones, a terminal event loop, asynchronous effects and effect replay, cancellation, secure-input redaction, a full layout/cell engine, or the JSON-RPC/JSONL transport. Those production contracts and their acceptance criteria are tracked in the Stave Linear project.
