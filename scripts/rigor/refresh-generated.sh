#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

refresh_one() {
	case "$1" in
		public-api)
			go run ./scripts/rigor/cmd/rigor public-api --write "${generated_dir}/public-api.txt"
			;;
		dependency-inventory)
			go run ./scripts/rigor/cmd/rigor dependency-inventory --write "${generated_dir}/dependency-inventory.json"
			;;
		license-inventory)
			go run ./scripts/rigor/cmd/rigor license-inventory --write "${generated_dir}/license-inventory.json"
			;;
		traceability)
			go run ./scripts/rigor/cmd/rigor traceability \
				--write "${generated_dir}/traceability.json" \
				--atlas-output "${generated_dir}/atlas.render.txt" \
				--atlas-matrix-output "${generated_dir}/atlas.matrix.json" \
				--lopper-output "${generated_dir}/lopper.render.txt"
			;;
		performance-report)
			src="${PERFORMANCE_REPORT_SOURCE:-${repo_root}/.artifacts/performance/report.json}"
			dst="${PERFORMANCE_REPORT_DEST:-${generated_dir}/performance.report.json}"
			if [[ ! -f "${src}" ]]; then
				printf 'missing performance report %s\n' "${src}" >&2
				exit 1
			fi
			cp "${src}" "${dst}"
			;;
		*)
			printf 'unknown generated surface %s\n' "$1" >&2
			exit 1
			;;
	esac
}

ensure_rigor_dirs

if [[ "$#" -eq 0 || "$1" == "all" ]]; then
	for surface in public-api dependency-inventory license-inventory traceability; do
		refresh_one "${surface}"
	done
	exit 0
fi

refresh_one "$1"
