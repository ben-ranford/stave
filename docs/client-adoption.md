# Client Adoption Guide

Stave is an application-neutral UI framework. Lopper is the first proving
client, but it does not receive a privileged API and its domain model, feature
flags, commands, themes, and rollout policy remain in the Lopper repository.

Every client adopts the same public contracts:

1. Define an application-owned model and pure `stave.Reducer`.
2. Derive an immutable `semantic.Tree` through `stave.View`.
3. Register typed, versioned actions in `action.Registry`.
4. Supply an application-owned theme, keymap, capability policy, and effects.
5. Create a session through `stave.Program.NewSession`.
6. Attach one or more runtimes or renderers without changing the model or
   semantic action authority.
7. Run `conformance.CheckClient` over representative fixtures and capability
   modes before claiming compatibility.

## Supported client shapes

| Client shape | Stave composition |
|---|---|
| Full-screen terminal application | `runtime/human` with terminal rendering, focus, keymap, and secure input |
| Line-oriented CLI | `runtime/human.LineDriver` with plain or monochrome output |
| Non-interactive snapshot or CI report | `render` plain or machine output without a TTY |
| Agent-controlled application | `runtime/agent` JSON-RPC over the same semantic tree and action registry |
| Remote SSH interface | Nested `adapters/ssh` module around an application session |
| Existing Bubble Tea or Lip Gloss application | Nested optional adapters consuming public Stave packages |
| Custom renderer or host framework | Application adapter consuming semantic/layout/surface contracts |

These are profiles of one framework, not forks. A client may expose several at
once—for example, a local TUI, non-interactive snapshot command, and agent
transport backed by the same session and action definitions.

## Client conformance

`conformance.Client` requires the renderer, typed action registry, and keymap
authority used by the application. `conformance.ClientFixture` supplies named
semantic scenarios and negotiated modes. `conformance.CheckClient` verifies:

- semantic accessibility and stable node identity;
- typed action registration and keyboard/action parity;
- secret redaction and field/dialog/table relations;
- rendering across colour, no-colour, Unicode, ASCII, narrow, non-TTY, and
  reduced-motion modes selected by the client;
- that the integration supplies real authority rather than self-declared
  action labels.

The checked-in primitive catalog runs through this same client contract with
`go run ./cmd/stave-conformance`. Applications should add their own domain
fixtures in their repositories and keep them green alongside Stave upgrades.

## Proving clients

- Lopper is the first migration client and exercises strangler rollout,
  legacy parity, typed domain actions, consequential confirmation, and
  rollback.
- Atlas is an independent second client and executable proof rig with a
  different model, brand, layouts, typed actions, replay, and 63-surface
  scenario/capability matrix. See [`atlas-regression-rig.md`](atlas-regression-rig.md).
- The Bubble Tea, Lip Gloss, and SSH modules prove that optional host stacks do
  not enter the root dependency graph.

New clients must not depend on Lopper packages, assets, schemas, feature flags,
or command semantics. Compatibility is defined by Stave's versioned semantic,
action, capability, theme, session, render, and protocol contracts.
