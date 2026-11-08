#!/bin/sh
# Keeps the repository deployment-neutral: fails when a name that must stay out of it (a company, a host, a customer) appears in what is
# about to be committed or in the tracked tree. The names are not in this repository: they are one extended regular expression per line
# in a private file, SCP_NEUTRAL_PATTERNS or ~/.dossier/cantara/source-code-portal/neutral-patterns.txt. Without that file the check
# does nothing, so it never bothers anyone who has none. Matches whole words, ignoring case.
#   check-neutral.sh staged        the staged changes (pre-commit)
#   check-neutral.sh msg <file>    a commit message (commit-msg)
#   check-neutral.sh tree          every tracked file (make check)
set -eu
patterns=${SCP_NEUTRAL_PATTERNS:-$HOME/.dossier/cantara/source-code-portal/neutral-patterns.txt}
[ -r "$patterns" ] || exit 0
cd "$(git rev-parse --show-toplevel)"
case "${1:-tree}" in
staged) hits=$(git diff --cached -U0 --diff-filter=ACM | grep -E '^\+[^+]' | grep -Eiwf "$patterns" || true) ;;
msg) hits=$(grep -Eiwf "$patterns" "$2" || true) ;;
tree) hits=$(git grep -nEiwf "$patterns" -- . ':!.claude/work/history' || true) ;;
*) echo "usage: check-neutral.sh staged | msg <file> | tree" >&2; exit 2 ;;
esac
if [ -n "$hits" ]; then
	echo "neutral: a name that must stay out of this repository is in it (the list: $patterns):" >&2
	echo "$hits" | cut -c1-160 >&2
	echo "Move the fact to the dossier (~/.dossier) and word it generically here." >&2
	exit 1
fi
