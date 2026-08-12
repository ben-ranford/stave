#!/usr/bin/env bash
set -euo pipefail

tag="${1:-}"
semver_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$'
if [[ ! "${tag}" =~ ${semver_pattern} ]]; then
	printf 'invalid Stave release tag %s\n' "${tag:-<empty>}" >&2
	exit 1
fi

version_without_build="${tag%%+*}"
if [[ "${version_without_build}" == *-* ]]; then
	prerelease="${version_without_build#*-}"
	prerelease="${prerelease%%+*}"
	IFS='.' read -r -a identifiers <<< "${prerelease}"
	for identifier in "${identifiers[@]}"; do
		if [[ "${identifier}" =~ ^[0-9]+$ && "${identifier}" != "0" && "${identifier}" == 0* ]]; then
			printf 'invalid Stave release tag %s: numeric prerelease identifiers must not contain leading zeroes\n' "${tag}" >&2
			exit 1
		fi
	done
	printf 'prerelease\n'
else
	printf 'ga\n'
fi
