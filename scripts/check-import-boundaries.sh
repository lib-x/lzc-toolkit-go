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
)

forbidden='(google\.golang\.org/grpc|golang\.org/x/crypto/ssh|github\.com/docker|github\.com/lib-x/lpk-go/(appstore|remote|project|lifecycle|image/dockerlocal))'

deps="$(go list -deps "${packages[@]}")"
matches="$(printf '%s\n' "$deps" | grep -E "$forbidden" || true)"
if [[ -n "$matches" ]]; then
	printf 'import boundary violation:\n%s\n' "$matches" >&2
	exit 1
fi
