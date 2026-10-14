#!/usr/bin/env bash
# Lay the repository seed into the Source Code Portal repository.
#
#   .claude/work/plans/50-repo-seed/bootstrap.sh [<repo-root>] [options]
#
# Copies repo/ (the starter files) into the root of the repository that holds this design package at .claude/work/plans/
# (default: derived from this script's location). The design package is not copied anywhere: planning and design live under
# .claude/work/, docs/ is user documentation only (owner layout rule, 2026-10-14). Nothing is overwritten: a seed file whose target
# exists with different content is a conflict, and all conflicts are listed before anything is copied. The one exception is a root
# .gitignore or .gitattributes whose every rule is already in the seed's version: the seed's version replaces it. Running it again
# after a successful run finds only identical files and changes nothing.
#
# Options
#   --git          git init -b main if <repo-root> is not a git repository yet, then stage everything (no commit)
#   --commit       like --git, then commit "chore: repository seed" (needs user.name and user.email)
#   --install      pnpm install --frozen-lockfile in web/ (needs Node 24.21 and pnpm 12.10.1)
#   --verify       run `make arch test-go vet` (needs Go 1.27.2; a locally older Go switches itself with GOTOOLCHAIN=auto, the default)
#   -h, --help     this text
#
# Bash 3.2 compatible (macOS). Exit codes: 0 ok, 1 a step failed or a conflict was found, 2 usage error.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
seed="$here/repo"
design="$(cd "$here/.." && pwd)"
root=""
do_git=0 do_commit=0 do_install=0 do_verify=0

usage() { sed -n '2,21p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }
die() { echo "bootstrap: $1" >&2; exit "${2:-1}"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --git) do_git=1 ;;
    --commit) do_git=1; do_commit=1 ;;
    --install) do_install=1 ;;
    --verify) do_verify=1 ;;
    -h|--help) usage; exit 0 ;;
    -*) die "unknown option $1" 2 ;;
    *) [ -z "$root" ] || die "one repository root only" 2; root="$1" ;;
  esac
  shift
done
[ -d "$seed" ] || die "seed directory $seed not found"
for must in 04-decisions.md 10-architecture.md 11-adr 20-design; do
  [ -e "$design/$must" ] || die "$design/$must not found: this script must stay at .claude/work/plans/50-repo-seed/ of the design package"
done
[ -n "$root" ] || root="$design/../../.."
[ -d "$root" ] || die "$root is not a directory" 2
root="$(cd "$root" && pwd)"
[ "$root/.claude/work/plans" -ef "$design" ] || die "$root is not the repository holding this design package (expected $root/.claude/work/plans to be $design)" 2

# rules_covered FILE SEED_FILE: every rule line of FILE (not blank, not a comment) also appears in SEED_FILE.
rules_covered() {
  local line
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in ''|'#'*) continue ;; esac
    grep -Fxq -e "$line" "$2" || return 1
  done < "$1"
}

echo "== 1. conflict check: $seed -> $root"
conflicts=0 replaced=""
while IFS= read -r rel; do
  rel="${rel#./}"
  dst="$root/$rel"
  if [ ! -e "$dst" ] && [ ! -L "$dst" ]; then continue; fi
  if [ -f "$dst" ] && cmp -s "$seed/$rel" "$dst"; then continue; fi
  case "$rel" in
    .gitignore|.gitattributes)
      if [ -f "$dst" ] && rules_covered "$dst" "$seed/$rel"; then replaced="$replaced $rel"; continue; fi ;;
  esac
  echo "  conflict: $rel exists and differs from the seed" >&2
  conflicts=$((conflicts + 1))
done <<EOF
$(cd "$seed" && find . \( -type f -o -type l \) | LC_ALL=C sort)
EOF
[ "$conflicts" -eq 0 ] || die "$conflicts conflict(s); nothing was copied. Resolve them (move the file, or fold its content into the seed) and run again"
[ -z "$replaced" ] || echo "  replaced by the seed's superset version:$replaced"

echo "== 2. starter files"
# -a keeps modes (the shell scripts stay executable) and copies dotfiles; node_modules or build output never exist in the seed.
cp -a "$seed/." "$root/"

echo "== 3. toolchain report"
report() { # name, command...
  local name="$1"; shift
  if command -v "$1" >/dev/null 2>&1; then printf '  %-14s %s\n' "$name" "$("$@" 2>&1 | head -1)"; else printf '  %-14s %s\n' "$name" "MISSING"; fi
}
report go go version
report golangci-lint golangci-lint version
report node node --version
report pnpm pnpm --version
report docker docker --version
printf '  %-14s %s\n' "wanted" "go 1.27.2 (go.mod), golangci-lint v2.14.0 (binary), node 24.21.0, pnpm 12.10.1"

if [ "$do_git" -eq 1 ]; then
  echo "== 4. git"
  if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "  existing repository on branch $(git -C "$root" symbolic-ref --short HEAD 2>/dev/null || echo '(detached)')"
  else
    git -C "$root" init -q -b main
  fi
  git -C "$root" add -A
  if [ "$do_commit" -eq 1 ]; then git -C "$root" commit -q -m "chore: repository seed" && git -C "$root" log --oneline -1; fi
fi

if [ "$do_install" -eq 1 ]; then
  echo "== 5. web dependencies"
  have_pnpm="$(pnpm --version 2>/dev/null || echo none)"
  case "$have_pnpm" in
    12.*) ;;
    *) die "--install needs pnpm 12.x (found $have_pnpm); the lockfile format of pnpm 12 is not readable by older versions: npm install --global pnpm@12.10.1" ;;
  esac
  (cd "$root/web" && pnpm install --frozen-lockfile)
fi

if [ "$do_verify" -eq 1 ]; then
  echo "== 6. verify (make arch test-go vet)"
  (cd "$root" && make arch test-go vet)
fi

cat <<EOF

Done. Next (design 16 section 13, tasks B-1 to B-3):
  cd $root
  make check                      # generated files, lint, arch, race tests, web checks, govulncheck, licences (needs golangci-lint v2.14.0, Node 24.21, pnpm 12.10.1)
  cat .claude/work/plans/50-repo-seed/seed-notes.md   # what the seed changed against the design files, and what it leaves open
  git remote -v                   # origin is git@github.com:Cantara/source-code-portal.git; protect main and set the repository rules (design 16 section 4.7.3)
EOF
