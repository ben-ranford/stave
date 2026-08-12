#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -lt 1 ]]; then
	printf 'usage: %s <pre-commit|pre-push> [--dry-run]\n' "$0" >&2
	exit 1
fi

hook_name="$1"
dry_run="${2:-}"

case "${hook_name}" in
	pre-commit)
		target="fast"
		;;
	pre-push)
		target="verify"
		;;
	*)
		printf 'unknown hook %s\n' "${hook_name}" >&2
		exit 1
		;;
esac

if [[ "${dry_run}" == "--dry-run" || "${RIGOR_HOOK_DRY_RUN:-0}" == "1" ]]; then
	printf 'hook %s would run: make %s\n' "${hook_name}" "${target}"
	exec make -n "${target}"
fi

exec make "${target}"
