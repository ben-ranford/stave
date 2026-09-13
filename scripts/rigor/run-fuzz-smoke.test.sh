#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/rigor/run-fuzz-smoke.sh"
temp_dir="$(mktemp -d)"
trap 'rm -rf "${temp_dir}"' EXIT

cat > "${temp_dir}/go" <<'GO'
#!/bin/sh
case "${GO_STUB_MODE:-valid}:$1:$2" in
	list-fail:list:*)
		echo 'MOCK go list failed' >&2
		exit 2
		;;
	target-fail:test:example/package)
		echo 'MOCK go test -list failed' >&2
		exit 2
		;;
	empty:test:example/package)
		exit 0
		;;
	*:list:*)
		echo example/package
		;;
	valid:test:example/package)
		case " $* " in
			*' -list '*) echo FuzzExample ;;
			*' -fuzz '*) printf '%s\n' "$*" >> "${GO_STUB_LOG}" ;;
		esac
		;;
esac
GO
chmod 700 "${temp_dir}/go"

run_with_stub() {
	PATH="${temp_dir}:${PATH}" "$@" bash "${script}"
}

expect_failure() {
	local expected="$1"
	shift
	local output
	if output="$(run_with_stub "$@" 2>&1)"; then
		printf 'expected fuzz smoke script to fail\n' >&2
		exit 1
	fi
	[[ "${output}" == *"${expected}"* ]] || {
		printf 'missing diagnostic: %s\n%s\n' "${expected}" "${output}" >&2
		exit 1
	}
}

expect_failure 'MOCK go list failed' env GO_STUB_MODE=list-fail
expect_failure 'MOCK go test -list failed' env GO_STUB_MODE=target-fail
expect_failure 'unexpected empty fuzz targets' env GO_STUB_MODE=empty

empty_output="$(run_with_stub env GO_STUB_MODE=empty STAVE_FUZZ_ALLOW_EMPTY=1 2>&1)"
[[ "${empty_output}" == *'no fuzz targets detected'* ]]

log_file="${temp_dir}/fuzz-runs.log"
run_with_stub env GO_STUB_MODE=valid GO_STUB_LOG="${log_file}"
[[ "$(wc -l < "${log_file}")" -eq 1 ]]
grep -Fx 'test example/package -run ^$ -fuzz ^FuzzExample$ -fuzztime=1x' "${log_file}"
