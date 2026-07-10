#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v docker >/dev/null 2>&1; then
	printf 'SKIP: docker is not installed\n'
	exit 0
fi
if ! docker buildx version >/dev/null 2>&1; then
	printf 'SKIP: docker buildx is unavailable\n'
	exit 0
fi
if ! docker info >/dev/null 2>&1; then
	printf 'SKIP: docker daemon is unavailable\n'
	exit 0
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/content"
cat >"$WORK/Dockerfile" <<'EOF'
FROM scratch
COPY hello.txt /hello.txt
EOF
printf 'hello from lzc-toolkit-go\n' >"$WORK/hello.txt"
printf 'content\n' >"$WORK/content/data.txt"
cat >"$WORK/lzc-build.yml" <<'EOF'
manifest: lzc-manifest.yml
contentdir: content
images:
  app:
    builder: local
    dockerfile: Dockerfile
    context: .
EOF
cat >"$WORK/lzc-manifest.yml" <<'EOF'
application:
  subdomain: local-image-validation
  image: embed:app
EOF
cat >"$WORK/package.yml" <<'EOF'
package: cloud.lazycat.apps.local-image-validation
version: 1.0.0
name: Local Image Validation
EOF

go run "$ROOT/internal/cmd/validate-project" -root "$WORK" -local-images
