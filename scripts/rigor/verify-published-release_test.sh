#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/rigor/verify-published-release.sh"
workdir="$(mktemp -d "${TMPDIR:-/tmp}/stave-release-probe-test.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT

mkdir -p "${workdir}/bin"
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		printf '%s' "$1" | sha256sum | awk '{print $1}'
	else
		printf '%s' "$1" | shasum -a 256 | awk '{print $1}'
	fi
}
changelog_digest="$(sha256 changelog)"
license_digest="$(sha256 license)"
report_digest="$(sha256 report)"
export changelog_digest license_digest report_digest
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
*/releases/tags/v1.0.0-rc.2)
	if [[ "${INCOMPLETE_METADATA:-}" == 1 ]] || { [[ "${DELAY_METADATA:-}" == 1 ]] && [[ ! -e "$(dirname "$0")/metadata-delayed" ]]; }; then
		touch "$(dirname "$0")/metadata-delayed"
		printf '%s' '{"tag_name":"v1.0.0-rc.2","assets":[]}' >"${out}"
	else
		cat >"${out}" <<JSON
{"tag_name":"v1.0.0-rc.2","assets":[{"name":"CHANGELOG.md","digest":"sha256:${changelog_digest}","browser_download_url":"https://assets/CHANGELOG.md"},{"name":"LICENSE","digest":"sha256:${license_digest}","browser_download_url":"https://assets/LICENSE"},{"name":"report.json","digest":"sha256:${report_digest}","browser_download_url":"https://assets/report.json"}]}
JSON
	fi
	;;
*/git/ref/tags/v1.0.0-rc.2) printf '%s' '{"object":{"sha":"tag-object","type":"tag"}}' >"${out}" ;;
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
'get github.com/ben-ranford/stave@v1.0.0-rc.2')
	bin_dir="$(dirname "$0")"
	[[ "$(<"${bin_dir}/fail-go-get")" != 1 ]] || exit 1
	if [[ "$(<"${bin_dir}/term-ignore-go-get")" == 1 ]]; then
		printf '%s\n' "$$" >"${bin_dir}/term-ignore-parent-pid"
		trap '' TERM
		while :; do sleep 30 & child_pid=$!; printf '%s\n' "${child_pid}" >"${bin_dir}/term-ignore-child-pid"; wait "${child_pid}" || true; done
	fi
	if [[ "$(<"${bin_dir}/slow-go-get")" == 1 ]]; then
		printf '%s\n' "$$" >"${bin_dir}/slow-go-get-pid"
		sleep 30
	fi
	if [[ "$(<"${bin_dir}/record-fast-go-get")" == 1 ]]; then printf '%s\n' "$$" >"${bin_dir}/fast-go-get-pid"; fi
	printf '\nrequire github.com/ben-ranford/stave v1.0.0-rc.2\n' >>go.mod
	;;
'list -m') printf '%s\n' '{"Path":"github.com/ben-ranford/stave","Version":"v1.0.0-rc.2","Sum":"h1:publicsum","Origin":{"Hash":"source-commit"}}' ;;
'run .') printf 'text: public module\n' ;;
*) printf 'unexpected go invocation: %s %s\n' "$1" "$2" >&2; exit 1 ;;
esac
EOF
chmod +x "${workdir}/bin/curl" "${workdir}/bin/go"
: >"${workdir}/bin/fail-go-get"
: >"${workdir}/bin/slow-go-get"
: >"${workdir}/bin/term-ignore-go-get"
: >"${workdir}/bin/record-fast-go-get"

PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/report.json"
jq -e '.tag_object_sha == "tag-object" and .source_sha == "source-commit" and .module.sum == "h1:publicsum" and .module.origin_sha == .source_sha and (.assets | length == 3)' "${workdir}/report.json" >/dev/null

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
for process_pid in "$(<"${workdir}/bin/term-ignore-parent-pid")" "$(<"${workdir}/bin/term-ignore-child-pid")"; do
	if kill -0 "${process_pid}" 2>/dev/null; then
		printf 'TERM-ignoring public Go resolution descendant %s remained after cleanup\n' "${process_pid}" >&2
		exit 1
	fi
done

if PATH="${workdir}/bin:${PATH}" "${script}" v1.0.0-rc.2 other/repository >"${workdir}/repository-failure.out" 2>"${workdir}/repository-failure.err"; then
	printf 'expected non-Stave repository rejection\n' >&2
	exit 1
fi
grep -q 'usage:' "${workdir}/repository-failure.err"
