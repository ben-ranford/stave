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
printf '%s\n' "${CHANGELOG_DIGEST}" >"${workdir}/bin/changelog-digest"
printf '%s\n' "${LICENSE_DIGEST}" >"${workdir}/bin/license-digest"
printf '%s\n' "${REPORT_DIGEST}" >"${workdir}/bin/report-digest"
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
fixture_dir="$(dirname "$0")"
if [[ -s "${fixture_dir}/assert-anonymous-curl" ]]; then
	for variable in HTTPS_PROXY https_proxy ALL_PROXY all_proxy CURL_HOME NETRC; do
		[[ -z "${!variable+x}" ]] || exit 22
	done
fi
if [[ -s "${fixture_dir}/fail-curl" ]]; then exit 22; fi
case "${url}" in
	*/releases/tags/*)
		tag_name="${url##*/}"
		if [[ -s "${fixture_dir}/incomplete-metadata" ]] || { [[ -s "${fixture_dir}/delay-metadata" ]] && [[ ! -e "${fixture_dir}/metadata-delayed" ]]; }; then
			touch "${fixture_dir}/metadata-delayed"
		printf '%s' '{"tag_name":"v1.0.0-rc.2","assets":[]}' >"${out}"
	else
		changelog_digest="$(<"${fixture_dir}/changelog-digest")"
		license_digest="$(<"${fixture_dir}/license-digest")"
		report_digest="$(<"${fixture_dir}/report-digest")"
		cat >"${out}" <<JSON
	{"tag_name":"${tag_name}","assets":[{"name":"CHANGELOG.md","digest":"sha256:${changelog_digest}","browser_download_url":"https://assets/CHANGELOG.md"},{"name":"LICENSE","digest":"sha256:${license_digest}","browser_download_url":"https://assets/LICENSE"},{"name":"report.json","digest":"sha256:${report_digest}","browser_download_url":"https://assets/report.json"}]}
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
	query="${4:-}"
	if [[ "${query}" == *@source-commit ]]; then
		touch "${bin_dir}/sha-query-called"
		exit 1
	fi
	selected_query=false
	if [[ "${query}" == "github.com/ben-ranford/stave@${module_version}" && "${GOMODCACHE:-}" == */provenance-modcache ]]; then
		selected_query=true
		touch "${bin_dir}/provenance-cache-used"
	fi
	module_path=github.com/ben-ranford/stave
	if [[ "${selected_query}" == false && "${query}" == *@* ]] && [[ -s "${bin_dir}/resolution-version-override" ]]; then module_version="$(<"${bin_dir}/resolution-version-override")"; fi
	if [[ "${query}" != *@* ]] && [[ -s "${bin_dir}/module-version-override" ]]; then module_version="$(<"${bin_dir}/module-version-override")"; fi
	if [[ "${selected_query}" == true && -s "${bin_dir}/selected-version-override" ]]; then module_version="$(<"${bin_dir}/selected-version-override")"; fi
	if [[ "${selected_query}" == true && -s "${bin_dir}/selected-path-override" ]]; then module_path="$(<"${bin_dir}/selected-path-override")"; fi
	if [[ "${selected_query}" == true && -s "${bin_dir}/fail-first-go-provenance" && ! -e "${bin_dir}/go-provenance-failed-once" ]]; then
		touch "${bin_dir}/go-provenance-failed-once"
		printf '{"Path":"partial"}\n'
		exit 1
	fi
	if [[ "${selected_query}" == true && "$(<"${bin_dir}/slow-go-provenance")" == 1 ]]; then
		printf '%s\n' "$$" >"${bin_dir}/slow-go-provenance-pid"
		sleep 30
	fi
	origin_sha=source-commit
	if [[ -s "${bin_dir}/origin-override" ]]; then origin_sha="$(<"${bin_dir}/origin-override")"; fi
	if [[ "${selected_query}" == true && -s "${bin_dir}/selected-origin-override" ]]; then origin_sha="$(<"${bin_dir}/selected-origin-override")"; fi
	if [[ "${selected_query}" == true && -s "${bin_dir}/omit-selected-origin" ]]; then
		printf '{"Path":"%s","Version":"%s","Sum":"h1:publicsum"}\n' "${module_path}" "${module_version}"
		exit 0
	fi
	printf '{"Path":"%s","Version":"%s","Sum":"h1:publicsum","Origin":{"Hash":"%s"}}\n' "${module_path}" "${module_version}" "${origin_sha}"
	;;
	'mod download')
		bin_dir="$(dirname "$0")"
		module_version="${4#github.com/ben-ranford/stave@}"
		if [[ -s "${bin_dir}/fail-first-go-download" && ! -e "${bin_dir}/go-download-failed-once" ]]; then
			touch "${bin_dir}/go-download-failed-once"
			printf '{"Path":"partial"}\n'
			exit 1
		fi
		if [[ "$(<"${bin_dir}/slow-go-download")" == 1 ]]; then
			printf '%s\n' "$$" >"${bin_dir}/slow-go-download-pid"
			sleep 30
		fi
		origin_sha=source-commit
		if [[ -s "${bin_dir}/origin-override" ]]; then origin_sha="$(<"${bin_dir}/origin-override")"; fi
		if [[ -s "${bin_dir}/omit-origin" ]]; then
			printf '{"Path":"github.com/ben-ranford/stave","Version":"%s","Sum":"h1:publicsum"}\n' "${module_version}"
		else
			printf '{"Path":"github.com/ben-ranford/stave","Version":"%s","Sum":"h1:publicsum","Origin":{"Hash":"%s"}}\n' "${module_version}" "${origin_sha}"
		fi
	;;
'run .')
	bin_dir="$(dirname "$0")"
	consumer_output='text: public module'
	if [[ -s "${bin_dir}/consumer-output-override" ]]; then consumer_output="$(<"${bin_dir}/consumer-output-override")"; fi
	printf '%s\n' "${consumer_output}"
	;;
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
: >"${workdir}/bin/consumer-output-override"
: >"${workdir}/bin/fail-curl"
: >"${workdir}/bin/incomplete-metadata"
: >"${workdir}/bin/delay-metadata"
: >"${workdir}/bin/assert-anonymous-curl"
: >"${workdir}/bin/omit-origin"
: >"${workdir}/bin/slow-go-download"
: >"${workdir}/bin/fail-first-go-download"
: >"${workdir}/bin/selected-origin-override"
: >"${workdir}/bin/selected-version-override"
: >"${workdir}/bin/selected-path-override"
: >"${workdir}/bin/omit-selected-origin"
: >"${workdir}/bin/slow-go-provenance"
: >"${workdir}/bin/fail-first-go-provenance"

PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/report.json"
jq -e '.tag_object_sha == "tag-object" and .source_sha == "source-commit" and .module.sum == "h1:publicsum" and .module.selected_version == "v1.0.0-rc.2" and .module.requested_version == .module.selected_version and .module.origin_sha == .source_sha and (.assets | length == 3)' "${workdir}/report.json" >/dev/null

printf '1\n' >"${workdir}/bin/fail-first-go-download"
PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/download-retry.json"
jq -e '.module.path == "github.com/ben-ranford/stave" and .module.sum == "h1:publicsum"' "${workdir}/download-retry.json" >/dev/null
: >"${workdir}/bin/fail-first-go-download"

printf '1\n' >"${workdir}/bin/assert-anonymous-curl"
PATH="${workdir}/bin:${PATH}" HTTPS_PROXY='http://operator:secret@proxy.invalid' ALL_PROXY='http://operator:secret@proxy.invalid' CURL_HOME=/private/curl-home "${script}" v1.0.0-rc.2 >"${workdir}/anonymous-curl.json"
: >"${workdir}/bin/assert-anonymous-curl"

printf 'text: unexpected module\n' >"${workdir}/bin/consumer-output-override"
if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 >"${workdir}/consumer-output-failure.out" 2>"${workdir}/consumer-output-failure.err"; then
	printf 'expected consumer output mismatch\n' >&2
	exit 1
fi
grep -Fq 'consumer output mismatch: got text:\ unexpected\ module, want text:\ public\ module' "${workdir}/consumer-output-failure.err"
: >"${workdir}/bin/consumer-output-override"

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

printf '1\n' >"${workdir}/bin/omit-origin"
PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 >"${workdir}/missing-origin.json"
jq -e '.module.origin_sha == .source_sha and .module.origin_source == "selected-version"' "${workdir}/missing-origin.json" >/dev/null
[[ -e "${workdir}/bin/provenance-cache-used" ]] || { printf 'probe must use a separate provenance module cache\n' >&2; exit 1; }
[[ ! -e "${workdir}/bin/sha-query-called" ]] || { printf 'probe must not query source SHA through the public proxy\n' >&2; exit 1; }

printf 'wrong-source\n' >"${workdir}/bin/selected-origin-override"
if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 >"${workdir}/selected-origin-mismatch.out" 2>"${workdir}/selected-origin-mismatch.err"; then
	printf 'expected selected module provenance mismatch\n' >&2
	exit 1
fi
grep -q 'selected module provenance mismatch' "${workdir}/selected-origin-mismatch.err"
: >"${workdir}/bin/selected-origin-override"

printf '1\n' >"${workdir}/bin/omit-selected-origin"
if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 >"${workdir}/selected-origin-missing.out" 2>"${workdir}/selected-origin-missing.err"; then
	printf 'expected selected module provenance absence failure\n' >&2
	exit 1
fi
grep -q 'selected module provenance mismatch' "${workdir}/selected-origin-missing.err"
: >"${workdir}/bin/omit-selected-origin"

printf 'v9.9.9\n' >"${workdir}/bin/selected-version-override"
if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 >"${workdir}/selected-version-mismatch.out" 2>"${workdir}/selected-version-mismatch.err"; then
	printf 'expected selected module version mismatch\n' >&2
	exit 1
fi
grep -q 'selected module provenance mismatch' "${workdir}/selected-version-mismatch.err"
: >"${workdir}/bin/selected-version-override"

printf 'example.com/wrong\n' >"${workdir}/bin/selected-path-override"
if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 >"${workdir}/selected-path-mismatch.out" 2>"${workdir}/selected-path-mismatch.err"; then
	printf 'expected selected module path mismatch\n' >&2
	exit 1
fi
grep -q 'selected module provenance mismatch' "${workdir}/selected-path-mismatch.err"
: >"${workdir}/bin/selected-path-override"

printf '1\n' >"${workdir}/bin/fail-first-go-provenance"
PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/provenance-retry.json"
[[ -e "${workdir}/bin/go-provenance-failed-once" ]] || { printf 'expected selected module provenance retry\n' >&2; exit 1; }
jq -e '.module.origin_source == "selected-version" and .module.origin_sha == .source_sha' "${workdir}/provenance-retry.json" >/dev/null
: >"${workdir}/bin/fail-first-go-provenance"
: >"${workdir}/bin/omit-origin"

printf '1\n' >"${workdir}/bin/delay-metadata"
PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/delayed-metadata.json"
jq -e '(.assets | length == 3)' "${workdir}/delayed-metadata.json" >/dev/null
: >"${workdir}/bin/delay-metadata"

printf '1\n' >"${workdir}/bin/incomplete-metadata"
if PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/metadata-failure.out" 2>"${workdir}/metadata-failure.err"; then
	printf 'expected incomplete release metadata failure\n' >&2
	exit 1
fi
grep -q 'release metadata incomplete after 2 attempts; missing digest or download URL for: CHANGELOG.md LICENSE report.json' "${workdir}/metadata-failure.err"
: >"${workdir}/bin/incomplete-metadata"

printf '1\n' >"${workdir}/bin/fail-curl"
if PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/failure.out" 2>"${workdir}/failure.err"; then
	printf 'expected propagation failure\n' >&2
	exit 1
fi
grep -q 'failed after 2 attempts' "${workdir}/failure.err"
: >"${workdir}/bin/fail-curl"

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

printf '1\n' >"${workdir}/bin/slow-go-download"
SECONDS=0
if PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=1 RELEASE_PROBE_RETRY_SECONDS=0 RELEASE_PROBE_GO_TIMEOUT_SECONDS=1 RELEASE_PROBE_TERMINATE_GRACE_SECONDS=1 "${script}" v1.0.0-rc.2 >"${workdir}/download-timeout-failure.out" 2>"${workdir}/download-timeout-failure.err"; then
	printf 'expected public module download timeout failure\n' >&2
	exit 1
fi
((SECONDS < 5)) || { printf 'stalled public module download exceeded its bounded timeout\n' >&2; exit 1; }
grep -q 'release probe timed out after 1s while downloading github.com/ben-ranford/stave@v1.0.0-rc.2' "${workdir}/download-timeout-failure.err"
download_pid="$(<"${workdir}/bin/slow-go-download-pid")"
if kill -0 "${download_pid}" 2>/dev/null; then
	printf 'stalled public Go download remained after timeout cleanup\n' >&2
	exit 1
fi
: >"${workdir}/bin/slow-go-download"

printf '1\n' >"${workdir}/bin/omit-origin"
printf '1\n' >"${workdir}/bin/slow-go-provenance"
PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=1 RELEASE_PROBE_RETRY_SECONDS=0 RELEASE_PROBE_GO_TIMEOUT_SECONDS=1 RELEASE_PROBE_TERMINATE_GRACE_SECONDS=1 "${script}" v1.0.0-rc.2 >"${workdir}/provenance-timeout-failure.out" 2>"${workdir}/provenance-timeout-failure.err" &
probe_pid=$!
for _ in {1..5}; do
	if [[ -s "${workdir}/bin/slow-go-provenance-pid" ]]; then
		break
	fi
	if ! kill -0 "${probe_pid}" 2>/dev/null; then
		break
	fi
	sleep 1
done
[[ -s "${workdir}/bin/slow-go-provenance-pid" ]] || {
	wait "${probe_pid}" 2>/dev/null || true
	printf 'selected module provenance lookup did not reach the stalled operation\n' >&2
	exit 1
}
SECONDS=0
if wait "${probe_pid}"; then
	printf 'expected selected module provenance timeout failure\n' >&2
	exit 1
fi
((SECONDS < 5)) || { printf 'selected module provenance exceeded its bounded timeout\n' >&2; exit 1; }
grep -q 'release probe timed out after 1s while reading selected github.com/ben-ranford/stave@v1.0.0-rc.2 provenance' "${workdir}/provenance-timeout-failure.err"
provenance_pid="$(<"${workdir}/bin/slow-go-provenance-pid")"
if kill -0 "${provenance_pid}" 2>/dev/null; then
	printf 'stalled selected module provenance lookup remained after timeout cleanup\n' >&2
	exit 1
fi
: >"${workdir}/bin/slow-go-provenance"
: >"${workdir}/bin/omit-origin"

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
