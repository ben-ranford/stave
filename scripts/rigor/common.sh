#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cache_root="${repo_root}/.cache/rigor"
tool_bin_dir="${cache_root}/bin"
tmp_root="${cache_root}/tmp"
generated_dir="${repo_root}/scripts/rigor/generated"

golangci_lint_version="${GOLANGCI_LINT_VERSION:-v1.64.8}"
actionlint_version="${ACTIONLINT_VERSION:-v1.7.7}"
govulncheck_version="$(tr -d '[:space:]' < "${repo_root}/.govulncheck-version")"

ensure_rigor_dirs() {
	mkdir -p "${tool_bin_dir}" "${tmp_root}" "${generated_dir}"
}

install_go_tool() {
	local name="$1"
	local pkg="$2"
	local version="$3"
	local binary="${tool_bin_dir}/${name}"
	local stamp="${tool_bin_dir}/.${name}-${version}.stamp"

	ensure_rigor_dirs
	if [[ -x "${binary}" && -f "${stamp}" ]]; then
		return 0
	fi

	rm -f "${tool_bin_dir}/.${name}-"*.stamp 2>/dev/null || true
	GOBIN="${tool_bin_dir}" go install "${pkg}@${version}"
	touch "${stamp}"
}
