# Configuration ownership

`config.Config` is validated by `Program.NewSession`, but validation does not
grant every runtime or adapter permission to consume every field. The following
map names the owner of each public configuration field.

| Field | Owner | Meaning |
| --- | --- | --- |
| `schemaVersion` | Program | Validates the supported configuration schema. |
| `app.id`, `app.version` | Application | Application identity supplied to integrations. |
| `theme.id` | Application | Application theme identity. |
| `theme.mode`, `theme.density` | Program | Resolves the Program theme. |
| `viewport.width`, `viewport.height` | Application | Runtime-detected or session-supplied viewport. |
| `capabilities.color`, `capabilities.unicode`, `capabilities.motion`, `capabilities.mouse`, `capabilities.alternateScreen` | Application | Capability policy supplied when composing a session. |
| `keymap.profile`, `keymap.bindings` | Application | Application action and key binding selection. |
| `runtime.mode`, `runtime.actionQueue`, `runtime.restoreOnPanic` | Application | Host runtime lifecycle policy. |
| `runtime.inputQueue` | Program | Session event queue capacity. |
| `protocol.enabled`, `protocol.transport` | Application | Protocol adapter selection and transport policy. |
| `protocol.maxMessageBytes` | Adapter-projected | Agent request size limit through `agent.OptionsFromConfig`. |
| `security.allowClipboard`, `security.allowCoordinateFallback`, `security.confirmationTTL` | Application | Security policy supplied while composing capabilities and confirmations. |
| `security.maxTreeNodes` | Adapter-projected | Agent snapshot tree limit through `agent.OptionsFromConfig`. |
| `diagnostics.level`, `diagnostics.format` | Application | Host diagnostics routing and presentation. |

To opt in to the documented agent projection, pass a validated configuration to
`agent.OptionsFromConfig`, then set any adapter-owned options explicitly. The
helper projects only message and tree limits; it does not select a transport or
change queue, output, concurrency, or Program runtime settings.

When a Program has already prepared a session, use `prepared.Config` as that
validated source before constructing the agent adapter.

## Unreleased local-checkout session bridge

`agent.BindSession` is an opt-in local-checkout API. It supplies a session's
snapshot envelope and an idempotent cancellation callback, while the
application continues to supply the action registry, authorization,
confirmation, and policy callbacks in `agent.Options`. It does not select a
transport or grant action authority.

The agent snapshot envelope projects the session's internal zero-based sequence
to the protocol's required one-based wire sequence. Revisions and hashes remain
unchanged.

Binding requires valid configuration and theme hashes. It projects at most the
latest 16 session diagnostics with a constant `SESSION_DIAGNOSTIC` code;
application-supplied codes, messages and attributes are not exposed. This
projection does not change the host session's retained diagnostic history.

Use a separate binding per snapshot consumer. Request a full snapshot, await its
response, then request patches sequentially from the most recently returned
revision. The bridge retains one baseline; stale patch requests are rejected
and should recover with a full snapshot. Concurrent consumers need separate
bindings and callbacks.
