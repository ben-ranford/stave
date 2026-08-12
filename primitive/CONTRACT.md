# Primitive contract

Primitive constructors are semantic-only. They do not select colours, glyphs,
terminal capabilities, or a global theme. Applications provide style intent and
brand metadata to a renderer.

Every interactive primitive has an accessible name, stable node ID, focus
metadata, and typed actions. The same action IDs are consumed by keyboard and
agent clients; coordinates are never required. Dialogs and modals include close
or cancel actions and `MasterDetail` exposes explicit `open_detail` and
`back_to_master` actions, so there is no unbounded focus trap.

Tables expose `table > rowgroup > row > cell`, stable `rowKey` metadata, header
nodes, sticky/numeric alignment intent, selection/disclosure actions, bounded
window metadata, and total counts. Reordering rows preserves their IDs.

Inputs mark secret values as redacted and expose only presence/validation
metadata. Charts retain text descriptions so ASCII, no-colour, narrow,
reduced-motion, non-TTY, screen-reader, and agent clients keep the same meaning.
