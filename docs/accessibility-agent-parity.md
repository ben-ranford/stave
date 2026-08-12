# Accessibility and Agent Action Parity

## Principle

Stave v1 uses one semantic tree for both accessibility and machine control. Agents do not get a second, weaker contract, and accessibility metadata is not bolted onto rendered bytes after the fact.

## Shared node contract

Each meaningful node must be able to expose:

- Role
- Accessible name and description
- Current value and value text where applicable
- Interaction state such as disabled, selected, expanded, checked, busy, invalid, readonly, pressed, or multiline
- Collection metadata such as level, position, set size, and row or column indices
- Relations such as labelled-by, described-by, controls, owns, active-descendant, and error-message
- Focusability and focus state
- Available actions

## Parity rules

1. Every interactive node has a non-empty accessible name.
2. Every meaningful human action has a keyboard path.
3. Every keyboard action resolves through the same typed action registry used by agents.
4. Focus is visible in visual renderers and explicit in semantic snapshots.
5. Colour is never the sole state signal.
6. Tables expose header and cell relationships.
7. Form errors relate to the owning field.
8. Progress exposes current and total or a clear indeterminate state.
9. Dialogs expose name, cancel or close path, and focus restoration behavior.
10. Reduced motion is resolved centrally, not per animation implementation.
11. Accessibility and agent snapshots are derived from the same tree.

## Action policy

- Stable action IDs and schema-versioned arguments are mandatory.
- Invalid, stale, ambiguous, unsupported, or unauthorized actions fail with structured errors.
- Global actions must not become ambiguous when surfaced from multiple nodes.
- Coordinate input is optional, negotiated, and lower trust than semantic targeting.

## Capability policy

- Headless and non-interactive clients must still receive typed snapshots and action manifests.
- Reduced-motion, narrow, ASCII, no-colour, and non-TTY modes are contract paths, not degraded afterthoughts.
- Secret fields expose only presence and validation state, never content.

## Operational checklist

| Requirement | Owner | Proof command |
|---|---|---|
| Interactive nodes have accessible names and actions | Accessibility owner | `go test ./conformance ./primitive` |
| Keyboard and agent paths resolve through the same action registry | Protocol owner | `go test ./keymap ./runtime/agent ./conformance` |
| Snapshot schema exposes focus, relations, and state | Schema owner | `go test ./semantic ./schema/...` and `make schema-freshness` |
| Degradation modes preserve meaning without colour or motion | Render owner | `go test ./capability ./render/... ./theme` |

## Related documents

- [Primitive Contract Checklist](primitives.md)
- [Security and Threat Model](security.md)
- [ADR-002](adr/ADR-002.md)
- [ADR-010](adr/ADR-010.md)
- [Runbooks](runbooks/README.md)
