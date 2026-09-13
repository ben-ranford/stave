#!/usr/bin/env bash
set -euo pipefail

# Verify a release exactly as an unauthenticated downstream Go consumer would.
# This script performs only public reads; it never creates, moves, or labels a release.

usage() {
	printf 'usage: %s <tag>\n' "${0##*/}" >&2
	exit 2
}

tag="${1:-}"
[[ $# -eq 1 && -n "${tag}" ]] || usage
repository="ben-ranford/stave"

attempts="${RELEASE_PROBE_ATTEMPTS:-3}"
retry_seconds="${RELEASE_PROBE_RETRY_SECONDS:-5}"
connect_timeout_seconds="${RELEASE_PROBE_CONNECT_TIMEOUT_SECONDS:-5}"
request_timeout_seconds="${RELEASE_PROBE_REQUEST_TIMEOUT_SECONDS:-15}"
go_timeout_seconds="${RELEASE_PROBE_GO_TIMEOUT_SECONDS:-60}"
terminate_grace_seconds="${RELEASE_PROBE_TERMINATE_GRACE_SECONDS:-2}"
[[ "${attempts}" =~ ^[1-9][0-9]*$ ]] || { printf 'RELEASE_PROBE_ATTEMPTS must be a positive integer\n' >&2; exit 2; }
[[ "${retry_seconds}" =~ ^[0-9]+$ ]] || { printf 'RELEASE_PROBE_RETRY_SECONDS must be a non-negative integer\n' >&2; exit 2; }
[[ "${connect_timeout_seconds}" =~ ^[1-9][0-9]*$ ]] || { printf 'RELEASE_PROBE_CONNECT_TIMEOUT_SECONDS must be a positive integer\n' >&2; exit 2; }
[[ "${request_timeout_seconds}" =~ ^[1-9][0-9]*$ ]] || { printf 'RELEASE_PROBE_REQUEST_TIMEOUT_SECONDS must be a positive integer\n' >&2; exit 2; }
[[ "${go_timeout_seconds}" =~ ^[1-9][0-9]*$ ]] || { printf 'RELEASE_PROBE_GO_TIMEOUT_SECONDS must be a positive integer\n' >&2; exit 2; }
[[ "${terminate_grace_seconds}" =~ ^[1-9][0-9]*$ ]] || { printf 'RELEASE_PROBE_TERMINATE_GRACE_SECONDS must be a positive integer\n' >&2; exit 2; }

for command in curl go jq mktemp; do
	command -v "${command}" >/dev/null 2>&1 || { printf 'required command not found: %s\n' "${command}" >&2; exit 2; }
done

workdir="$(mktemp -d "${TMPDIR:-/tmp}/stave-release-probe.XXXXXX")"
trap 'chmod -R u+w "${workdir}" 2>/dev/null || true; rm -rf "${workdir}"' EXIT

fetch() {
	local url="$1" output="$2" attempt=1
	while ! curl -q --connect-timeout "${connect_timeout_seconds}" --max-time "${request_timeout_seconds}" --fail --silent --show-error --location --output "${output}" "${url}"; do
		if (( attempt >= attempts )); then
			printf 'release probe failed after %s attempts while fetching %s\n' "${attempts}" "${url}" >&2
			return 1
		fi
		printf 'release probe attempt %s/%s failed while fetching %s; retrying in %ss\n' "${attempt}" "${attempts}" "${url}" "${retry_seconds}" >&2
		sleep "${retry_seconds}"
		((attempt += 1))
	done
}

retry_command() {
	local description="$1" attempt=1
	shift
	while ! "$@"; do
		if (( attempt >= attempts )); then
			printf 'release probe failed after %s attempts while %s\n' "${attempts}" "${description}" >&2
			return 1
		fi
		printf 'release probe attempt %s/%s failed while %s; retrying in %ss\n' "${attempt}" "${attempts}" "${description}" "${retry_seconds}" >&2
		sleep "${retry_seconds}"
		((attempt += 1))
	done
}

descendant_pids() {
	local parent_pid="$1" child_pid
	while IFS= read -r child_pid; do
		[[ -n "${child_pid}" ]] || continue
		descendant_pids "${child_pid}"
		printf '%s\n' "${child_pid}"
	done < <(ps -eo pid=,ppid= | awk -v parent_pid="${parent_pid}" '$2 == parent_pid {print $1}')
}

terminate_processes() {
	local signal="$1"
	shift
	local process_pid
	for process_pid in "$@"; do
		kill "-${signal}" "${process_pid}" 2>/dev/null || true
	done
}

process_is_running() {
	local process_pid="$1" process_state
	process_state="$(ps -o stat= -p "${process_pid}" 2>/dev/null | tr -d '[:space:]')"
	[[ -n "${process_state}" && "${process_state}" != Z* ]]
}

run_with_timeout() {
	local description="$1"
	shift
	"$@" &
	local command_pid=$!
	local timeout_deadline=$((SECONDS + go_timeout_seconds))
	while process_is_running "${command_pid}"; do
		if (( SECONDS >= timeout_deadline )); then
			break
		fi
		sleep 1
	done
	if ! process_is_running "${command_pid}"; then
		if wait "${command_pid}"; then
			return 0
		else
			return $?
		fi
	fi

	printf 'release probe timed out after %ss while %s\n' "${go_timeout_seconds}" "${description}" >&2
	local -a process_pids=()
	local process_pid
	while IFS= read -r process_pid; do
		[[ -n "${process_pid}" ]] && process_pids+=("${process_pid}")
	done < <(descendant_pids "${command_pid}"; printf '%s\n' "${command_pid}")
	terminate_processes TERM "${process_pids[@]}"
	local grace_deadline=$((SECONDS + terminate_grace_seconds))
	while (( SECONDS < grace_deadline )); do
		local process_alive=false
		for process_pid in "${process_pids[@]}"; do
			if process_is_running "${process_pid}"; then
				process_alive=true
				break
			fi
		done
		"${process_alive}" || break
		sleep 1
	done
	terminate_processes KILL "${process_pids[@]}"
	wait "${command_pid}" 2>/dev/null || true
	return 124
}

api_base="https://api.github.com/repos/${repository}"
release_json="${workdir}/release.json"
tag_ref_json="${workdir}/tag-ref.json"

wait_for_release_metadata() {
	local attempt=1 expected_digest asset_url asset_name
	local -a incomplete_assets=()
	while :; do
		fetch "${api_base}/releases/tags/${tag}" "${release_json}"
		incomplete_assets=()
		for asset_name in CHANGELOG.md LICENSE report.json; do
			expected_digest="$(jq -er --arg name "${asset_name}" '.assets[] | select(.name == $name) | .digest' "${release_json}" 2>/dev/null || true)"
			asset_url="$(jq -er --arg name "${asset_name}" '.assets[] | select(.name == $name) | .browser_download_url' "${release_json}" 2>/dev/null || true)"
			if [[ ! "${expected_digest}" =~ ^sha256:[0-9a-f]{64}$ || -z "${asset_url}" ]]; then
				incomplete_assets+=("${asset_name}")
			fi
		done
		if ((${#incomplete_assets[@]} == 0)); then
			return 0
		fi
		if (( attempt >= attempts )); then
			printf 'release metadata incomplete after %s attempts; missing digest or download URL for: %s\n' "${attempts}" "${incomplete_assets[*]}" >&2
			return 1
		fi
		printf 'release metadata incomplete for %s; retrying in %ss (missing digest or download URL for: %s)\n' "${tag}" "${retry_seconds}" "${incomplete_assets[*]}" >&2
		sleep "${retry_seconds}"
		((attempt += 1))
	done
}

wait_for_release_metadata
fetch "${api_base}/git/ref/tags/${tag}" "${tag_ref_json}"

release_tag="$(jq -er '.tag_name' "${release_json}")"
[[ "${release_tag}" == "${tag}" ]] || { printf 'release tag mismatch: requested %s, got %s\n' "${tag}" "${release_tag}" >&2; exit 1; }
tag_object_sha="$(jq -er '.object.sha' "${tag_ref_json}")"
tag_object_type="$(jq -er '.object.type' "${tag_ref_json}")"
[[ "${tag_object_type}" == tag ]] || { printf 'release tag %s must be annotated, got %s\n' "${tag}" "${tag_object_type}" >&2; exit 1; }
tag_object_json="${workdir}/tag-object.json"
fetch "${api_base}/git/tags/${tag_object_sha}" "${tag_object_json}"
source_sha="$(jq -er '.object.sha' "${tag_object_json}")"
source_type="$(jq -er '.object.type' "${tag_object_json}")"
[[ "${source_type}" == commit ]] || { printf 'annotated tag %s does not point to a commit\n' "${tag}" >&2; exit 1; }

consumer="${workdir}/consumer"
mkdir "${consumer}"
cat >"${consumer}/main.go" <<'EOF'
package main

import (
	"fmt"

	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func main() {
	node, err := primitive.Text(primitive.Options{Namespace: "release", View: "probe", Entity: "consumer", Name: "Release probe"}, "public module")
	if err != nil {
		panic(err)
	}
	tree, err := semantic.NewTree(1, node)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s: %s\n", tree.Root().Role(), tree.Root().Value().Text)
}
EOF

# Keep this environment hermetic: no user Go configuration, workspace, private
# proxy exclusions, credentials, or repository-local replace directives can leak in.
go_environment=(
	"PATH=${PATH}" "HOME=${workdir}/home" "GOWORK=off"
	"GOPROXY=https://proxy.golang.org" "GOSUMDB=sum.golang.org"
	"GOPRIVATE=" "GONOPROXY=" "GONOSUMDB="
	"GOMODCACHE=${workdir}/modcache" "GOCACHE=${workdir}/gocache"
)
mkdir "${workdir}/home"
(
	cd "${consumer}"
	env -i "${go_environment[@]}" go mod init example.com/stave-release-probe >/dev/null
	retry_command "resolving github.com/ben-ranford/stave@${tag} through the public Go proxy and checksum database" run_with_timeout "resolving github.com/ben-ranford/stave@${tag}" env -i "${go_environment[@]}" go get "github.com/ben-ranford/stave@${tag}" >/dev/null
	! grep -qE '^replace[[:space:]]' go.mod
	module_json="$(env -i "${go_environment[@]}" go list -m -json "github.com/ben-ranford/stave@${tag}")"
	printf '%s\n' "${module_json}" >"${workdir}/module.json"
	env -i "${go_environment[@]}" go run . >"${workdir}/consumer-output.txt"
)
module_path="$(jq -er '.Path' "${workdir}/module.json")"
module_version="$(jq -er '.Version' "${workdir}/module.json")"
module_sum="$(jq -er '.Sum' "${workdir}/module.json")"
module_origin_sha="$(jq -er '.Origin.Hash' "${workdir}/module.json")"
[[ "${module_path}" == "github.com/ben-ranford/stave" && "${module_version}" == "${tag}" && -n "${module_sum}" && "${module_origin_sha}" == "${source_sha}" ]]
grep -qx 'text: public module' "${workdir}/consumer-output.txt"

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

assets_json='[]'
for asset_name in CHANGELOG.md LICENSE report.json; do
	expected_digest="$(jq -er --arg name "${asset_name}" '.assets[] | select(.name == $name) | .digest' "${release_json}")"
	[[ "${expected_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'asset %s has no SHA-256 digest\n' "${asset_name}" >&2; exit 1; }
	asset_url="$(jq -er --arg name "${asset_name}" '.assets[] | select(.name == $name) | .browser_download_url' "${release_json}")"
	asset_path="${workdir}/${asset_name}"
	fetch "${asset_url}" "${asset_path}"
	actual_digest="sha256:$(sha256 "${asset_path}")"
	[[ "${actual_digest}" == "${expected_digest}" ]] || { printf 'digest mismatch for %s: got %s, want %s\n' "${asset_name}" "${actual_digest}" "${expected_digest}" >&2; exit 1; }
	assets_json="$(jq -cn --argjson assets "${assets_json}" --arg name "${asset_name}" --arg digest "${actual_digest}" '$assets + [{name: $name, digest: $digest}]')"
done

jq -n \
	--arg repository "${repository}" --arg tag "${tag}" --arg tag_object_sha "${tag_object_sha}" \
	--arg source_sha "${source_sha}" --arg module_sum "${module_sum}" --arg module_origin_sha "${module_origin_sha}" --arg consumer_output "$(<"${workdir}/consumer-output.txt")" \
	--argjson assets "${assets_json}" \
	'{repository: $repository, tag: $tag, tag_object_sha: $tag_object_sha, source_sha: $source_sha, module: {path: "github.com/ben-ranford/stave", version: $tag, sum: $module_sum, origin_sha: $module_origin_sha, proxy: "https://proxy.golang.org", sumdb: "sum.golang.org"}, consumer_output: $consumer_output, assets: $assets}'
