package manifests

import (
	"path"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/version"
)

// GlobMatch matches a repo-relative path against a pattern: '*' matches within one segment, '**' as a whole segment
// matches zero or more segments, '?' one non-separator character. Used for workspace/module member patterns.
func GlobMatch(pattern, p string) bool {
	return globSegs(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func globSegs(pat, s []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for len(pat) > 0 && pat[0] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globSegs(pat, s[i:]) {
					return true
				}
			}
			return false
		}
		if len(s) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], s[0]); err != nil || !ok {
			return false
		}
		pat, s = pat[1:], s[1:]
	}
	return len(s) == 0
}

// ExpandMembers resolves member patterns written relative to base against the set of known directories (repo-relative),
// rejecting results that escape the repository. The result is sorted and de-duplicated.
func ExpandMembers(base string, patterns, dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, pat := range patterns {
		joined := path.Clean(path.Join(base, pat))
		if joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(joined) {
			continue
		}
		for _, d := range dirs {
			if !seen[d] && GlobMatch(joined, d) {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// JoinLock joins declared dependencies with locked components of the same unit: a declared dependency present in the lock
// gets the locked Version and ResLocked; lock entries without a declaration stay in the locked list; stale locks are diagnosed.
// Matching is by ComponentID; with several locked versions of one component the Direct one wins, else the first.
func JoinLock(eco EcosystemID, deps []Dependency, locked []LockedComponent) ([]Dependency, []Diagnostic) {
	byID := make(map[ComponentID]int, len(locked))
	for i, l := range locked {
		if j, ok := byID[l.ID]; !ok || (locked[j].Relationship != RelDirect && l.Relationship == RelDirect) {
			byID[l.ID] = i
		}
	}
	sys := version.System(version.Of(string(eco)))
	out := make([]Dependency, len(deps))
	copy(out, deps)
	var diags []Diagnostic
	for i := range out {
		d := &out[i]
		if d.Source != SourceRegistry {
			continue
		}
		j, ok := byID[d.Target]
		if !ok {
			if len(locked) > 0 && d.Scope != ScopeOptional {
				diags = append(diags, Diagnostic{Code: "lock.missing", Severity: SevWarning, Message: "declared dependency is absent from the lockfile: " + d.Target.String(), Loc: d.Loc()})
			}
			continue
		}
		l := locked[j]
		if d.Requirement != "" {
			if ok, known := version.Satisfies(sys, l.Version, d.Requirement); known && !ok {
				diags = append(diags, Diagnostic{Code: "lock.stale", Severity: SevWarning, Message: "locked version does not satisfy the declared requirement: " + d.Target.String(), Loc: d.Loc()})
			}
		}
		d.Version, d.Resolution = l.Version, ResLocked
	}
	return out, diags
}
