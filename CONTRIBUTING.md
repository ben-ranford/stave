# Contributing

Stave is a private Go source library with release-grade repository gates. Keep the core standard-library-only, preserve deterministic snapshots and schema contracts, and treat public API drift as an intentional reviewed change.

## Local workflow

1. Install the managed hooks with `make hooks-install`.
2. Use `make fast` for the pre-commit surface.
3. Use `make verify` before pushing.
4. Use `make ci` before cutting or validating a release branch or tag.

The rigor harness installs pinned repo-local tools into `.cache/rigor/bin`; no global `golangci-lint`, `actionlint`, or `govulncheck` installation is required.

## Release and compatibility policy

- Update `CHANGELOG.md` for user-visible behavior changes.
- Refresh tracked inventories with `make generated-refresh` whenever exported API, render traces, dependency shape, or schema evidence changes.
- Keep command examples (`cmd/atlas`, `cmd/lopper`) and schema evidence fresh; CI treats stale traceability artifacts as a failure.
- The root `github.com/ben-ranford/stave` package must remain standard-library-only unless the library policy changes explicitly.

## Validation reference

- Formatting: `make fmt-check`
- Lint and vet: `make lint vet`
- Test and race: `make test race`
- Coverage threshold: `make coverage-threshold`
- Fuzz smoke: `make fuzz-smoke`
- Benchmark smoke: `make benchmark-smoke`
- Security and workflow validation: `make govulncheck workflow-validate`
