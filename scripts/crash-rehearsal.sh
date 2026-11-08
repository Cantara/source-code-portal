#!/usr/bin/env bash
# Rehearses that an instance that dies survives (a hard requirement): this tree runs against a fake GitHub, an administrator saves a
# layout and an announcement, the process is killed with SIGKILL the moment the saves are acknowledged and GitHub is gone, and the
# restarted process serves the catalogue and both saves from the store alone. Also proves the import of layout.dir files from before
# documents lived in the store (the file is removed before the restart, so only the store can have it).
# Usage: scripts/crash-rehearsal.sh. Needs go, curl, openssl. Takes about half a minute.
source "$(dirname "$0")/rehearsal-lib.sh"
rehearsal_init

echo "building this tree"
build_tree "$root" "$work/scp"
start_github
openssl rand -hex 32 >"$work/ops-token"
openssl rand -hex 32 >"$work/session-key"
mkdir "$work/layouts"
echo '{"elements":[{"kind":"telemetry"}]}' >"$work/layouts/dashboard.json" # from before documents were in the store
cat >"$work/scp.yaml" <<YAML
server: {public_addr: "127.0.0.1:$port", admin_addr: "127.0.0.1:$admin", public_url: "$base"}
log: {level: info, format: text}
store: {path: "$db"}
layout: {dir: "$work/layouts"}
auth:
  session_key_file: "$work/session-key"
  tokens: [{name: ops, secret_file: "$work/ops-token", groups: [staff], admin: true}]
access: {private_groups: [staff]}
github:
  sources:
    - {id: gh, base_url: "$(cat "$work/gh.url")", orgs: [acme], token_file: "$work/token"}
catalog:
  groups:
    - {id: all, name: All, default_repo: alpha, match: [{catch_all: true}]}
YAML
auth=(-H "Authorization: Bearer $(cat "$work/ops-token")")
page() { curl -sf "$base/api/v1/pages/dashboard"; }

echo "1. start, import the layout file, save a layout and an announcement"
run "$work/scp" before
serves alpha || fail "the portal did not read GitHub"
grep -q "kept in the store" "$work/before.log" || fail "layout.dir was not imported"
page | grep -q '"kind":"telemetry"' || fail "the imported layout is not served"
rev=$(curl -sf "${auth[@]}" "$base/api/v1/admin/layouts/dashboard" | python3 -c 'import json,sys; print(json.load(sys.stdin)["revision"])')
code=$(curl -s -o "$work/put-layout.json" -w '%{http_code}' "${auth[@]}" -X PUT -H 'Content-Type: application/json' \
	-d "{\"revision\":\"$rev\",\"elements\":[{\"kind\":\"telemetry\"},{\"kind\":\"fleet-map\"}]}" "$base/api/v1/admin/layouts/dashboard")
[ "$code" = 200 ] || fail "saving the layout: $code $(cat "$work/put-layout.json")"
code=$(curl -s -o "$work/put-note.json" -w '%{http_code}' "${auth[@]}" -X PUT -H 'Content-Type: application/json' \
	-d '{"revision":"","announcements":[{"id":"noon","title":"Deploy at noon","tone":"notice","until":"2099-01-01T00:00:00Z","published":true}]}' \
	"$base/api/v1/admin/announcements")
[ "$code" = 200 ] || fail "saving the announcement: $code $(cat "$work/put-note.json")"

echo "2. the process dies (SIGKILL) as soon as the saves are acknowledged; GitHub and the layout file are gone"
kill9 "$pid"
stop_github
rm -rf "$work/layouts"

echo "3. the restarted process serves the catalogue and both saves from the store alone"
run "$work/scp" after
serves alpha || fail "the catalogue did not survive the kill"
serves beta || fail "beta did not survive the kill"
page | grep -q '"kind":"fleet-map"' || fail "the saved layout did not survive the kill"
page | grep -q 'Deploy at noon' || fail "the saved announcement did not survive the kill"
stop "$pid"

echo "PASS: an instance killed with SIGKILL comes back with everything it had acknowledged"
rm -rf "$work"
