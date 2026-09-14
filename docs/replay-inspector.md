# Replay transcript inspector

`stave-replay` is an unreleased v1.1.0 development command that validates a
saved canonical transcript or compares two saved transcripts outside an
application test harness. It compares recorded evidence only. It does not
execute an application model: replay execution needs the application's explicit
`replay.ApplyFunc`.

```sh
go run ./cmd/stave-replay validate -input transcript.json
go run ./cmd/stave-replay compare -expected expected.json -actual actual.json
```

The command writes one structured JSON result. Its exit status is `0` for a
valid transcript or matching evidence, `2` for a valid evidence mismatch, and
`3` for invalid input. Usage errors return `64`.

Validation checks transcript content; JSON whitespace and field order need not
match the canonical serializer output. Inputs are bounded to 16 MiB, 100,000
records, and 400,000 JSON values (scalars and containers; object keys do not
count separately). For semantic snapshots, the product of node count and total
relation count must not exceed 500,000 per validation pass (one million across
the two validation passes during decoding). The decoder rejects trailing
JSON, duplicate or unknown fields, unsupported schema versions, malformed
events, and inconsistent transcript chains. Invalid-input reports intentionally
do not echo raw input. Mismatch reports use the replay package's existing
sensitive-event redaction before serialization.

For evidence produced by a session, every event carries the event schema
version explicitly. A revision advances exactly when the model, tree, or
surface hash changes, and every revision advance changes the tree hash;
configuration, theme, and capability hashes stay fixed for the session. The
initial capability hash must match the saved manifest.
Non-effect records retain the effect ledger; effect-result records use the
session's single delivery mode and either retain the prior ledger for a
rejected event or derive it from the prior ledger and canonical event.
Validation establishes checkpoint consistency, not checkpoint origin; a saved
checkpoint can therefore begin with coherent prior ledger and declaration data.

Known producer limitation: an invalid effect request after a rendered state
change can produce mismatched event and result revisions
([#100](https://github.com/ben-ranford/stave/issues/100)). The inspector rejects
these inconsistent artifacts even if they use canonical JSON formatting.

If the command cannot write its report, it exits `1` and emits a bounded diagnostic on standard error. A successful validation or comparison requires successful report output.
