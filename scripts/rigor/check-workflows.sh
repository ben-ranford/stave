#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

"${repo_root}/scripts/rigor/install-tools.sh" actionlint
"${tool_bin_dir}/actionlint" -config-file "${repo_root}/.github/actionlint.yaml"

workflow_guard_dir="${repo_root}/scripts/rigor/workflow-guard"
(
	cd "${workflow_guard_dir}"
	go test -mod=readonly ./...
	go run -mod=readonly . "${repo_root}/.github/workflows"
)
