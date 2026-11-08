# Shared by the rehearsal scripts (source it). Builds, a fake GitHub, one portal at a time on a store in a temp directory.
# Needs go, git and curl. Sets: root, work, port, admin, db, base. Call rehearsal_init first.
set -euo pipefail

pids=()
rehearsal_init() {
	root=$(git rev-parse --show-toplevel)
	work=$(mktemp -d)
	port=$((20000 + RANDOM % 20000))
	admin=$((port + 1))
	db="$work/scp.db"
	base="http://127.0.0.1:$port"
	trap rehearsal_cleanup EXIT
}
rehearsal_cleanup() {
	for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
	git -C "$root" worktree remove --force "$work/old" 2>/dev/null || true
}
fail() {
	echo "FAIL: $*" >&2
	for f in "$work"/*.log; do echo "--- $f" >&2; tail -n 15 "$f" >&2; done
	echo "files kept in $work" >&2
	exit 1
}

# build_tree <dir> <out>: the program built from a source tree
build_tree() { (cd "$1" && CGO_ENABLED=0 go build -o "$2" ./cmd/scp); }

# start_github: the fake GitHub of cmd/scp/rehearsal_test.go (the organisation acme, the repositories alpha and beta, the token
# rehearsal-token); its pid is in ghpid, its address in $work/gh.url
start_github() {
	(cd "$root" && go test -c -o "$work/github.test" ./cmd/scp)
	REHEARSAL_GH_URL_FILE="$work/gh.url" "$work/github.test" -test.run '^TestServeForRehearsal$' -test.timeout 0 >"$work/github.log" 2>&1 &
	ghpid=$!
	pids+=("$ghpid")
	for _ in $(seq 1 120); do [ -s "$work/gh.url" ] && break; sleep 0.5; done
	[ -s "$work/gh.url" ] || fail "the fake GitHub did not start"
	echo rehearsal-token >"$work/token"
}
stop_github() {
	kill "$ghpid" 2>/dev/null || true
	wait "$ghpid" 2>/dev/null || true
}

# run <binary> <log>: start in the background; the pid is in pid. stop <pid> ends it politely, kill9 <pid> as a crash.
run() {
	"$1" serve -c "$work/scp.yaml" >"$work/$2.log" 2>&1 &
	pid=$!
	pids+=("$pid")
}
stop() {
	kill -TERM "$1" 2>/dev/null || true
	wait "$1" 2>/dev/null || true
}
kill9() {
	kill -KILL "$1" 2>/dev/null || true
	wait "$1" 2>/dev/null || true
}

# serves <what>: wait until the dashboard names it; lacks <what>: it does not
serves() {
	for _ in $(seq 1 120); do
		curl -sf "$base/api/v1/dashboard" 2>/dev/null | grep -q "$1" && return 0
		sleep 0.5
	done
	return 1
}
lacks() { ! curl -sf "$base/api/v1/dashboard" 2>/dev/null | grep -q "$1"; }
