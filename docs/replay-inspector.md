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
match the canonical serializer output. Inputs are bounded to 16 MiB and
100,000 records. The decoder rejects trailing
JSON, duplicate or unknown fields, unsupported schema versions, malformed
events, and inconsistent transcript chains. Invalid-input reports intentionally
do not echo raw input. Mismatch reports use the replay package's existing
sensitive-event redaction before serialization.

For evidence produced by a session, every event carries the event schema
version explicitly. A revision advances exactly when the model, tree, or
surface hash changes; configuration, theme, and capability hashes stay fixed
for the session. Effect-result records use the session's single delivery mode.

If the command cannot write its report, it exits `1` and emits a bounded diagnostic on standard error. A successful validation or comparison requires successful report output.
