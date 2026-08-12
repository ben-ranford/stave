#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

"${repo_root}/scripts/rigor/install-tools.sh" actionlint
"${tool_bin_dir}/actionlint" -config-file "${repo_root}/.github/actionlint.yaml"
