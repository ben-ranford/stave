# Stave

Stave is a renderer-independent Go UI primitives framework for branded human and agent interfaces. Applications own their identity (theme tokens, glyphs, assets, semantic labels, actions, and keymaps); Stave owns deterministic state, layout, rendering, protocol, and runtime contracts.

## Documentation

Start with the [adoption guide](docs/client-adoption.md), then see the
[primitive reference](docs/primitives.md),
[accessibility and agent-control expectations](docs/accessibility-agent-parity.md),
[compatibility guarantees](docs/compatibility.md), and
[security contract](docs/security.md).

## Verify

```sh
go test ./... -count=1
make verify
make schema-freshness
```

Schemas are checked in under [`schema/`](schema/). Changes that affect a wire
or semantic contract require schema review and migration notes.
