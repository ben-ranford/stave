# Performance Budgets

These are v1 release budgets. The checked-in core report records deterministic synthetic/component measurements; the live loopback PTY SSH round trip is a separate adapter test. Taken together they are release evidence for the recorded host, not a cross-platform latency guarantee.

## Budgets

| Target | Budget |
|---|---:|
| Pure reducer p95, ordinary input | `<= 2 ms` |
| View plus validate p95, 2,000 nodes | `<= 5 ms` |
| Layout plus surface diff p95, 120x40 viewport | `<= 8 ms` |
| Synthetic clone, arrange, and diff p95 | `<= 50 ms` |
| Live loopback SSH input-to-output p95 excluding network | `<= 75 ms` |
| Initial 10,000-node semantic snapshot | `<= 100 ms` |
| Initial 10,000-node incremental memory | `<= 64 MiB` |
| Idle CPU without active progress or motion | `< 1%` of one core |
| Headless deterministic render variance | `0` differing bytes |
| Protocol pure action acknowledgement p95 | `<= 100 ms` |
| Default maximum protocol line | `4 MiB` |
| Default maximum semantic nodes | `100,000` |
| Resize-storm recovery after last resize | `<= 100 ms` |
| Terminal restore after cancellation | `<= 250 ms` |

## Measurement policy

- Benchmark reports must record invocation args, sample count, strict flag, VCS revision when available, hardware, Go version, node distribution, viewport, renderer, capabilities, allocations, and percentiles.
- Any regression above 20% requires review and disposition before release.
- A budget miss may ship only with a documented ADR or explicit stage downgrade.

## Reproducible evidence gate

Run `make verify-performance` to execute the fixed-fixture percentile harness and
write `.artifacts/performance/report.json`. The report records three warmed,
isolated attempts per target, selects the lowest p95 as the uncontended
component cost, and retains all attempts for audit. It also records the invocation
args, requested sample count, strict-mode flag, fixed `GOMAXPROCS=1` execution, VCS revision when available,
Go/runtime and host identity, viewport, capability manifest, node count,
allocation delta, and nearest-rank p50/p95/p99 samples. `make benchmark-smoke`
runs the same harness with a short sample count for developer feedback. The
release run uses 101 nearest-rank samples and fails when a timing, allocation,
idle-CPU, or deterministic invariant misses its budget. Persistent
environment-specific misses require an ADR and explicit release stage downgrade
before a target is changed. When release automation needs a retained evidence
artifact, `scripts/rigor/refresh-generated.sh performance-report` mirrors the
latest report into the generated evidence directory.

The harness covers reducer execution, view validation, layout plus surface
diff, synthetic clone/arrange/diff, synthetic dispatch/arrange/diff, 10,000-node
snapshot serialization, pure protocol acknowledgement parsing,
resize-storm recovery, component terminal restore after cancellation, idle CPU, allocation
limits, and deterministic headless rendering. The live SSH adapter evidence
remains separate in `adapters/ssh/bridge_test.go::TestSSHTransportPerformanceBudget`.

## Current repo status

- Deterministic replay, snapshot properties, layout, surface diff, and semantic rendering are covered by package tests and benchmark fixtures.
- The measured 2,000-node arrange path is approximately 3.0 ms, 120x40 render approximately 7.3 ms, and surface diff approximately 25 µs on the recorded Apple M3 run; allocation and platform variance remain release follow-ups.
- The release report records invocation args, sample count, strict flag, VCS
  revision when available, host, Go runtime, OS/architecture, CPU count,
  balanced-tree distribution, renderer identity, viewport, capabilities,
  allocations, and nearest-rank percentiles. Cross-host comparison remains a
  release-owner review rather than an asserted universal guarantee.

## Contrast and brand fixtures

- `theme/theme_test.go::TestSemanticContrastAcrossColourLadder` is the full-colour/ANSI256/ANSI16 semantic contrast gate.
- `go run ./cmd/lopper` and `go run ./cmd/atlas` are the checked-in contrasting-brand render fixtures. Atlas additionally gates seven scenarios across nine capability profiles in `scripts/rigor/generated/atlas.matrix.json` via `make atlas-check`.
- These fixtures prove deterministic brand separation and semantic contrast policy, but do not replace interactive terminal or accessibility review.

## Performance controls

| Control | Owner | Proof command |
|---|---|---|
| Deterministic snapshot variance remains zero | Session owner | `go test ./replay ./session ./semantic` |
| Benchmarks cover reducer, render, protocol, and restore paths | Performance owner | `make benchmark-smoke` plus `.artifacts/performance/report.json` |
| Queue and memory limits are enforced under load | Runtime owner | `go test ./runtime/... ./effect ./session` |
| Resize-storm and timeout recovery are measured | Runtime owner | `make verify-performance` |

## Related documents

- [Release, Rollout, and Rollback Policy](release.md)
- [Protocol or Handler Timeout Runbook](runbooks/protocol-handler-timeout.md)
- [Replay Divergence Runbook](runbooks/replay-divergence.md)
