#!/usr/bin/env bash
# Assemble the UNION module: the real sibling assets overlaid on this unit's skeleton, so cmd/scp is compiled against the
# packages other units actually wrote (design 16 section 12.5). Usage: union/assemble.sh <empty-target-dir>
#
#   cmd/scp, cmd/scp/internal/bridge   this unit            (16-cmd-build-release.assets)
#   scm freshness job routes reactor   01 kernel            (01-kernel.assets)
#   store (core) readmodel             03 storage           (03-readmodel-store.assets)
#   refresh                            04 engine            (04-refresh-webhook.assets)
#   webhook                            04                   (needs 04's httpapi and ghverify copies)
#   platform/*                         02 platform          (02-platform.assets)
#   catalog                            08 catalogue         (08-catalog.assets/mod)
#   ops                                15 live/ops/overview (15-live-ops-overview.assets), with 15's httpapi and logx
#   github                             STAND-IN written from 05's document (05 ships no code assets)
#
# What it proves is listed in design 16 section 12.5; what it found is in section 14 (cross-unit findings U-1 .. U-6).
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)            # .../16-cmd-build-release.assets
base=$(cd "$here/.." && pwd)                       # .../20-design
dst=${1:?usage: assemble.sh <empty-target-dir>}
[ -d "$dst" ] && [ -z "$(ls -A "$dst")" ] || { echo "target must be an existing empty directory" >&2; exit 2; }

cp -R "$here/cmd" "$here/go.mod" "$here/go.sum" "$here/web" "$dst/"
mkdir -p "$dst/internal"
cp -R "$here/internal/github" "$here/internal/docs" "$dst/internal/"

# real packages (tests removed: they belong to their units and pull extra dependencies)
take() { # unit-dir package [dest-package]
  local src="$base/$1/internal/$2" out="$dst/internal/${3:-$2}"
  [ -d "$src" ] || { echo "missing $src" >&2; exit 1; }
  rm -rf "$out"; cp -R "$src" "$out"; find "$out" -name '*_test.go' -delete
}
for p in scm freshness job routes reactor; do take 01-kernel.assets "$p"; done
take 03-readmodel-store.assets store
take 03-readmodel-store.assets readmodel
take 04-refresh-webhook.assets refresh
take 04-refresh-webhook.assets webhook
take 04-refresh-webhook.assets ghverify
take 15-live-ops-overview.assets ops
take 15-live-ops-overview.assets httpapi
take 15-live-ops-overview.assets logx
cp -R "$base/02-platform.assets/internal/platform" "$dst/internal/platform"
cp -R "$base/08-catalog.assets/mod/internal/catalog" "$dst/internal/catalog"; find "$dst/internal/catalog" -name '*_test.go' -delete
cp "$base/02-platform.assets/go.mod" "$base/02-platform.assets/go.sum" "$dst/"
mkdir -p "$dst/internal/archtest" && cp -R "$here/internal/archtest/." "$dst/internal/archtest/"

cp "$here/union/union_test.go.txt" "$dst/cmd/scp/union_test.go"

cd "$dst"
go get modernc.org/sqlite@v1.60.1 github.com/getkin/kin-openapi@v0.150.0 github.com/oapi-codegen/runtime@v1.7.0 >/dev/null
cat "$base/06-httpapi.assets/go.sum" >> go.sum && sort -u go.sum -o go.sum
go mod tidy -e >/dev/null 2>&1 || true
echo "assembled in $dst; try: go build ./... && go test ./cmd/scp/internal/bridge; then union/swap-catalog.py $dst [--propose-fixes] and go test -run TestUnion ./cmd/scp"
