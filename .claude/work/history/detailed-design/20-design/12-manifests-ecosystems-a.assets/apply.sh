#!/bin/sh
# Build the scratch module of unit 12: the core scaffold of unit 11 + this overlay + the two patch files, then vet and test.
# usage: apply.sh <target-dir>   (the directory must not exist)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
core="$here/../11-manifests-core.assets"
dst=${1:?target directory}
[ ! -e "$dst" ] || { echo "$dst exists" >&2; exit 1; }
cp -R "$core" "$dst"
cp -R "$here/overlay/." "$dst/"
cd "$dst"
patch -p1 < "$here/patches/core-rr-a.diff"
patch -p1 < "$here/patches/go-mod.diff"
unformatted=$(gofmt -l .)
[ -z "$unformatted" ] || { echo "gofmt: $unformatted" >&2; exit 1; }
go vet ./...
go mod tidy -diff
# purity guard (design section 8): the six ecosystem packages and their helpers have no goroutine, no init, no clock, no randomness, no file or network access
if grep -rnE '(^|[^A-Za-z_.])go func|^func init\(\)|^(import)?[[:space:]]*"(os|os/exec|sync|sync/atomic|time|math/rand|math/rand/v2|crypto/rand|log|log/slog|net|net/http|io/ioutil|runtime)"$'internal/manifests/ecosystems internal/manifests/internal/tomlx internal/manifests/internal/expand internal/manifests/internal/redact internal/manifests/internal/linex internal/manifests/internal/repopath --include='*.go' --exclude='*_test.go' | grep -v '/testdata/'; then
  echo "purity guard failed" >&2; exit 1
fi
go test -race -count=1 ./internal/manifests/...
