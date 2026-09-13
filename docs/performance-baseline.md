# Performance baseline comparison

`stave-performance-compare` is an unreleased v1.1.0 development command. It
compares two saved reports produced by `stave-performance`; it never updates or
creates a baseline.

```sh
go run ./cmd/stave-performance -strict -out baseline.json
go run ./cmd/stave-performance -strict -out candidate.json
go run ./cmd/stave-performance-compare -baseline baseline.json -candidate candidate.json
```

Both reports must be from the same host, Go version, OS/architecture, CPU
count, fixture, capabilities, and exact run parameters. The executable path
(`os.Args[0]`) is excluded because launch and build locations can differ
between runs or revisions, and the `-out` destination is excluded because it
names the artifact rather than a measurement parameter. All remaining arguments
and environment metadata are compared. Source revisions are recorded and may
differ. The comparator rejects reports that fail their existing absolute
budgets, change their budget schema, or use incompatible environment or
reproducibility metadata.

It emits the versioned `stave.performance.comparison/v1` JSON envelope. Each
metric has an explicit threshold: p95 measurements and allocation allow a 10%
increase to absorb ordinary same-host noise, while idle CPU allows 0.10
percentage points. Exit status is `0` when all deltas are within tolerance, `2`
for a valid regression, `3` for invalid or incompatible input, and `64` for
usage errors.
