package main

import (
	"slices"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/health"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/issues"
	"github.com/cantara/source-code-portal/internal/prereq"
	"github.com/cantara/source-code-portal/internal/pulls"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// openIssues is a repository's open issues, the best count held (design/features.md, P-77), and whether it counts the open pull
// requests too: the issues read when every page was, else GitHub's count less the open pull requests read, else GitHub's count of
// both. A switched-off feature has no facet, so the order needs no switch.
func openIssues(e *snapshot.RepoEntry) (n int, withPulls bool) {
	if set, _ := issues.Of(e); set != nil && set.Exact {
		return set.Total, false
	}
	if p := pulls.Open(e); p >= 0 {
		return max(e.Repo.OpenIssues-p, 0), false
	}
	return e.Repo.OpenIssues, true
}

// repoIssuesAPI puts the best count of open issues on a repository summary.
func repoIssuesAPI(_ *snapshot.Snapshot, e *snapshot.RepoEntry, r *oapi.Repo) {
	r.OpenIssues, r.OpenIssuesWithPulls = openIssues(e)
}

// issueCount is openIssues alone, for the badges.
func issueCount(e *snapshot.RepoEntry) int {
	n, _ := openIssues(e)
	return n
}

// judged is the health rules that apply: the findings of a switched-off feature are never made, so they are not told.
func judged(f config.Features) health.Rules {
	r := health.Describe()
	if !f.Pulls {
		r.Findings = slices.DeleteFunc(r.Findings, func(k health.Kind) bool { return k.Code == "reviews" || k.Code == "updates" })
	}
	return r
}

// needs is what the switched-on features need of GitHub.
func needs(f config.Features) []prereq.Need {
	return slices.DeleteFunc(slices.Clone(prereq.Needs), func(n prereq.Need) bool {
		return n.Feature == prereq.Pulls && !f.Pulls || n.Feature == prereq.Issues && !f.Issues
	})
}

// offElements is the layout elements of the features switched off.
func offElements(f config.Features) []oapi.LayoutElementKind {
	if !f.Pulls {
		return []oapi.LayoutElementKind{oapi.LayoutElementKindPulls}
	}
	return nil
}
