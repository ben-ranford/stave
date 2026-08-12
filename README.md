# Stave

Stave is a renderer-independent Go UI primitives framework for branded human and agent interfaces. Applications own their identity (theme tokens, glyphs, assets, semantic labels, actions, and keymaps); Stave owns deterministic state, layout, rendering, protocol, and runtime contracts.

## Current v1 status

This release candidate contains the production package implementation and its conformance, replay, protocol, security, adapter, full-colour, and measured performance evidence. The first root-module publication is `v1.0.0-rc.1`; GA remains gated on published Lopper proving-client evidence and exact-tag release review.

Implemented package surfaces include `semantic`, `action`, `event`, `effect`, `state`, `session`, `replay`, `capability`, `theme`, `config`, `layout`, `surface`, `render`, `runtime/human`, `runtime/agent`, `primitive`, `protocol`, `diag`, `focus`, `keymap`, `input`, `secret`, `observer`, and `conformance`. Optional Bubble Tea, Lip Gloss, and SSH adapters remain nested modules so core consumers do not inherit adapter dependencies.

Lopper is the first proving client, not Stave's product boundary. Its migration contract, feature-flag rollout, parity fixtures, and rollback path live in [`docs/lopper-migration.md`](docs/lopper-migration.md). Atlas is the independent regression and proof rig: it runs seven semantic scenarios across nine capability profiles through the production `Program`, typed actions, snapshots, replay, rendering, and conformance contracts. See [`docs/atlas-regression-rig.md`](docs/atlas-regression-rig.md). Shared adapter fixtures prove the same surface can flow through optional UI stacks. The reusable adoption contract and supported client shapes are documented in [`docs/client-adoption.md`](docs/client-adoption.md).

## Proof points

- Root composition and action-reference validation: [`stave.go`](stave.go) and `go test .`
- Immutable semantics and exact replay: [`semantic/`](semantic/), [`session/`](session/), [`replay/`](replay/), and `go test ./semantic ./session ./replay`
- Human and agent runtimes: [`runtime/human/`](runtime/human/), [`runtime/agent/`](runtime/agent/), and `go test ./runtime/...`
- Full colour and accessibility contrast: [`capability/`](capability/), [`theme/`](theme/), [`render/`](render/), and `go test ./capability ./theme ./render`
- Multi-mode client, primitive, and adapter conformance: `go run ./cmd/stave-conformance`, `conformance.CheckClient`, and `make adapters`
- Independent non-Lopper proof rig: [`internal/atlasrig`](internal/atlasrig), [`cmd/atlas`](cmd/atlas), and `make atlas-check`
- Candidate and GA release evidence: `make verify`, `make release-contract`, and `make release-ga-contract`

## Verify

```sh
go test ./... -count=1
make verify
make schema-freshness
```

Schemas are checked in under [`schema/`](schema/), requirements and evidence under [`requirements/`](requirements/), and release/runbook policy under [`docs/`](docs/). Changes that affect a wire or semantic contract require schema review and migration notes.
