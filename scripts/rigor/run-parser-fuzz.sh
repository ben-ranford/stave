#!/usr/bin/env bash
set -euo pipefail

fuzz_time="${STAVE_PARSER_FUZZ_TIME:-30s}"

targets=(
	"./config:FuzzConfigParseCanonical"
	"./protocol:FuzzDecodeLineBounded"
)

for target in "${targets[@]}"; do
	pkg="${target%%:*}"
	name="${target##*:}"
	discovered="$(go test "${pkg}" -list "^${name}$" 2>/dev/null)"
	if ! rg -qx "${name}" <<<"${discovered}"; then
		printf 'required parser fuzz target missing: %s in %s\n' "${name}" "${pkg}" >&2
		exit 1
	fi
	go test "${pkg}" -run '^$' -fuzz "^${name}$" -fuzztime="${fuzz_time}"
done
