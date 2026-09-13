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

Inputs are bounded to 16 MiB and 100,000 records. The decoder rejects trailing
JSON, duplicate or unknown fields, unsupported schema versions, malformed
events, and inconsistent transcript chains. Invalid-input reports intentionally
do not echo raw input. Mismatch reports use the replay package's existing
sensitive-event redaction before serialization.
