#!/usr/bin/env bash
# Image smoke test (design 16 section 4.8). Runs against a built image, locally (`make smoke`) and in CI, for the source-built
# image (every pull request) and for the release candidate image (before it is promoted, release.yml).
#
#   deploy/smoke.sh IMAGE
#
# Environment (all optional):
#   FAKEGITHUB_BIN   path to a linux static build of cmd/fakegithub. Set: FULL mode (fake GitHub on a private docker network,
#                    warm-up, SPA, snapshot, shutdown backup, warm restart with GitHub down). Unset: BASIC mode (hardening, probes, stop).
#   SMOKE_WORLD      fixture directory for the fake (default testdata/worlds/demo)
#   SMOKE_CONFIG     configuration mounted into the container (default deploy/dev.container.yaml; secrets are mounted at /run/secrets)
#   EXPECT_BUILD_ID  when set, X-SCP-Build of the shell must equal it
#   READY_TIMEOUT    seconds to wait for /readyz in FULL mode (default 120)
#
# Needs the docker CLI and curl on the host; the image has neither (probes go through published loopback ports).
set -euo pipefail

IMAGE=${1:?usage: smoke.sh IMAGE}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
WORLD=${SMOKE_WORLD:-$root/testdata/worlds/demo}
CONFIG=${SMOKE_CONFIG:-$here/dev.container.yaml}
READY_TIMEOUT=${READY_TIMEOUT:-120}
run=scp-smoke-$$
net=$run-net
vol=$run-data
full=0
[ -n "${FAKEGITHUB_BIN:-}" ] && full=1

fail() { echo "SMOKE FAIL: $*" >&2; dump; exit 1; }
ok()   { echo "ok   $*"; }
dump() { docker logs --tail 60 "$run" 2>&1 | sed 's/^/  scp| /' >&2 || true; docker logs --tail 20 "$run-fake" 2>&1 | sed 's/^/  fake| /' >&2 || true; }
cleanup() { docker rm -f "$run" "$run-fake" >/dev/null 2>&1 || true; docker volume rm -f "$vol" >/dev/null 2>&1 || true; docker network rm "$net" >/dev/null 2>&1 || true; rm -rf "${tmp:-}"; }
trap cleanup EXIT
tmp=$(mktemp -d)

# Secrets of the smoke stack, throwaway and world-readable on purpose: the container user (65532) must read bind-mounted files.
mkdir -p "$tmp/secrets"
printf 'dev-webhook-secret' >"$tmp/secrets/scp-demo-webhook"
token=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
printf '%s' "$token" >"$tmp/secrets/scp-admin-token"

port_of() { docker port "$1" "$2/tcp" | head -1 | sed 's/.*://'; } # published loopback port of a container port
http() { # http URL [header-file] -> prints body; exit 22 on non-2xx
  curl -fsS --max-time 5 ${2:+-D "$2"} "$1"
}
wait_for() { # wait_for SECONDS DESCRIPTION CMD...
  local n=$1 what=$2; shift 2
  for _ in $(seq "$n"); do "$@" >/dev/null 2>&1 && return 0; sleep 1; done
  fail "timed out after ${n}s waiting for $what"
}

docker network create "$net" >/dev/null
docker volume create "$vol" >/dev/null

if [ "$full" = 1 ]; then
  [ -d "$WORLD" ] || fail "fixture world $WORLD not found"
  cp -R "$WORLD" "$tmp/world"
  docker run --rm --user "$(id -u):$(id -g)" -v "$FAKEGITHUB_BIN":/fakegithub:ro -v "$tmp/world":/world \
    gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 \
    /fakegithub keygen --out /world >/dev/null || fail "fakegithub keygen failed"
  cp "$tmp/world/app.pem" "$tmp/secrets/scp-demo-app.pem"
  docker run -d --name "$run-fake" --network "$net" --network-alias fakegithub --read-only \
    -v "$FAKEGITHUB_BIN":/fakegithub:ro -v "$tmp/world":/world:ro \
    gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 \
    /fakegithub serve --world /world --listen 0.0.0.0:9191 --allow-remote --path-prefix /api/v3 >/dev/null
  ok "fake GitHub started"
fi

start_scp() {
  # Hardening is part of the contract being tested: read-only root, non-root user, no capabilities, no new privileges, limits.
  # There is no --tmpfs: distroless has no /tmp and the process must not need one (store temp_store=MEMORY).
  docker run -d --name "$run" --network "$net" --read-only --user 65532:65532 --cap-drop ALL \
    --security-opt no-new-privileges --pids-limit 256 --memory 512m -e GOMEMLIMIT=400MiB \
    -v "$vol":/data -v "$CONFIG":/etc/scp/config.yaml:ro -v "$tmp/secrets":/run/secrets:ro -e SCP_CONFIG=/etc/scp/config.yaml \
    -p 127.0.0.1::9090 -p 127.0.0.1::9091 "$IMAGE" serve >/dev/null
}
chmod -R a+rX "$tmp/secrets"
start_scp
pub=$(port_of "$run" 9090)
adm=$(port_of "$run" 9091)

wait_for 30 "/livez" http "http://127.0.0.1:$pub/livez"
ok "livez answers (the listeners open once the store is open and migrated; the engine catch-up and the warm-up run after)"

# --- image properties ------------------------------------------------------------------------------------------------------
[ "$(docker inspect -f '{{.Config.User}}' "$run")" = "65532:65532" ] || fail "container user is not 65532:65532"
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$run")" = "true" ] || fail "root filesystem is not read-only"
ok "non-root, read-only root filesystem"
if docker run --rm --entrypoint /bin/sh "$IMAGE" -c true >/dev/null 2>&1; then fail "the image contains a shell"; fi
docker create --name "$run-x" "$IMAGE" >/dev/null
docker export "$run-x" | tar -t 2>/dev/null >"$tmp/files" || true
docker rm -f "$run-x" >/dev/null
if grep -E '(^|/)(sh|bash|ash|busybox|apk|apt|apt-get|dpkg|curl|wget)$' "$tmp/files" >/dev/null; then fail "the image contains a shell or a package manager"; fi
ok "no shell, no package manager"
[ "$(docker image inspect -f '{{.Config.Entrypoint}}' "$IMAGE")" = "[/scp]" ] || fail "entrypoint is not /scp"
ver=$(docker run --rm "$IMAGE" version) || fail "scp version does not run"
grep -q '^scp ' <<<"$ver" || fail "scp version prints no 'scp ' line"
ok "scp version runs"

# --- readiness and content -------------------------------------------------------------------------------------------------
if [ "$full" = 1 ]; then
  wait_for "$READY_TIMEOUT" "/readyz after fixture warm-up" http "http://127.0.0.1:$pub/readyz"
  ok "readyz after warm-up"
  body=$(http "http://127.0.0.1:$pub/" "$tmp/shell.h") || fail "GET / failed"
  grep -qi '^content-type: text/html' "$tmp/shell.h" || fail "GET / is not text/html"
  grep -q 'id="root"' <<<"$body" || fail "GET / does not look like the SPA shell"
  if [ -n "${EXPECT_BUILD_ID:-}" ]; then
    grep -qi "^x-scp-build: $EXPECT_BUILD_ID" "$tmp/shell.h" || fail "X-SCP-Build is not $EXPECT_BUILD_ID"
  fi
  ok "the SPA shell is served with its build id"
  snap=$(http "http://127.0.0.1:$pub/api/v1/snapshot") || fail "GET /api/v1/snapshot failed"
  grep -q '"version"' <<<"$snap" || fail "snapshot has no version"
  http "http://127.0.0.1:$pub/llms.txt" >/dev/null || fail "GET /llms.txt failed"
  ok "API and llms.txt answer"
else
  wait_for 30 "/readyz" http "http://127.0.0.1:$pub/readyz"
  ok "readyz"
fi

# --- admin surface stays on the admin listener ---------------------------------------------------------------------------
# Captured first and matched afterwards: "curl | grep -q" under pipefail can fail on SIGPIPE although the line is there (V16-12).
metrics=$(curl -fsS --max-time 5 -H "Authorization: Bearer $token" "http://127.0.0.1:$adm/metrics") || fail "GET /metrics on the admin listener failed"
grep -q '^scp_build_info' <<<"$metrics" || fail "/metrics on the admin listener lacks scp_build_info"
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$adm/metrics" || true)
[ "$code" = 401 ] || fail "the admin listener answered $code without its bearer token (want 401)"
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$pub/metrics" || true)
[ "$code" != 200 ] || fail "/metrics is served on the public listener"
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$pub/debug/pprof/" || true)
[ "$code" != 200 ] || fail "pprof is served on the public listener"
ok "metrics only on the admin listener"

# --- SIGHUP is a reload request, not a termination (design 16 section 8.4, V16-01) -------------------------------------------
docker kill -s HUP "$run" >/dev/null
sleep 2
[ "$(docker inspect -f '{{.State.Running}}' "$run")" = "true" ] || fail "SIGHUP terminated the process (it must reload and keep serving)"
http "http://127.0.0.1:$pub/livez" >/dev/null || fail "/livez does not answer after SIGHUP"
ok "SIGHUP: reload requested, still serving"

# --- graceful stop -----------------------------------------------------------------------------------------------------
t0=$(date +%s)
docker stop -t 45 "$run" >/dev/null
took=$(( $(date +%s) - t0 ))
rc=$(docker inspect -f '{{.State.ExitCode}}' "$run")
[ "$rc" = 0 ] || fail "exit code $rc after SIGTERM (want 0)"
[ "$took" -le 30 ] || fail "stopping took ${took}s (budget 45s, typical under 12s)"
logs=$(docker logs "$run" 2>&1) || fail "docker logs failed"
grep -q 'shutdown complete' <<<"$logs" || fail "no 'shutdown complete' log line"
ok "SIGTERM: clean exit in ${took}s"

# --- the backup taken at shutdown (store.backup.on_shutdown; VACUUM INTO works on a read-only root because temp_store=MEMORY) -----
if [ "$full" = 1 ]; then
  docker run --rm --read-only -v "$vol":/d:ro --entrypoint /scp "$IMAGE" store check --db /d/scp.db.bak >/dev/null \
    || fail "no verifiable backup (/data/scp.db.bak) after the stop: store.backup.on_shutdown did not run"
  ok "shutdown backup written on a read-only root and verified"
fi

# --- warm restart with GitHub down ---------------------------------------------------------------------------------------
if [ "$full" = 1 ]; then
  docker rm -f "$run-fake" >/dev/null
  docker rm "$run" >/dev/null
  start_scp
  pub=$(port_of "$run" 9090)
  wait_for 40 "/readyz from the warm store with GitHub unreachable" http "http://127.0.0.1:$pub/readyz"
  snap=$(http "http://127.0.0.1:$pub/api/v1/snapshot") || fail "GET /api/v1/snapshot failed after the warm restart"
  grep -q '"version"' <<<"$snap" || fail "no snapshot after the warm restart"
  ok "warm restart: ready from the persisted store while GitHub is down"
fi

echo "SMOKE OK ($([ "$full" = 1 ] && echo full || echo basic))"
