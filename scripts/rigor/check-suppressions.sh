#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
allowlist="${repo_root}/scripts/rigor/suppression-allowlist.txt"
matches_file="$(mktemp "${TMPDIR:-/tmp}/stave-suppressions.XXXXXX")"
trap 'rm -f "${matches_file}"' EXIT INT TERM
cd "${repo_root}"

if [[ ! -f "${allowlist}" ]]; then
	printf 'missing suppression allowlist: %s\n' "${allowlist}" >&2
	exit 1
fi

# Keep this pattern out of the source files scanned below so the guard cannot
# match its own implementation. Each result is deliberately line-specific:
# moving or changing an exception requires an explicit allowlist review.
set +e
rg -n '//[[:space:]]*(no''lint|no''sec|lint:(ignore|file-ignore))|#[[:space:]]*(no''qa|no''sec)|NOSONAR' --glob '*.go' >"${matches_file}"
grep_status=$?
set -e
if [[ "${grep_status}" -ne 0 && "${grep_status}" -ne 1 ]]; then
	exit "${grep_status}"
fi

if [[ ! -s "${matches_file}" ]]; then
	printf 'suppression check passed: no inline suppression markers found\n'
	exit 0
fi

unexpected=0
while IFS= read -r match; do
	if ! grep -Fqx -- "${match}" "${allowlist}"; then
		printf 'unapproved inline suppression: %s\n' "${match}" >&2
		unexpected=1
	fi
done <"${matches_file}"

if [[ "${unexpected}" -ne 0 ]]; then
	printf 'Inline suppressions require an exact, reviewed entry in scripts/rigor/suppression-allowlist.txt. Prefer fixing the underlying finding.\n' >&2
	exit 1
fi

printf 'suppression check passed: all inline suppressions are reviewed\n'
