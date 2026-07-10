#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

repos=(
	openlist-lzcapp
	forgejo-lzcapp
	vaultwarden-lzcapp
	moltis-lzcapp
	nowledge-mem-lzcapp
)

for repo in "${repos[@]}"; do
	destination="$WORK/$repo"
	gh repo clone "lazycat-contrib/$repo" "$destination" -- --depth 1
	config="lzc-build.yml"
	if [[ ! -f "$destination/$config" ]]; then
		printf 'SKIP %s: no %s\n' "$repo" "$config"
		continue
	fi
	go run "$ROOT/internal/cmd/validate-project" -root "$destination" -config "$config"
done
