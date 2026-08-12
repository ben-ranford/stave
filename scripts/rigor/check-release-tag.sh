#!/usr/bin/env bash
set -euo pipefail

tag="${1:-}"
if [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
	printf 'invalid Stave release tag %s\n' "${tag:-<empty>}" >&2
	exit 1
fi

if [[ "${tag}" == *-* ]]; then
	printf 'prerelease\n'
else
	printf 'ga\n'
fi
