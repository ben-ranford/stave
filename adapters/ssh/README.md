# Stave SSH Adapter

`adapters/ssh` is an optional nested Go module that keeps Wish/SSH transport types out of the root Stave module.

It remains internal through the root `v1.0.0-rc.1` release; the local root
`replace` is workspace-only and must be removed for independent adapter
publication.

## Version pin

- Wish: `charm.land/wish/v2 v2.0.3`
- Wish toolchain declaration: `go 1.25.12`
- Root Stave is consumed through a local `replace github.com/ben-ranford/stave => ../..` for local development and tests.

## What the adapter owns

- Per-connection capability derivation from SSH PTY state, client environment, and window size.
- Per-client session construction through an adapter-local `SessionFactory`.
- Safe routing of input, resize, shutdown, and disconnect/cancel events into one isolated Stave session per SSH connection.
- Separate stdout and stderr/diagnostic streams.
- Connection-scoped writer budgets, environment budgets, command budgets, input budgets, and operation timeouts.
- Cleanup ordering that calls `Restore`, then closes the SSH session, then closes the bridged Stave session.

## What the adapter does not own

- Listener policy, authentication policy, or deployment topology.
- Application reducers, renderers, or domain actions.
- Any root-module import of Wish or `charm.land/ssh` types.

Use `Handler(...)` when you want a direct `charm.land/ssh` session handler. Use `Middleware(...)` when you are composing into a Wish server. The middleware is terminal: it fully handles the SSH session rather than falling through to `next`.

## Security and isolation notes

- Diagnostics are routed to stderr only; adapter diagnostics never go to stdout.
- Diagnostic text is sanitized and intentionally generic so resource-limit and transport errors do not echo input bytes or environment values.
- Each connection gets independent writers, negotiated capabilities, and a fresh session factory call; there is no package-global terminal state.
- Non-PTY sessions negotiate plain, noninteractive capabilities and avoid interactive assumptions.

## Local verification

Run from `adapters/ssh`:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
```

The PTY integration tests are skipped under `go test -race` because `charm.land/ssh v0.4.2` currently exposes a PTY session-state race during window mutation. The race run still covers the non-PTY fallback path and adapter-local synchronization.
