# Atlas Regression and Proof Rig

Atlas is Stave's application-owned regression client. It is not a tutorial,
reference product, or substitute for Lopper parity. Its purpose is to make the
framework's cross-client claims executable without importing another product's
domain model.

The rig lives in [`internal/atlasrig`](../internal/atlasrig), while
[`cmd/atlas`](../cmd/atlas) is a thin command wrapper. The checked-in manifest
is [`testdata/atlas-scenarios.json`](../testdata/atlas-scenarios.json), and the
deterministic matrix is
[`scripts/rigor/generated/atlas.matrix.json`](../scripts/rigor/generated/atlas.matrix.json).

## Proof matrix

Seven semantic scenarios run through nine negotiated capability profiles: 63
deterministic artifacts per matrix.

| Scenario | Contract exercised |
|---|---|
| `ready` | Branded grid, typed inspect/retire actions, table semantics |
| `empty` | First-class empty state and empty table window |
| `loading` | Live loading status and indeterminate progress |
| `error` | Safe alert text and explicit table error state |
| `invalid-form` | Text, numeric, checkbox, select, required/error relations |
| `modal-confirmation` | Hidden background, modal focus metadata, consequential confirmation |
| `dense-table` | Twenty-row window, stable row IDs, tabs, chart, pagination, master/detail |

Each scenario is rendered as machine JSON, truecolor, ANSI-256, ANSI-16,
monochrome, narrow ASCII, non-TTY plain output, reduced motion, and accessible
screen-reader line output.

## What is verified

- the same `stave.Program` owns model, reducer, view, action registry, theme,
  capability negotiation, session, snapshot, and transcript;
- semantic snapshots validate and retain stable hashes;
- action IDs in every tree exist in the typed registry and real keymap;
- read-only action schemas reject unknown fields;
- route retirement is consequential and its confirmation is single-use;
- a recorded Atlas session replays to the same checkpoint and transcript;
- full colour levels emit distinct terminal encodings while no-colour and
  machine profiles remain escape-free;
- narrow ASCII stays within the negotiated 28-column terminal surface;
- machine, plain, terminal, and semantic projections remain consistent;
- malformed manifests, unknown scenarios/profiles, duplicate JSON keys, and
  stale generated evidence fail closed;
- Atlas fixtures contain no Lopper language or dependency.

The matrix contains hashes and contract metadata rather than timestamps or
host data. Identical inputs therefore produce byte-identical checked-in proof.

## Commands

```sh
go run ./cmd/atlas                         # ready/machine-json fixture
go run ./cmd/atlas -list                   # available scenarios and profiles
go run ./cmd/atlas -scenario error -profile ansi16
go run ./cmd/atlas -matrix                 # complete deterministic matrix
go run ./cmd/atlas -verify                 # conformance + matrix determinism
make atlas-check                           # tests plus executable verification
make schema-freshness                      # checked-in matrix freshness
```

An intentional fixture change requires `make traceability-refresh`; generated
files are never edited by hand. `make verify` runs `atlas-check`, so future
Stave clients can treat Atlas as a regression oracle while still owning their
own domain fixtures and rollout policy.
