#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

install_named_tool() {
	case "$1" in
		golangci-lint)
			install_go_tool golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint "${golangci_lint_version}"
			;;
		actionlint)
			install_go_tool actionlint github.com/rhysd/actionlint/cmd/actionlint "${actionlint_version}"
			;;
		govulncheck)
			install_go_tool govulncheck golang.org/x/vuln/cmd/govulncheck "${govulncheck_version}"
			;;
		*)
			printf 'unknown tool %s\n' "$1" >&2
			exit 1
			;;
	esac
}

if [[ "$#" -eq 0 ]]; then
	set -- golangci-lint actionlint govulncheck
fi

for tool_name in "$@"; do
	install_named_tool "${tool_name}"
done
