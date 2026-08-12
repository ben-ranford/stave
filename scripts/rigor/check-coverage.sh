#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 ]]; then
	printf 'usage: %s <minimum-percent> <coverprofile>\n' "$0" >&2
	exit 1
fi

minimum="$1"
profile="$2"

mkdir -p "$(dirname "${profile}")"
go test -covermode=atomic -coverprofile="${profile}" ./...
actual="$(
	go tool cover -func="${profile}" |
	awk '/^total:/ { gsub("%", "", $3); print $3 }'
)"

awk -v actual="${actual}" -v minimum="${minimum}" 'BEGIN { exit (actual + 0 >= minimum + 0) ? 0 : 1 }' || {
	printf 'coverage gate failed: %.1f%% < %.1f%%\n' "${actual}" "${minimum}" >&2
	exit 1
}

printf 'coverage gate passed: %.1f%% >= %.1f%%\n' "${actual}" "${minimum}"
