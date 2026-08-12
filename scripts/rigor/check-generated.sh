#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

if [[ "$#" -ne 1 ]]; then
	printf 'usage: %s <public-api|dependency-inventory|license-inventory|traceability>\n' "$0" >&2
	exit 1
fi

ensure_rigor_dirs
workdir="$(mktemp -d "${tmp_root}/generated.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT

case "$1" in
	public-api)
		go run ./scripts/rigor/cmd/rigor public-api --write "${workdir}/public-api.txt"
		diff -u "${generated_dir}/public-api.txt" "${workdir}/public-api.txt"
		;;
	dependency-inventory)
		go run ./scripts/rigor/cmd/rigor dependency-inventory --write "${workdir}/dependency-inventory.json"
		diff -u "${generated_dir}/dependency-inventory.json" "${workdir}/dependency-inventory.json"
		;;
	license-inventory)
		go run ./scripts/rigor/cmd/rigor license-inventory --write "${workdir}/license-inventory.json"
		diff -u "${generated_dir}/license-inventory.json" "${workdir}/license-inventory.json"
		;;
	traceability)
		go run ./scripts/rigor/cmd/rigor traceability \
			--write "${workdir}/traceability.json" \
			--atlas-output "${workdir}/atlas.render.txt" \
			--atlas-matrix-output "${workdir}/atlas.matrix.json" \
			--lopper-output "${workdir}/lopper.render.txt"
		diff -u "${generated_dir}/traceability.json" "${workdir}/traceability.json"
		diff -u "${generated_dir}/atlas.render.txt" "${workdir}/atlas.render.txt"
		diff -u "${generated_dir}/atlas.matrix.json" "${workdir}/atlas.matrix.json"
		diff -u "${generated_dir}/lopper.render.txt" "${workdir}/lopper.render.txt"
		;;
	*)
		printf 'unknown generated surface %s\n' "$1" >&2
		exit 1
		;;
esac
