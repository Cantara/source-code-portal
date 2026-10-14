#!/usr/bin/env bash
# Lay the repository seed (repo/) into the root of this repository. Run once, at M0.
#
#   .claude/work/plans/50-repo-seed/bootstrap.sh [--install]
#
#   --install   pnpm install --frozen-lockfile in web/
#
# Stops without copying if a seed file would replace a different existing file (a root .gitignore or .gitattributes is replaced).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../../../.." && pwd)"

clashes() {
  local f
  (cd "$here/repo" && find . -type f) | while IFS= read -r f; do
    case "$f" in ./.gitignore|./.gitattributes) continue ;; esac
    if [ -e "$root/$f" ] && ! cmp -s "$here/repo/$f" "$root/$f"; then echo "  $f"; fi
  done
}
clash="$(clashes)"
if [ -n "$clash" ]; then printf 'bootstrap: these files exist and differ from the seed; nothing copied:\n%s\n' "$clash" >&2; exit 1; fi

cp -a "$here/repo/." "$root/"
echo "seed copied into $root; commit it as: chore: repository seed"

if [ "${1:-}" = "--install" ]; then (cd "$root/web" && pnpm install --frozen-lockfile); fi
