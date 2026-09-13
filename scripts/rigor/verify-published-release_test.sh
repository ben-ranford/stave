#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/rigor/verify-published-release.sh"
workdir="$(mktemp -d "${TMPDIR:-/tmp}/stave-release-probe-test.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT

mkdir -p "${workdir}/bin"
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
*/releases/tags/v1.0.0-rc.2) cat >"${out}" <<JSON
{"tag_name":"v1.0.0-rc.2","assets":[{"name":"CHANGELOG.md","digest":"sha256:$(printf changelog | shasum -a 256 | awk '{print $1}')","browser_download_url":"https://assets/CHANGELOG.md"},{"name":"LICENSE","digest":"sha256:$(printf license | shasum -a 256 | awk '{print $1}')","browser_download_url":"https://assets/LICENSE"},{"name":"report.json","digest":"sha256:$(printf report | shasum -a 256 | awk '{print $1}')","browser_download_url":"https://assets/report.json"}]}
JSON
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
'get github.com/ben-ranford/stave@v1.0.0-rc.2') [[ ! -e "$(dirname "$0")/fail-go-get" ]] || exit 1; printf '\nrequire github.com/ben-ranford/stave v1.0.0-rc.2\n' >>go.mod ;;
'list -m') printf '%s\n' '{"Path":"github.com/ben-ranford/stave","Version":"v1.0.0-rc.2","Sum":"h1:publicsum","Origin":{"Hash":"source-commit"}}' ;;
'run .') printf 'text: public module\n' ;;
*) printf 'unexpected go invocation: %s %s\n' "$1" "$2" >&2; exit 1 ;;
esac
EOF
chmod +x "${workdir}/bin/curl" "${workdir}/bin/go"

PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/report.json"
jq -e '.tag_object_sha == "tag-object" and .source_sha == "source-commit" and .module.sum == "h1:publicsum" and .module.origin_sha == .source_sha and (.assets | length == 3)' "${workdir}/report.json" >/dev/null

if PATH="${workdir}/bin:${PATH}" FAIL_CURL=1 RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/failure.out" 2>"${workdir}/failure.err"; then
	printf 'expected propagation failure\n' >&2
	exit 1
fi
grep -q 'failed after 2 attempts' "${workdir}/failure.err"

touch "${workdir}/bin/fail-go-get"
if PATH="${workdir}/bin:${PATH}" RELEASE_PROBE_ATTEMPTS=2 RELEASE_PROBE_RETRY_SECONDS=0 "${script}" v1.0.0-rc.2 >"${workdir}/module-failure.out" 2>"${workdir}/module-failure.err"; then
	printf 'expected public module propagation failure\n' >&2
	exit 1
fi
grep -q 'failed after 2 attempts while resolving github.com/ben-ranford/stave@v1.0.0-rc.2' "${workdir}/module-failure.err"
