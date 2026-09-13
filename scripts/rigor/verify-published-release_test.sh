#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/rigor/verify-published-release.sh"
workdir="$(mktemp -d "${TMPDIR:-/tmp}/stave-release-probe-test.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT

mkdir -p "${workdir}/bin"
sha256() {
	local value="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		printf '%s' "${value}" | sha256sum | awk '{print $1}'
	else
		printf '%s' "${value}" | shasum -a 256 | awk '{print $1}'
	fi
}
CHANGELOG_DIGEST="$(sha256 changelog)"
LICENSE_DIGEST="$(sha256 license)"
REPORT_DIGEST="$(sha256 report)"
export CHANGELOG_DIGEST LICENSE_DIGEST REPORT_DIGEST
cat >"${workdir}/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
out=''
for ((i = 1; i <= $#; i++)); do
	if [[ "${!i}" == --output ]]; then
		((i += 1)); out="${!i}"
	fi
done
url="${!#}"
if [[ "${FAIL_CURL:-}" == 1 ]]; then exit 22; fi
case "${url}" in
*/releases/tags/*)
	tag_name="${url##*/}"
	if [[ "${INCOMPLETE_METADATA:-}" == 1 ]] || { [[ "${DELAY_METADATA:-}" == 1 ]] && [[ ! -e "$(dirname "$0")/metadata-delayed" ]]; }; then
		touch "$(dirname "$0")/metadata-delayed"
		printf '%s' '{"tag_name":"v1.0.0-rc.2","assets":[]}' >"${out}"
	else
		cat >"${out}" <<JSON
{"tag_name":"${tag_name}","assets":[{"name":"CHANGELOG.md","digest":"sha256:${CHANGELOG_DIGEST}","browser_download_url":"https://assets/CHANGELOG.md"},{"name":"LICENSE","digest":"sha256:${LICENSE_DIGEST}","browser_download_url":"https://assets/LICENSE"},{"name":"report.json","digest":"sha256:${REPORT_DIGEST}","browser_download_url":"https://assets/report.json"}]}
JSON
	fi
	;;
*/git/ref/tags/*) printf '%s' '{"object":{"sha":"tag-object","type":"tag"}}' >"${out}" ;;
*/git/tags/tag-object) printf '%s' '{"object":{"sha":"source-commit","type":"commit"}}' >"${out}" ;;
https://assets/CHANGELOG.md) printf changelog >"${out}" ;;
https://assets/LICENSE) printf license >"${out}" ;;
https://assets/report.json) printf report >"${out}" ;;
*) exit 22 ;;
esac
EOF
cat >"${workdir}/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
'mod init') printf 'module example.com/stave-release-probe\n' >go.mod ;;
'get github.com/ben-ranford/stave@'*)
	bin_dir="$(dirname "$0")"
	requested_version="${2#github.com/ben-ranford/stave@}"
	canonical_version="${requested_version%%+*}"
	[[ "$(<"${bin_dir}/fail-go-get")" != 1 ]] || exit 1
	if [[ "$(<"${bin_dir}/term-ignore-go-get")" == 1 ]]; then
		printf '%s\n' "$$" >"${bin_dir}/term-ignore-parent-pid"
		spawn_replacement() {
			sleep 30 & child_pid=$!
			printf '%s\n' "${child_pid}" >"${bin_dir}/term-ignore-replacement-child-pid"
			wait "${child_pid}" || true
		}
		trap 'spawn_replacement' TERM
		while :; do
			sleep 30 & child_pid=$!
			printf '%s\n' "${child_pid}" >"${bin_dir}/term-ignore-child-pid"
			wait "${child_pid}" || true
		done
	fi
	if [[ "$(<"${bin_dir}/slow-go-get")" == 1 ]]; then
		printf '%s\n' "$$" >"${bin_dir}/slow-go-get-pid"
		sleep 30
	fi
	if [[ "$(<"${bin_dir}/record-fast-go-get")" == 1 ]]; then printf '%s\n' "$$" >"${bin_dir}/fast-go-get-pid"; fi
	printf '%s\n' "${canonical_version}" >"${bin_dir}/module-version"
	printf '\nrequire github.com/ben-ranford/stave %s\n' "${canonical_version}" >>go.mod
	;;
'list -m')
	bin_dir="$(dirname "$0")"
	module_version="$(<"${bin_dir}/module-version")"
	if [[ "${4:-}" == *@* ]] && [[ -s "${bin_dir}/resolution-version-override" ]]; then module_version="$(<"${bin_dir}/resolution-version-override")"; fi
	if [[ "${4:-}" != *@* ]] && [[ -s "${bin_dir}/module-version-override" ]]; then module_version="$(<"${bin_dir}/module-version-override")"; fi
	origin_sha=source-commit
	if [[ -s "${bin_dir}/origin-override" ]]; then origin_sha="$(<"${bin_dir}/origin-override")"; fi
	printf '{"Path":"github.com/ben-ranford/stave","Version":"%s","Sum":"h1:publicsum","Origin":{"Hash":"%s"}}\n' "${module_version}" "${origin_sha}"
	;;
'mod download')
	bin_dir="$(dirname "$0")"
	module_version="${4#github.com/ben-ranford/stave@}"
	origin_sha=source-commit
	if [[ -s "${bin_dir}/origin-override" ]]; then origin_sha="$(<"${bin_dir}/origin-override")"; fi
	printf '{"Path":"github.com/ben-ranford/stave","Version":"%s","Sum":"h1:publicsum","Origin":{"Hash":"%s"}}\n' "${module_version}" "${origin_sha}"
	;;
'run .') printf 'text: public module\n' ;;
*) printf 'unexpected go invocation: %s %s\n' "$1" "$2" >&2; exit 1 ;;
esac
EOF
chmod +x "${workdir}/bin/curl" "${workdir}/bin/go"
: >"${workdir}/bin/fail-go-get"
: >"${workdir}/bin/slow-go-get"
: >"${workdir}/bin/term-ignore-go-get"
: >"${workdir}/bin/record-fast-go-get"
: >"${workdir}/bin/module-version-override"
: >"${workdir}/bin/resolution-version-override"
: >"${workdir}/bin/origin-override"

PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/report.json"
jq -e '.tag_object_sha == "tag-object" and .source_sha == "source-commit" and .module.sum == "h1:publicsum" and .module.selected_version == "v1.0.0-rc.2" and .module.requested_version == .module.selected_version and .module.origin_sha == .source_sha and (.assets | length == 3)' "${workdir}/report.json" >/dev/null

for tag_name in v1.0.0+build.7 v1.0.0-rc.2+build.7; do
	PATH="${workdir}/bin:${PATH}" "${script}" "${tag_name}" >"${workdir}/build-metadata.json"
	jq -e --arg tag_name "${tag_name}" --arg canonical_version "${tag_name%%+*}" '.tag == $tag_name and .module.selected_version == $canonical_version and .module.requested_version == $canonical_version and .module.origin_sha == .source_sha' "${workdir}/build-metadata.json" >/dev/null
done

printf 'v0.1.1-0.20260101000000-sourcecommit\n' >"${workdir}/bin/module-version-override"
printf 'v0.1.1-0.20260101000000-sourcecommit\n' >"${workdir}/bin/resolution-version-override"
PATH="${workdir}/bin:${PATH}" "${script}" v0.1.0+build.7 >"${workdir}/pseudo-version.json"
jq -e '.module.selected_version == "v0.1.1-0.20260101000000-sourcecommit" and .module.requested_version == .module.selected_version and .module.origin_sha == .source_sha' "${workdir}/pseudo-version.json" >/dev/null
: >"${workdir}/bin/module-version-override"
: >"${workdir}/bin/resolution-version-override"

printf 'v9.9.9\n' >"${workdir}/bin/module-version-override"
if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0+build.7 >"${workdir}/canonical-mismatch.out" 2>"${workdir}/canonical-mismatch.err"; then
	printf 'expected canonical module version mismatch\n' >&2
	exit 1
fi
: >"${workdir}/bin/module-version-override"

printf 'wrong-source\n' >"${workdir}/bin/origin-override"
if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2+build.7 >"${workdir}/origin-mismatch.out" 2>"${workdir}/origin-mismatch.err"; then
	printf 'expected module origin mismatch\n' >&2
	exit 1
fi
: >"${workdir}/bin/origin-override"

PATH="${workdir}/bin:${PATH}" DELAY_METADATA=1 RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/delayed-metadata.json"
jq -e '(.assets | length == 3)' "${workdir}/delayed-metadata.json" >/dev/null

if PATH="${workdir}/bin:${PATH}" INCOMPLETE_METADATA=1 RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/metadata-failure.out" 2>"${workdir}/metadata-failure.err"; then
	printf 'expected incomplete release metadata failure\n' >&2
	exit 1
fi
grep -q 'release metadata incomplete after 2 attempts; missing digest or download URL for: CHANGELOG.md LICENSE report.json' "${workdir}/metadata-failure.err"

if PATH="${workdir}/bin:${PATH}" FAIL_CURL=1 RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/failure.out" 2>"${workdir}/failure.err"; then
	printf 'expected propagation failure\n' >&2
	exit 1
fi
grep -q 'failed after 2 attempts' "${workdir}/failure.err"

printf '1\n' >"${workdir}/bin/fail-go-get"
if PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/module-failure.out" 2>"${workdir}/module-failure.err"; then
	printf 'expected public module propagation failure\n' >&2
	exit 1
fi
grep -q 'failed after 2 attempts while resolving github.com/ben-ranford/stave@v1.0.0-rc.2' "${workdir}/module-failure.err"

: >"${workdir}/bin/fail-go-get"
printf '1\n' >"${workdir}/bin/record-fast-go-get"
PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_GO_TIMEOUT_SECONDS=3 "${script}" v1.0.0-rc.2 >"${workdir}/fast-success.json"
fast_pid="$(<"${workdir}/bin/fast-go-get-pid")"
if kill -0 "${fast_pid}" 2>/dev/null; then
	printf 'fast public Go resolution left a child process running\n' >&2
	exit 1
fi

: >"${workdir}/bin/record-fast-go-get"
printf '1\n' >"${workdir}/bin/slow-go-get"
SECONDS=0
if PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 RELEASE_PROBE_GO_TIMEOUT_SECONDS=1 RELEASE_PROBE_TERMINATE_GRACE_SECONDS=1 "${script}" v1.0.0-rc.2 >"${workdir}/timeout-failure.out" 2>"${workdir}/timeout-failure.err"; then
	printf 'expected public module timeout failure\n' >&2
	exit 1
fi
((SECONDS < 8)) || { printf 'stalled public module probe exceeded its bounded timeout\n' >&2; exit 1; }
grep -q 'release probe timed out after 1s while resolving github.com/ben-ranford/stave@v1.0.0-rc.2' "${workdir}/timeout-failure.err"
slow_pid="$(<"${workdir}/bin/slow-go-get-pid")"
if kill -0 "${slow_pid}" 2>/dev/null; then
	printf 'stalled public Go resolution remained after timeout cleanup\n' >&2
	exit 1
fi

: >"${workdir}/bin/slow-go-get"
printf '1\n' >"${workdir}/bin/term-ignore-go-get"
SECONDS=0
if PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=1 RELEASE_PROBE_RETRY_SECONDS=0 RELEASE_PROBE_GO_TIMEOUT_SECONDS=1 RELEASE_PROBE_TERMINATE_GRACE_SECONDS=1 "${script}" v1.0.0-rc.2 >"${workdir}/term-ignore-failure.out" 2>"${workdir}/term-ignore-failure.err"; then
	printf 'expected TERM-ignoring public module timeout failure\n' >&2
	exit 1
fi
((SECONDS < 5)) || { printf 'TERM-ignoring public module probe exceeded its bounded timeout\n' >&2; exit 1; }
[[ -s "${workdir}/bin/term-ignore-replacement-child-pid" ]] || { printf 'TERM-ignoring fixture did not replace its child during cleanup\n' >&2; exit 1; }
for process_pid in "$(<"${workdir}/bin/term-ignore-parent-pid")" "$(<"${workdir}/bin/term-ignore-child-pid")" "$(<"${workdir}/bin/term-ignore-replacement-child-pid")"; do
	process_state="$({ ps -o stat= -p "${process_pid}" 2>/dev/null || true; } | tr -d '[:space:]')"
	if [[ -n "${process_state}" && "${process_state}" != Z* ]]; then
		printf 'TERM-ignoring public Go resolution descendant %s remained after cleanup (state %s)\n' "${process_pid}" "${process_state}" >&2
		exit 1
	fi
done

if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 other/repository >"${workdir}/repository-failure.out" 2>"${workdir}/repository-failure.err"; then
	printf 'expected non-Stave repository rejection\n' >&2
	exit 1
fi
grep -q 'usage:' "${workdir}/repository-failure.err"
