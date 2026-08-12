#!/usr/bin/env bash
set -euo pipefail

ran_any=0


while IFS= read -r pkg; do
	[[ -n "${pkg}" ]] || continue
	while IFS= read -r fuzz_target; do
		[[ -n "${fuzz_target}" ]] || continue
		ran_any=1
		go test "${pkg}" -run '^$' -fuzz "^${fuzz_target}$" -fuzztime=1x
	done < <(go test "${pkg}" -list '^Fuzz' 2>/dev/null | awk '/^Fuzz/ { print $1 }')
done < <(go list ./...)

if [[ "${ran_any}" -eq 0 ]]; then
	printf 'no fuzz targets detected\n'
fi
