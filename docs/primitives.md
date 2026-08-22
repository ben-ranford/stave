# UI primitives

Stave primitives produce semantic UI nodes rather than tying your application
to a particular renderer. Supply stable application-owned identifiers and
semantic roles for every meaningful element.

## Available primitives

The foundational set is `Text`, `Heading`, `Status`, `Alert`, `Section`,
`List`, `CodeBlock`, `Button`, `Link`, `Input`, `Spacer`, `Divider`, `Tabs`,
and `Viewport`.

Use layout primitives such as stacks, rows, and grids to compose those nodes.
Tables, forms, progress indicators, overlays, and master-detail views add the
semantic requirements below.

## Tables

- Use the role path `table -> rowgroup -> row -> cell`.
- Use the `columnheader` role for table headers, and expose sort direction and
  typed sort actions.
- Keep row IDs and actions stable in narrow layouts.
- Use bounded windows and a total count for large tables.

## Master-detail views

- Key selection by stable entity identity.
- Provide explicit `open_detail` and `back_to_master` actions in narrow mode.
- Restore focus to the originating master row.

## Forms

- Supported fields include text, multiline, number, select or combobox,
  checkbox, radio group, secret, and read-only value.
- Give each field a stable ID, label, validation state, and error relation.
- Validate every field before dispatching submission.

## Progress

- Expose current and total for determinate progress.
- Mark indeterminate progress explicitly.
- Emit bounded milestones instead of animation frames in non-TTY output.

## Overlays

- Provide a help overlay, popover, modal dialog, or command palette as needed.
- Modal dialogs need a close or cancel path unless application policy says
  otherwise.
- Confine keyboard focus to an active modal and restore it when the modal
  closes.
- Keep background nodes discoverable but inert while a modal is active.

## Related guides

- [Accessibility and agent-control expectations](accessibility-agent-parity.md)
- [Adopt Stave in an application](client-adoption.md)
