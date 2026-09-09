# Stave Lip Gloss v2 Adapter

This module isolates the optional Lip Gloss dependency from the root Stave module.

It is internal through the root `v1.0.0-rc.1` release and is not a supported
consumer dependency yet. The local root `replace` is workspace-only and must
be removed for independent adapter publication with its own version and tag.

## Scope

- Module path: `github.com/ben-ranford/stave/adapters/lipgloss`
- Go version: `1.25`
- Lip Gloss pin: `charm.land/lipgloss/v2 v2.0.6`
- Root Stave import: local nested-module `replace github.com/ben-ranford/stave => ../..`

The root Stave module does not import Lip Gloss and does not expose Lip Gloss types. This adapter consumes public Stave packages such as `capability` and `surface`, and renders the resolved `surface.Surface` style data directly so it stays isolated from root theme internals.

## Contract

The adapter compiles Stave surface styles into pure Lip Gloss v2 styles. It does not mutate Stave semantics, IDs, or layout geometry.

- Dark and light appearance are explicit in the adapter profile.
- Output mode, TTY behavior, and color level are explicit in the adapter profile.
- Plain and machine-oriented output stay ANSI-free.
- Surface width is preserved by padding blank cells and validating cell widths before rendering.
- Monochrome uses contrast-safe foreground fallback plus reverse-video for background-only emphasis instead of inventing application semantics.

## Version isolation

Charm major-version churn is contained here. The nested module can change independently as long as the Stave-facing adapter contract remains compatible.
