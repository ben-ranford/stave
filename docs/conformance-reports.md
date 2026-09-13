# Consume bounded conformance reports

Applications using a local checkout of the unreleased 1.1 development surface
can turn failures they already produced with `conformance` into a stable JSON
artifact for CI. It is not available in the published `v1.0.0-rc.2` module.
This is a report format only: it does not load Go fixtures, run renderers, or
execute application code.

## Library contract

Use `conformance.NewJSONReport` to add the schema version and documentation
links to `conformance.Failure` values, then use `conformance.MarshalJSONReport`
to produce canonical JSON. The format is `stave.conformance.report.v1`.

Reports contain a `schemaVersion` and a deterministically sorted `failures`
array. Every failure has `path`, `rule`, `detail`, and `documentation` fields.
`documentation` is the repository-relative guide anchor for built-in rules when
one exists, and is otherwise an empty string. Consumers must preserve that
value instead of substituting a caller-controlled URL.

`ParseJSONReport` and `ReadJSONReport` are strict boundaries for received
reports. They reject unknown or duplicate JSON fields, trailing JSON, an
unsupported schema version, incorrect documentation links, more than 256
failures, any field larger than 4 KiB, and reports larger than 64 KiB.

## CLI formatting

The existing `go run ./cmd/stave-conformance` command still runs Stave's
primitive conformance catalog. To format an already-produced report without
running that catalog, pass exactly one report file or standard input:

```sh
go run ./cmd/stave-conformance --report report.json
cat report.json | go run ./cmd/stave-conformance --report -
```

The command accepts only the versioned report format in this mode and writes
canonical JSON to standard output. Invalid or oversized input exits before any
fixture, plugin, renderer, or application code is invoked.
