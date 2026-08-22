# Accessibility and agent-control expectations

## Principle

Stave uses one semantic tree for accessibility and machine control. Agents do
not get a second, weaker contract, and accessibility metadata is not bolted
onto rendered bytes after the fact.

## Shared node contract

Each meaningful node must expose the applicable parts of this contract:

- Role
- Accessible name and description
- Current value and value text where applicable
- Interaction state such as disabled, selected, expanded, checked, busy,
  invalid, readonly, pressed, or multiline
- Collection metadata such as level, position, set size, and row or column
  indices
- Relations such as labelled-by, described-by, controls, owns,
  active-descendant, and error-message
- Focusability and focus state
- Available actions

## Parity rules

1. Every interactive node has a non-empty accessible name.
2. Every meaningful human action has a keyboard path.
3. Every keyboard action resolves through the same typed action registry used
   by agents.
4. Focus is visible in visual renderers and explicit in semantic snapshots.
5. Colour is never the sole state signal.
6. Tables expose header and cell relationships.
7. Form errors relate to the owning field.
8. Progress exposes current and total or a clear indeterminate state.
9. Dialogs expose a name, cancel or close path, and focus-restoration behavior.
10. Reduced motion is resolved centrally, not per animation.
11. Accessibility and agent snapshots derive from the same tree.

## Action policy

- Stable action IDs and schema-versioned arguments are mandatory.
- Invalid, stale, ambiguous, unsupported, or unauthorized actions fail with
  structured errors.
- Global actions must not become ambiguous when surfaced from multiple nodes.
- Coordinate input is optional, negotiated, and lower trust than semantic
  targeting.

## Capability policy

- Headless and non-interactive clients still receive typed snapshots and action
  manifests.
- Reduced-motion, narrow, ASCII, no-colour, and non-TTY modes are contract
  paths, not afterthoughts.
- Secret fields expose only presence and validation state, never content.

## Related guides

- [UI primitives](primitives.md)
- [Security contract](security.md)
