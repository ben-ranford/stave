# Adopt Stave in an application

Stave is application-neutral: your application owns its model, language,
theme, actions, and effects. Stave provides the semantic UI and runtime
contracts that let those choices work consistently across human and automated
interfaces.

The current root module release candidate is `v1.0.0-rc.2` <!-- x-release-please-version -->.
It is not a GA promise; published Lopper parity and rollback evidence remains
the promotion gate. Begin with the root module only. The nested SSH, Bubble Tea,
and Lip Gloss modules are internal and are not yet supported consumer
dependencies. Pin this candidate after its release tag is published.

For a compiled first semantic tree, use the [root quick start](../README.md#quick-start).

## Runnable dual-runtime local-checkout tutorial

The unreleased `examples/dualruntime` package and `stave-dual-runtime` command
show one application session used by both hosts. From a local checkout run the
human line host with `printf 'inc\n' | go run ./cmd/stave-dual-runtime`, or the
JSONL host with `printf '{"jsonrpc":"2.0","id":1,"method":"stave.initialize"}\n{"jsonrpc":"2.0","id":2,"method":"stave.initialized"}\n{"jsonrpc":"2.0","id":3,"method":"stave.action.invoke","params":{"callId":"inc","actionId":"example.increment.v1"}}\n' | go run ./cmd/stave-dual-runtime -agent`.

The tutorial owns its action registry and explicit authorization callback;
`agent.BindSession` supplies only snapshots and once-only cancellation. Cancel
the command context and close the application session on shutdown. Configure
limits through `Program.NewSession`; see [configuration ownership](config-ownership.md)
and the existing security and stale-target contract tests before adapting the
example to production.

## Build an application

Use the public contracts in this order:

1. Define an application-owned model and pure `stave.Reducer`.
2. Derive an immutable `semantic.Tree` through `stave.View`.
3. Register typed, versioned actions in `action.Registry`.
4. Supply an application-owned theme, keymap, capability policy, and effects.
5. Create a session through `stave.Program.NewSession`.
6. Attach one or more runtimes or renderers without changing the model or
   semantic action authority.
7. Run `conformance.CheckClient` over representative fixtures and capability
   modes before claiming compatibility.

## Supported application shapes

| Client shape | Stave composition |
|---|---|
| Full-screen terminal application | `runtime/human` with terminal rendering, focus, keymap, and secure input |
| Line-oriented CLI | `runtime/human.LineDriver` with plain or monochrome output |
| Non-interactive snapshot or CI report | `render` plain or machine output without a TTY |
| Agent-controlled application | `runtime/agent` JSON-RPC over the same semantic tree and action registry |
| Remote SSH interface | Future SSH adapter around an application session |
| Existing Bubble Tea or Lip Gloss application | Future optional adapters consuming public Stave packages |
| Custom renderer or host framework | Application adapter consuming semantic/layout/surface contracts |

These are profiles of one framework, not forks. A client may expose several at
once—for example, a local TUI, non-interactive snapshot command, and agent
transport backed by the same session and action definitions.

## Validate an integration

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

Add representative domain fixtures in your application repository and keep
them green when upgrading Stave. Compatibility is defined by Stave's versioned
semantic, action, capability, theme, session, render, and protocol contracts.
