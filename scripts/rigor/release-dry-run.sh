#!/usr/bin/env bash
set -euo pipefail

if [[ -n "$(git status --porcelain)" ]]; then
	printf 'release candidate requires a clean worktree\n' >&2
	exit 1
fi

last_tag="$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || true)"
base_version="1.0.0"
if [[ -n "${last_tag}" ]]; then
	base_version="${last_tag#v}"
	base_version="${base_version%%-*}"
	base_version="${base_version%%+*}"
fi

timestamp="$(date -u +%Y%m%d%H%M%S)"
sha="$(git rev-parse --short=7 HEAD)"
candidate_tag="v${base_version}-rolling.${timestamp}.g${sha}"

channel="$("$(dirname "${BASH_SOURCE[0]}")/check-release-tag.sh" "${candidate_tag}")"
if [[ "${channel}" != prerelease ]] || [[ ! "${candidate_tag}" =~ -rolling\.[0-9]{14}\.g[0-9a-f]{7}$ ]]; then
	printf 'invalid rolling prerelease tag %s\n' "${candidate_tag}" >&2
	exit 1
fi

printf 'candidate_tag=%s\n' "${candidate_tag}"
printf 'base_version=%s\n' "${base_version}"
printf 'source_ref=%s\n' "$(git rev-parse HEAD)"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
	{
		printf 'candidate_tag=%s\n' "${candidate_tag}"
		printf 'base_version=%s\n' "${base_version}"
	} >> "${GITHUB_OUTPUT}"
fi
