# Bubble Tea v2 Adapter

This module isolates Bubble Tea v2 integration from Stave core.

It remains an internal nested module through the root `v1.0.0-rc.1` release.
The local root `replace` is intentional for workspace verification; independent
adapter release automation must consume a matching tagged Stave module before
publishing this adapter.

## Scope

- Module path: `github.com/ben-ranford/stave/adapters/bubbletea`
- Bubble Tea pin: `charm.land/bubbletea/v2 v2.0.9`
- Toolchain floor in this module: Go 1.25
- Root Stave module remains Bubble Tea free

The adapter wraps a Bubble Tea `tea.Model` around Stave-native semantic-tree, action-registry, capability, render, event, and effect hooks. Bubble Tea types stay inside this nested module; the root module does not import Bubble Tea and does not expose Bubble Tea types in public signatures.

## Design

- Bubble Tea messages are normalized into public Stave `event.Event` values and public `effect.Request`/`effect.Call`/`effect.Outcome` values where possible.
- `tea.KeyPressMsg` and `tea.KeyReleaseMsg` map to `event.Key`.
- `tea.WindowSizeMsg` maps to `event.Resize`.
- `tea.PasteMsg` maps to `event.Text`.
- `tea.FocusMsg` and `tea.BlurMsg` map to `event.Focus` and `event.Blur`.
- Bubble Tea keyboard enhancement reports remain adapter-local because the root event schema does not define a dedicated kind for them.
- Reducers stay authoritative for state evolution.
- Semantic trees stay authoritative for rendered meaning.
- Action registries stay authoritative for action manifests and explicit action dispatch.
- Effects execute through the adapter `EffectHandler` and return to the reducer as `event.EffectResult` events.

## Terminal feature declarations

Bubble Tea v2 moved terminal features onto `tea.View` fields. This adapter preserves that boundary by deriving `tea.View` declarations from Stave capability/runtime state plus adapter `Features`:

- alternate screen
- mouse mode
- bracketed paste
- focus reporting
- keyboard enhancement requests
- window title

## Conformance hooks

`Hooks` exposes read-only observation points for:

- incoming Bubble Tea messages
- normalized Stave events
- rendered frames
- effect batches

The hooks exist for conformance and regression tests; they do not become application state.

## Local development

The nested module uses a local replace for the root module:

```go
replace github.com/ben-ranford/stave => ../..
```

That replace is local to this module and does not affect the root module graph.

## Migration notes

Use this adapter when an application wants Bubble Tea v2 program management without making Bubble Tea part of Stave core. Applications should keep domain state, semantic derivation, action schemas, and effect handlers in Stave-native code and treat this module as a transport/runtime edge.
