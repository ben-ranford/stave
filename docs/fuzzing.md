# Parser fuzzing

`make fuzz-smoke` discovers every repository fuzz target and runs one bounded
iteration. It includes configuration parsing/canonicalization and JSON-RPC
request framing. The parser targets seed duplicate and unknown fields, trailing
and null input, malformed UTF-8 and JSON, nesting boundaries, and exact input
size boundaries.

For a separately chosen, bounded parser run, use a local checkout:

```sh
STAVE_PARSER_FUZZ_TIME=30s make fuzz-parser-long
```

The command first verifies that both required parser fuzz targets are present.
`config.Parse` consumes a sparse layer and applies defaults. Canonical output
represents a resolved `Config`, so the configuration target decodes it into a
zero `Config` with strict JSON checks and then validates it before comparing
canonical bytes and hashes. Accepted configuration and request values must
round-trip canonically; invalid input must be bounded and must not echo the
seeded secret value in configuration or protocol errors. The configuration input-size and
nesting bounds belong to the fuzz harness; its nesting guard uses JSON tokens
so braces in strings remain covered. The protocol parser separately enforces
the caller-provided byte cap and its production depth-64 guard. Protocol
round-trip checks allow the emitted JSON length because escaping can expand
an accepted input.
A completed fuzz run exercises only the chosen time budget. It does not claim
that the library has no vulnerabilities.
