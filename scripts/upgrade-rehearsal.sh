#!/usr/bin/env bash
# Rehearses an upgrade and a rollback of the store between a released build and this one, offline (a fake GitHub):
#   1. the released build reads the fake GitHub into a store,
#   2. this build opens that store with GitHub gone: it migrates, takes the scp.db.pre-NNNN copy, and serves the data from the store,
#   3. the released build, older than the schema, quarantines the migrated store and starts empty (the documented rollback hazard),
#   4. restoring the copy makes the released build serve the data again.
# Usage: scripts/upgrade-rehearsal.sh [released-tag]   (default: the newest tag). Needs go, git, curl. Takes about a minute.
set -euo pipefail

root=$(git rev-parse --show-toplevel)
prev=${1:-$(git -C "$root" describe --tags --abbrev=0)}
work=$(mktemp -d)
pids=()
fail() { echo "FAIL: $*" >&2; for f in "$work"/*.log; do echo "--- $f" >&2; tail -n 15 "$f" >&2; done; echo "files kept in $work" >&2; exit 1; }
cleanup() {
	for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
	git -C "$root" worktree remove --force "$work/old" 2>/dev/null || true
}
trap cleanup EXIT

port=$((20000 + RANDOM % 20000))
admin=$((port + 1))
db="$work/scp.db"
base="http://127.0.0.1:$port"

echo "building $prev and this tree"
git -C "$root" worktree add --detach "$work/old" "$prev" >/dev/null 2>&1
(cd "$work/old" && CGO_ENABLED=0 go build -o "$work/scp-old" ./cmd/scp)
(cd "$root" && CGO_ENABLED=0 go build -o "$work/scp-new" ./cmd/scp)

echo "starting the fake GitHub"
(cd "$root" && REHEARSAL_GH_URL_FILE="$work/gh.url" go test -count=1 -timeout 0 -run '^TestServeForRehearsal$' ./cmd/scp >"$work/github.log" 2>&1) &
ghpid=$!
pids+=("$ghpid")
for _ in $(seq 1 120); do [ -s "$work/gh.url" ] && break; sleep 0.5; done
[ -s "$work/gh.url" ] || fail "the fake GitHub did not start"

echo rehearsal-token >"$work/token"
cat >"$work/scp.yaml" <<YAML
server: {public_addr: "127.0.0.1:$port", admin_addr: "127.0.0.1:$admin"}
log: {level: info, format: text}
store: {path: "$db"}
github:
  sources:
    - {id: gh, base_url: "$(cat "$work/gh.url")", orgs: [acme], token_file: "$work/token"}
catalog:
  groups:
    - {id: all, name: All, default_repo: alpha, match: [{catch_all: true}]}
YAML

run() { # run <binary> <log>: start in the background, remember the pid in $pid
	"$1" serve -c "$work/scp.yaml" >"$work/$2.log" 2>&1 &
	pid=$!
	pids+=("$pid")
}
stop() {
	kill -TERM "$1" 2>/dev/null || true
	wait "$1" 2>/dev/null || true
}
serves() { # serves <what>: wait until the dashboard names it, or fail
	for _ in $(seq 1 120); do
		curl -sf "$base/api/v1/dashboard" 2>/dev/null | grep -q "$1" && return 0
		sleep 0.5
	done
	return 1
}
lacks() { ! curl -sf "$base/api/v1/dashboard" 2>/dev/null | grep -q "$1"; }

echo "1. $prev reads GitHub into the store"
run "$work/scp-old" old1
serves alpha || fail "$prev did not serve alpha"
serves beta || fail "$prev did not serve beta"
stop "$pid"
kill "$ghpid" 2>/dev/null || true # GitHub is gone from here on: only the store can answer
wait "$ghpid" 2>/dev/null || true
[ -s "$db" ] || fail "no store was written"

echo "2. this build opens the store: migrates, copies, serves"
run "$work/scp-new" new
serves alpha || fail "this build lost the data of $prev's store"
serves beta || fail "this build lost beta"
stop "$pid"
ls "$work"/scp.db.pre-* >/dev/null 2>&1 || fail "no scp.db.pre-NNNN copy was taken before the migration"
copy=$(ls "$work"/scp.db.pre-* | sort | head -n 1)

echo "3. $prev on the migrated store: quarantined, empty"
run "$work/scp-old" old2
for _ in $(seq 1 60); do ls "$work"/scp.db.quarantine-* >/dev/null 2>&1 && break; sleep 0.5; done
ls "$work"/scp.db.quarantine-* >/dev/null 2>&1 || fail "$prev did not quarantine the newer store"
lacks alpha || fail "$prev served data from a store it cannot read"
stop "$pid"

echo "4. the copy brings the data back for $prev"
rm -f "$work"/scp.db.quarantine-* "$db" "$db-wal" "$db-shm"
cp "$copy" "$db"
run "$work/scp-old" old3
serves alpha || fail "$prev does not serve alpha from the restored copy"
stop "$pid"

echo "PASS: upgrade from $prev and rollback with the copy"
rm -rf "$work"
