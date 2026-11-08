#!/usr/bin/env bash
# Rehearses an upgrade and a rollback of the store between a released build and this one, offline (a fake GitHub). The released build
# reads the fake GitHub into a store (1). Then, by the schema generation of that store (its SCP1 application mark):
#   the same generation as this build:
#     2. this build opens the store with GitHub gone: it migrates, takes the scp.db.pre-NNNN copy, and serves the data from the store,
#     3. the released build, older than the schema, quarantines the migrated store and starts empty (the documented rollback hazard),
#     4. restoring the copy makes the released build serve the data again.
#   an earlier generation (the releases before the one baseline, SC1):
#     2. this build moves the store aside (scp.db.quarantine-*) and reads GitHub again into a new one,
#     3. the released build serves the new store with GitHub gone (the baseline holds all it needs),
#     4. renaming the old store back makes the released build serve its own data again.
# Usage: scripts/upgrade-rehearsal.sh [released-tag]   (default: the newest tag). Needs go, git, curl, od. Takes about a minute.
source "$(dirname "$0")/rehearsal-lib.sh"
rehearsal_init
prev=${1:-$(git -C "$root" describe --tags --abbrev=0)}

echo "building $prev and this tree"
git -C "$root" worktree add --detach "$work/old" "$prev" >/dev/null 2>&1
build_tree "$work/old" "$work/scp-old"
build_tree "$root" "$work/scp-new"

echo "starting the fake GitHub"
start_github
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

echo "1. $prev reads GitHub into the store"
run "$work/scp-old" old1
serves alpha || fail "$prev did not serve alpha"
serves beta || fail "$prev did not serve beta"
stop "$pid"
[ -s "$db" ] || fail "no store was written"

# The application mark is four bytes at offset 68 of the file's header: "SCP1" in the generation this tree makes.
mark=$(od -An -tx1 -j68 -N4 "$db" | tr -d ' \n')

if [ "$mark" = "53435031" ]; then
	stop_github # GitHub is gone from here on: only the store can answer

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
else
	echo "2. this build finds an earlier schema generation: moves the store aside, reads GitHub again"
	run "$work/scp-new" new
	serves alpha || fail "this build did not read alpha again"
	serves beta || fail "this build did not read beta again"
	stop "$pid"
	ls "$work"/scp.db.quarantine-* >/dev/null 2>&1 || fail "the earlier store was not moved aside"
	grep -q "earlier schema generation" "$work/new.log" || fail "the log does not say why the store was moved aside"
	stop_github # GitHub is gone from here on
	[ "$(od -An -tx1 -j68 -N4 "$db" | tr -d ' \n')" = "53435031" ] || fail "the new store does not carry the SCP1 mark"

	echo "3. $prev serves the new store (the baseline holds everything it needs)"
	run "$work/scp-old" old2
	serves alpha || fail "$prev does not serve alpha from the new store"
	serves beta || fail "$prev does not serve beta from the new store"
	stop "$pid"

	echo "4. renaming the old store back brings it back for $prev"
	old=$(ls "$work"/scp.db.quarantine-* | grep -v -e '-wal$' -e '-shm$' | sort | head -n 1)
	rm -f "$db" "$db-wal" "$db-shm"
	mv "$old" "$db"
	run "$work/scp-old" old3
	serves alpha || fail "$prev does not serve alpha from its own store"
	stop "$pid"

	echo "PASS: upgrade from $prev (an earlier generation: moved aside and read again) and rollback by renaming"
fi
rm -rf "$work"
