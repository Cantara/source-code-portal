#!/usr/bin/env bash
# Bootstrap the new Source Code Portal repository from this seed kit.
#
#   50-repo-seed/bootstrap.sh <target-dir> [options]
#
# Copies repo/ (the starter files) into <target-dir>, mirrors the design documents into <target-dir>/docs/design/, optionally
# initialises git and runs the checks of bootstrap step 1. Safe to run twice into an empty directory; it refuses a non-empty one.
#
# Options
#   --design DIR   the design workspace to mirror (default: the parent directory of this script, i.e. the design package root)
#   --no-design    do not mirror the design documents (CI of the seed itself; the repository is then not self-contained)
#   --git          git init -b main in <target-dir> and stage everything (no commit)
#   --commit       like --git, then commit "chore: repository seed" (needs user.name and user.email)
#   --install      pnpm install --frozen-lockfile in web/ (needs Node 24.21 and pnpm 12.10.1)
#   --verify       run `make arch test-go vet` (needs Go 1.27.2; a locally older Go switches itself with GOTOOLCHAIN=auto, the default)
#   -h, --help     this text
#
# Bash 3.2 compatible (macOS). Exit codes: 0 ok, 1 a step failed, 2 usage error.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
seed="$here/repo"
design="$(cd "$here/.." && pwd)"
target=""
do_design=1 do_git=0 do_commit=0 do_install=0 do_verify=0

usage() { sed -n '2,18p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }
die() { echo "bootstrap: $1" >&2; exit "${2:-1}"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --design) [ $# -ge 2 ] || die "--design needs a directory" 2; design="$(cd "$2" && pwd)"; shift ;;
    --no-design) do_design=0 ;;
    --git) do_git=1 ;;
    --commit) do_git=1; do_commit=1 ;;
    --install) do_install=1 ;;
    --verify) do_verify=1 ;;
    -h|--help) usage; exit 0 ;;
    -*) die "unknown option $1" 2 ;;
    *) [ -z "$target" ] || die "one target directory only" 2; target="$1" ;;
  esac
  shift
done
[ -n "$target" ] || { usage >&2; exit 2; }
[ -d "$seed" ] || die "seed directory $seed not found"

outside_workspace() { case "$1" in "$here"|"$here"/*|"$design"|"$design"/*) return 1 ;; esac; }
case "$target" in /*) want="$target" ;; *) want="$PWD/$target" ;; esac
outside_workspace "$want" || die "target must be outside the design workspace" 2
mkdir -p "$target"
target="$(cd "$target" && pwd)"
outside_workspace "$target" || die "target must be outside the design workspace" 2
if [ -n "$(ls -A "$target")" ]; then die "$target is not empty; bootstrap only fills an empty directory"; fi

echo "== 1. starter files: $seed -> $target"
# -a keeps modes (the shell scripts stay executable) and copies dotfiles; node_modules or build output never exist in the seed.
cp -a "$seed/." "$target/"

if [ "$do_design" -eq 1 ]; then
  echo "== 2. design documents: $design -> $target/docs/design"
  for must in 04-decisions.md 10-architecture.md 11-adr 20-design; do
    [ -e "$design/$must" ] || die "$design/$must not found: --design must point at the design package root"
  done
  mkdir -p "$target/docs/design"
  # Everything except the seed kit itself, dependency folders and OS files. Order is irrelevant; the README of the mirror says what is what.
  # The workspace README.md (index and user guide of the design package) is mirrored as INDEX.md: docs/design/README.md is the map written
  # for this repository (it ships in the seed) and must not be overwritten.
  (cd "$design" && tar -cf - --exclude='./50-repo-seed' --exclude='./README.md' --exclude='node_modules' --exclude='.DS_Store' --exclude='.git' .) | (cd "$target/docs/design" && tar -xf -)
  if [ -f "$design/README.md" ]; then cp "$design/README.md" "$target/docs/design/INDEX.md"; fi
  # A checksum list lets a reviewer prove the mirror is unmodified later: (cd docs/design && shasum -a 256 -c MANIFEST.sha256 --quiet)
  manifest="$target/docs/design/MANIFEST.sha256"
  (cd "$target/docs/design" && find . -type f ! -name MANIFEST.sha256 ! -name MIRROR.txt -print0 | LC_ALL=C sort -z | xargs -0 shasum -a 256 > "$manifest")
  printf 'source: %s\nmirrored: %s\nfiles: %s\n' "$design" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(wc -l < "$manifest" | tr -d ' ')" > "$target/docs/design/MIRROR.txt"
  [ -f "$target/docs/design/README.md" ] || die "docs/design/README.md (the map of the mirror) is missing from the seed"
else
  echo "== 2. design documents: skipped (--no-design)"
fi

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
  git -C "$target" init -q -b main
  git -C "$target" add -A
  if [ "$do_commit" -eq 1 ]; then git -C "$target" commit -q -m "chore: repository seed" && git -C "$target" log --oneline -1; fi
fi

if [ "$do_install" -eq 1 ]; then
  echo "== 5. web dependencies"
  have_pnpm="$(pnpm --version 2>/dev/null || echo none)"
  case "$have_pnpm" in
    12.*) ;;
    *) die "--install needs pnpm 12.x (found $have_pnpm); the lockfile format of pnpm 12 is not readable by older versions: npm install --global pnpm@12.10.1" ;;
  esac
  (cd "$target/web" && pnpm install --frozen-lockfile)
fi

if [ "$do_verify" -eq 1 ]; then
  echo "== 6. verify (make arch test-go vet)"
  (cd "$target" && make arch test-go vet)
fi

cat <<EOF

Done. Next (design 16 section 13, tasks B-1 to B-3):
  cd $target
  make check                      # generated files, lint, arch, race tests, web checks, govulncheck, licences (needs golangci-lint v2.14.0, Node 24.21, pnpm 12.10.1)
  cat docs/seed-notes.md          # what the seed changed against the design files, and what it leaves open
  git remote add origin <url>     # then protect the branch and set the repository rules (design 16 section 4.7.3)
EOF
