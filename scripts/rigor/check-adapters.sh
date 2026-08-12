#!/usr/bin/env bash
set -euo pipefail

found=0
while IFS= read -r module_file; do
	[[ -n "${module_file}" ]] || continue
	found=1
	module_dir="$(dirname "${module_file}")"
	printf 'verifying nested module %s\n' "${module_dir}"
	(
		cd "${module_dir}"
		go test ./...
		go test -race ./...
		go vet ./...
	)
done < <(
	find . -name go.mod \
		-not -path './go.mod' \
		-not -path './.cache/*' \
		-not -path './vendor/*' \
		-not -path './scripts/rigor/*' |
	sort
)

if [[ "${found}" -eq 0 ]]; then
	printf 'no nested adapter modules detected\n'
fi
