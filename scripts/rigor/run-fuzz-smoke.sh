#!/usr/bin/env bash
set -euo pipefail

ran_any=0

if ! packages="$(go list ./...)"; then
	printf 'fuzz package discovery failed\n' >&2
	exit 1
fi

while IFS= read -r pkg; do
	[[ -n "${pkg}" ]] || continue
	if ! fuzz_targets="$(go test "${pkg}" -list '^Fuzz' | awk '/^Fuzz/ { print $1 }')"; then
		printf 'fuzz target discovery failed for package %s\n' "${pkg}" >&2
		exit 1
	fi
	while IFS= read -r fuzz_target; do
		[[ -n "${fuzz_target}" ]] || continue
		ran_any=1
		go test "${pkg}" -run '^$' -fuzz "^${fuzz_target}$" -fuzztime=1x
	done <<< "${fuzz_targets}"
done <<< "${packages}"

if [[ "${ran_any}" -eq 0 ]]; then
	if [[ "${STAVE_FUZZ_ALLOW_EMPTY:-0}" != "1" ]]; then
		printf 'unexpected empty fuzz targets; set STAVE_FUZZ_ALLOW_EMPTY=1 to allow this intentionally\n' >&2
		exit 1
	fi
	printf 'no fuzz targets detected\n'
fi
