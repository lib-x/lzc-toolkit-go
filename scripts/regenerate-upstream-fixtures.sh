#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/testdata/upstream/2.0.9"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

NPM_PROJECT="$TMP/npm"
V1_DIR="$TMP/v1"
V2_DIR="$TMP/v2"
RESOURCE_DIR="$TMP/resource"
KEY_DIR="$TMP/keys"

mkdir -p "$NPM_PROJECT" "$V1_DIR" "$V2_DIR" "$RESOURCE_DIR" "$KEY_DIR" "$OUT"

cd "$NPM_PROJECT"
npm init -y >/dev/null
npm install --save-exact @lazycatcloud/lzc-cli@2.0.9 >/dev/null

cat >"$V1_DIR/lzc-build.yml" <<'YAML'
manifest: lzc-manifest.yml
YAML

cat >"$V1_DIR/lzc-manifest.yml" <<'YAML'
package: cloud.lazycat.test.v1
version: 1.0.0
name: Upstream V1
application:
  subdomain: upstream-v1
YAML

cat >"$V2_DIR/lzc-build.yml" <<'YAML'
manifest: lzc-manifest.yml
YAML

cat >"$V2_DIR/package.yml" <<'YAML'
package: cloud.lazycat.test.v2
version: 2.0.0
name: Upstream V2
YAML

cat >"$V2_DIR/lzc-manifest.yml" <<'YAML'
application:
  subdomain: upstream-v2
YAML

cat >"$RESOURCE_DIR/lzc-build.yml" <<'YAML'
resource_exports:
  - kind: skill
    source: resources/skill
YAML

cat >"$RESOURCE_DIR/package.yml" <<'YAML'
package: cloud.lazycat.test.resources
version: 1.0.0
name: Upstream Resources
YAML

mkdir -p "$RESOURCE_DIR/resources/skill/demo"
cat >"$RESOURCE_DIR/resources/skill/demo/manifest.json" <<'JSON'
{"name":"demo","description":"upstream fixture"}
JSON

rm -f "$OUT/v1-simple.lpk" "$OUT/v2-simple.lpk" "$OUT/resource-only.lpk" "$OUT/signed-v2.lpk"

npx --no-install lzc-cli project build "$V1_DIR" -f lzc-build.yml -o "$OUT/v1-simple.lpk"
npx --no-install lzc-cli project build "$V2_DIR" -f lzc-build.yml -o "$OUT/v2-simple.lpk"
npx --no-install lzc-cli project build "$RESOURCE_DIR" -f lzc-build.yml -o "$OUT/resource-only.lpk"
node - "$KEY_DIR" <<'NODE'
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const keyDir = process.argv[2];
fs.mkdirSync(keyDir, { recursive: true });
const { privateKey, publicKey } = crypto.generateKeyPairSync('ed25519');
const privatePath = path.join(keyDir, 'upstream.ed25519.private.pem');
const publicPath = path.join(keyDir, 'upstream.ed25519.public.pem');
fs.writeFileSync(privatePath, privateKey.export({ type: 'pkcs8', format: 'pem' }), { mode: 0o600 });
fs.writeFileSync(publicPath, publicKey.export({ type: 'spki', format: 'pem' }));
NODE
npx --no-install lzc-cli lpk sign "$OUT/v2-simple.lpk" \
	--private-key "$KEY_DIR/upstream.ed25519.private.pem" \
	--public-key "$KEY_DIR/upstream.ed25519.public.pem" \
	--key-id upstream \
	--output "$OUT/signed-v2.lpk"

cd "$ROOT"
{
	cat <<'MARKDOWN'
# lzc-cli 2.0.9 upstream fixtures

Generated from npm package `@lazycatcloud/lzc-cli@2.0.9`.

- integrity: `sha512-L+DUKBD5HrFctnqZ4a8vofXY7f5+4ukpfw4rSnNbeE9s48lsLOr3vvbaWZCDSR6xkivRYTovQMWKqcli6s8mUQ==`
- shasum: `88a3847bbd1c0c2e709cbc7a96fae52f9f832a85`

Regenerate with:

```bash
bash scripts/regenerate-upstream-fixtures.sh
```

The script builds:

- `v1-simple.lpk`
- `v2-simple.lpk`
- `resource-only.lpk`
- `signed-v2.lpk`

## sha256

MARKDOWN
	sha256sum "$OUT/v1-simple.lpk" "$OUT/v2-simple.lpk" "$OUT/resource-only.lpk" "$OUT/signed-v2.lpk" |
		sed "s#  $OUT/#  #"
} >"$OUT/README.md"

sha256sum "$OUT/v1-simple.lpk" "$OUT/v2-simple.lpk" "$OUT/resource-only.lpk" "$OUT/signed-v2.lpk"
