#!/bin/sh
# Fetch the non-committed oracle corpus of unit 12 into $1 (default ./oracle-root) and verify every git blob id.
# Needs: gh (authenticated, only for the repository blob API), curl, git. Nothing is executed from the downloaded files.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=${1:-./oracle-root}
mkdir -p "$root"
grep -v '^#' "$here/SOURCES.tsv" | while IFS="$(printf '\t')" read -r local src path sha size lic head note; do
  [ -n "$local" ] || continue
  dest="$root/$local"
  mkdir -p "$(dirname "$dest")"
  case "$src" in
    https://*) curl -fsSL "$src" -o "$dest" ;;
    *) gh api "repos/$src/git/blobs/$sha" -H "Accept: application/vnd.github.raw" > "$dest" ;;
  esac
  got=$(git hash-object "$dest")
  if [ "$got" != "$sha" ]; then echo "MISMATCH $local want $sha got $got" >&2; exit 1; fi
done
echo "oracle corpus verified in $root"
