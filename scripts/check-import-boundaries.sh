#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

packages=(
	./archive
	./manifest
	./lpk
	./inspect
	./lint
	./signature
	./build
	./oci
	./image
	./image/dockerarchive
	./auth
	./auth/tokenfile
	./remote
	./workflow
)

forbidden='(google\.golang\.org/grpc|golang\.org/x/crypto/ssh|github\.com/docker|github\.com/lib-x/lzc-toolkit-go/(appstore|remote/(ssh|shellapi)|project|lifecycle|image/(dockerlocal|buildpack)))'

deps="$(go list -deps "${packages[@]}")"
matches="$(printf '%s\n' "$deps" | rg -e "$forbidden" || true)"
if [[ -n "$matches" ]]; then
	printf 'import boundary violation:\n%s\n' "$matches" >&2
	exit 1
fi

project_forbidden='(google\.golang\.org/grpc|golang\.org/x/crypto/ssh|github\.com/docker|github\.com/lib-x/lzc-toolkit-go/(appstore|remote/(ssh|shellapi)|project/rsync|image/(dockerlocal|buildpack)))'
project_deps="$(go list -deps ./project)"
project_matches="$(printf '%s\n' "$project_deps" | rg -e "$project_forbidden" || true)"
if [[ -n "$project_matches" ]]; then
	printf 'project import boundary violation:\n%s\n' "$project_matches" >&2
	exit 1
fi

project_workflow_forbidden='(google\.golang\.org/grpc|golang\.org/x/crypto/ssh|github\.com/docker|github\.com/lib-x/lzc-toolkit-go/(appstore|remote/(ssh|shellapi)|image/(dockerlocal|buildpack)))'
project_workflow_deps="$(go list -deps ./workflow/project)"
project_workflow_matches="$(printf '%s\n' "$project_workflow_deps" | rg -e "$project_workflow_forbidden" || true)"
if [[ -n "$project_workflow_matches" ]]; then
	printf 'project workflow import boundary violation:\n%s\n' "$project_workflow_matches" >&2
	exit 1
fi
