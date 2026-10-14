#!/bin/sh
# Reproduces the executable proof of design unit 07 (section 12.6 of the design document).
#
#   sh prove.sh [scratch-dir]
#
# Stage 1 (contract only): apicheck on the design documents, split of the single-file documents into the ADR-020 repository
# layout, Redocly lint and bundle, oapi-codegen v2.8.0 (always-prefix-enum-values), the alias file of every feature api
# package, build, vet, test, and the comparison of the registered patterns with the bundled paths (both directions).
# Stage 2 (integration with unit 06; runs only when 06-httpapi.assets exists): overlay the generated packages on the real
# httpapi prototype of unit 06, apply the additions this contract asks of it (sketch/httpapi_additions.go and
# sketch/apitest_zerotime.go) and run `go test -race` over the whole tree.
#
# Needs: Go 1.27.x, Node (npx) for Redocly, python3 with PyYAML, network for the first module download.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)             # .../07-openapi-contract.assets/tools
ASSETS=$(dirname "$HERE")                       # .../07-openapi-contract.assets
DESIGN=$(dirname "$ASSETS")                     # .../20-design
A=${API_DIR:-$DESIGN/api}                       # common.yaml, openapi-public.yaml, openapi-admin.yaml, trusted-members.txt
S=${1:-$(mktemp -d)}                            # scratch directory
MOD=${MODULE:-github.com/cantara/source-code-portal}
R="npx --yes @redocly/cli@2.59.0"
export GOFLAGS=-mod=mod
FEATURES="catalog commits docs live manifests overview pages ops"
gen(){ go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 "$@"; }
echo "scratch: $S"
mkdir -p "$S" && cd "$S" && rm -rf split bin integ && mkdir bin

echo "== tools (apicheck, mock)"
(cd "$HERE" && go build -o "$S/bin/apicheck" ./apicheck && go build -o "$S/bin/mock" ./mock)

echo "== apicheck on the design documents"
"$S/bin/apicheck" -trusted "$A/trusted-members.txt" -public "$A/openapi-public.yaml" -admin "$A/openapi-admin.yaml" | tail -3
python3 -I "$HERE/keycheck.py" "$A/common.yaml" "$A/openapi-public.yaml" "$A/openapi-admin.yaml"

echo "== apicheck negative tests (one failing mutation per rule)"
(cd "$HERE" && APICHECK_DOCS="$A" go test -count=1 ./apicheck 2>&1 | tail -3)

echo "== split"
python3 -I "$HERE/split.py" "$A/openapi-public.yaml" "$A/openapi-admin.yaml" "$A/common.yaml" split >/dev/null
cp "$A/trusted-members.txt" split/api/trusted-members.txt
cd split

echo "== lint"; $R lint api/openapi.yaml 2>&1 | tail -3; $R lint api/openapi.admin.yaml 2>&1 | tail -3
echo "== bundle"; mkdir -p bundled
$R bundle api/openapi.yaml -o bundled/openapi.bundled.yaml 2>&1 | tail -1
$R bundle api/openapi.admin.yaml -o bundled/openapi.admin.bundled.yaml 2>&1 | tail -1
echo "== apicheck on the bundles"
"$S/bin/apicheck" -bundled -trusted api/trusted-members.txt -public bundled/openapi.bundled.yaml -admin bundled/openapi.admin.bundled.yaml | tail -1

cat > go.mod <<EOT
module $MOD

go 1.27

require github.com/oapi-codegen/runtime v1.7.0
EOT
cat > internal/httpapi/openapi/oapi.yaml <<'EOT'
package: openapi
output: common.gen.go
generate: {models: true, std-http-server: true, strict-server: true}
compatibility: {always-prefix-enum-values: true}
output-options: {skip-prune: true}
EOT
for f in $FEATURES; do
cat > internal/$f/${f}api/oapi.yaml <<EOT
package: ${f}api
output: ${f}.gen.go
generate: {models: true, std-http-server: true, strict-server: true}
compatibility: {always-prefix-enum-values: true}
import-mapping:
  ../../httpapi/openapi/common.yaml: $MOD/internal/httpapi/openapi
EOT
done
echo "== generate"
(cd internal/httpapi/openapi && gen -config oapi.yaml common.yaml)
for f in $FEATURES; do
  (cd internal/$f/${f}api && gen -config oapi.yaml $f.yaml)
  "$S/bin/apicheck" aliases -common internal/httpapi/openapi/common.yaml -gen internal/httpapi/openapi/common.gen.go -module "$MOD" -pkg ${f}api > internal/$f/${f}api/common_aliases.go
done
test -z "$(gofmt -l internal)" || { echo "FAIL: unformatted generated code"; gofmt -l internal; exit 1; }
find . -name '*.gen.go' | xargs wc -l | tail -12

cp "$ASSETS/sketch/mux_test.go" .
mkdir -p internal/apispec
cp bundled/openapi.bundled.yaml bundled/openapi.admin.bundled.yaml "$ASSETS/sketch/apispec.go" "$ASSETS/sketch/apispec_test.go" internal/apispec/
sed -i.bak "s#example.com/scp#$MOD#g" mux_test.go internal/apispec/apispec_test.go && rm -f mux_test.go.bak internal/apispec/*.bak

echo "== build vet test (stage 1)"
go build ./... && go vet ./... && go test -count=1 ./... 2>&1 | grep -v 'no test files' | tail -8

echo "== import check: a feature api package imports only httpapi/openapi (matrix 5.2, rank 1)"
for f in $FEATURES; do
  go list -f '{{join .Imports "\n"}}' ./internal/$f/${f}api | grep "^$MOD" | sed "s#^#   ${f}api -> #" | sort -u
done

echo "== patterns versus bundled paths (both directions)"
python3 -I - <<'EOT'
import yaml
d = yaml.safe_load(open('bundled/openapi.admin.bundled.yaml'))
want = set()
for p, item in d['paths'].items():
    for m in item:
        if m in ('get', 'post'):
            want.add(m.upper() + ' ' + p)
got = set(l.strip() for l in open('patterns.txt') if l.strip()) | {'GET /api/v1/openapi.yaml'}
print('operations in bundled admin document:', len(want), ' generated+root patterns:', len(got))
print('only in document:', sorted(want - got)); print('only registered:', sorted(got - want))
assert want == got
EOT

if [ -d "$DESIGN/06-httpapi.assets" ]; then
  echo "== stage 2: integration with the unit 06 prototype"
  cd "$S" && cp -R "$DESIGN/06-httpapi.assets" integ && cd integ
  # the shared package and unit 06's own commitsapi stub are regenerated from the current contract; the other seven
  # feature api packages (with their alias files) come from stage 1; unit 06's tests keep using its stub and bundles
  cp "$S/split/internal/httpapi/openapi/common.yaml" "$S/split/internal/httpapi/openapi/oapi.yaml" internal/httpapi/openapi/
  (cd internal/httpapi/openapi && gen -config oapi.yaml common.yaml)
  (cd internal/commits/commitsapi && gen -config oapi.yaml commits.yaml)
  for f in catalog docs live manifests overview pages ops; do
    mkdir -p internal/$f/${f}api && cp "$S/split/internal/$f/${f}api/"*.go "$S/split/internal/$f/${f}api/$f.yaml" internal/$f/${f}api/
  done
  sed -i.bak "s#$MOD#github.com/cantara/source-code-portal#g" internal/*/*api/*.go && find . -name '*.bak' -delete
  cp "$ASSETS/sketch/httpapi_additions.go" "$ASSETS/sketch/httpapi_additions_test.go" internal/httpapi/
  mkdir -p internal/live/testdata && cp "$ASSETS/sketch/live.go" "$ASSETS/sketch/live_test.go" internal/live/
  cp "$S/split/bundled/openapi.admin.bundled.yaml" internal/live/testdata/
  mkdir -p internal/overview && cp "$ASSETS/sketch/alias.go" "$ASSETS/sketch/alias_test.go" internal/overview/
  cp "$ASSETS/sketch/apitest_zerotime.go" "$ASSETS/sketch/apitest_zerotime_test.go" internal/httpapi/apitest/
  go build ./... && go vet ./... && go test -race -count=1 ./... 2>&1 | grep -v 'no test files' | tail -20
  echo "== stage 2 import check: the live feature imports only what matrix 5.2 allows (httpapi, liveapi), never httpapi/openapi"
  imports=$(go list -f '{{join .Imports " "}}' ./internal/live | tr ' ' '\n' | grep cantara | tr '\n' ' ')
  echo "internal/live imports: $imports"
  case "$imports" in *httpapi/openapi*) echo "FAIL: live imports httpapi/openapi"; exit 1;; esac
fi
echo "== done"
