#!/usr/bin/env bash
# Assert that a built scp binary has the shape that ships (design 16 sections 4.6 and 4.9). Used by ci.yml (build job) and release.yml.
#   deploy/check-binary.sh bin/scp [--allow-dirty]
# Reads only the Go build info embedded in the binary and runs `scp version --json` when the binary runs on this host.
set -euo pipefail
bin=${1:?usage: check-binary.sh BINARY [--allow-dirty]}
allow_dirty=${2:-}
info=$(go version -m "$bin")
fail() { echo "check-binary: $*" >&2; echo "$info" >&2; exit 1; }

grep -qE $'\tbuild\t-tags=spa$' <<<"$info"           || fail "build tags are not exactly 'spa' (a development tag, or no SPA, would ship)"
grep -qE $'\tbuild\tCGO_ENABLED=0$' <<<"$info"       || fail "CGO is not disabled"
grep -qE $'\tbuild\t-trimpath=true$' <<<"$info"      || fail "-trimpath is missing"
if grep -qE $'\tbuild\t-race=true' <<<"$info"; then fail "built with the race detector"; fi
if grep -qE $'\tbuild\t-cover' <<<"$info"; then fail "built with coverage instrumentation"; fi
if [ "$allow_dirty" != "--allow-dirty" ]; then
  grep -qE $'\tbuild\tvcs\\.modified=false$' <<<"$info" || fail "the working tree was modified when this binary was built"
fi

# Dynamic linking would make the distroless static image fail at start: ldd must not find an interpreter.
if command -v ldd >/dev/null 2>&1 && ldd "$bin" >/dev/null 2>&1 && file "$bin" | grep -q 'dynamically linked'; then fail "the binary is dynamically linked"; fi

# Run it where it can run: the embedded SPA must be the real bundle, and the build id must be the commit.
if out=$("$bin" version --json 2>/dev/null); then
  grep -q '"spa": "spa"' <<<"$out" || fail "the embedded bundle is the placeholder (web/dist was not built)"
  if [ -n "${EXPECT_BUILD_ID:-}" ]; then grep -q "\"build\": \"$EXPECT_BUILD_ID\"" <<<"$out" || fail "build id is not $EXPECT_BUILD_ID"; fi
fi
echo "check-binary: $bin ok"
