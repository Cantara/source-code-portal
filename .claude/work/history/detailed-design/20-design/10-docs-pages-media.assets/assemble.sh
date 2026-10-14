#!/bin/sh
# Assembles a buildable scratch module from this directory plus the sibling reference code of units 01, 02, 03 and 06.
# Usage: assemble.sh <target-dir>      (the target must not exist)
# Then:  cd <target-dir> && GOFLAGS=-mod=mod go vet ./internal/docs/... ./internal/media ./internal/pages ./cmd/... &&
#        GOFLAGS=-mod=mod go test -race ./internal/docs/... ./internal/media ./internal/pages ./cmd/...
# The adversarial timing tests skip themselves under -race; run them once without it:
#        GOFLAGS=-mod=mod go test -run 'Worst|Budget|Scaling' ./internal/docs/internal/render
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
DESIGN=$(cd "$HERE/.." && pwd)
T=$1
[ ! -e "$T" ] || { echo "$T exists" >&2; exit 1; }
mkdir -p "$T/internal/platform" "$T/cmd"
cp -R "$HERE/internal/docs" "$HERE/internal/media" "$HERE/internal/pages" "$T/internal/"
cp -R "$HERE/cmd/scp" "$T/cmd/"
cp -R "$HERE/testdata" "$HERE/go.mod" "$HERE/go.sum" "$T/"
for p in scm freshness job routes reactor safehtml; do cp -R "$DESIGN/01-kernel.assets/internal/$p" "$T/internal/$p"; done
for p in readmodel store; do cp -R "$DESIGN/03-readmodel-store.assets/internal/$p" "$T/internal/$p"; done
for p in secret logx netguard; do cp -R "$DESIGN/02-platform.assets/internal/platform/$p" "$T/internal/platform/$p"; done
cp -R "$DESIGN/06-httpapi.assets/internal/httpapi" "$T/internal/httpapi"
rm -rf "$T/internal/httpapi/validate" "$T/internal/httpapi/apitest" "$T/internal/store/sqlite" "$T/internal/store/storetest"
# unit 06 reference code names the logger package internal/logx; the architecture's path is internal/platform/logx
grep -rl 'internal/logx"' "$T/internal" | xargs sed -i.bak 's#internal/logx"#internal/platform/logx"#'
find "$T" -name '*.bak' -delete
# deviations D-2 and D-3: three regular expressions of the kernel safehtml policy (to be adopted by unit 01, PolicyVersion 2)
(cd "$T" && patch -p0 < "$HERE/safehtml-D2-D3.patch")
echo "assembled in $T"
