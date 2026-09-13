# Native hosted smoke checks

The CI workflow runs bounded root-module smoke tests on GitHub-hosted
`macos-14` and `windows-2025` runners. They execute startup, cancellation,
EOF, newline input, no-colour/non-TTY, and Unicode capability tests selected by
`scripts/rigor/native-smoke`.

The helper consumes `go test -json` and fails if any named smoke test has zero
matching test events. It does not replace Linux minimum/current-Go, race,
security, cross-compile, or full verification jobs.

Each native job has a 12-minute timeout. The pair therefore has a maximum
budget of 24 hosted runner minutes per workflow run; actual usage is visible in
the GitHub Actions run and should remain well below that ceiling. There are no
platform skips in this smoke set. Any future unsupported native behavior must
be skipped with a tracked issue and a documented rationale rather than treated
as a passing native check.
