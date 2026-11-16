package fleethealth

import (
	"slices"

	"github.com/cantara/source-code-portal/internal/feature/issues"
	"github.com/cantara/source-code-portal/internal/feature/pulls"
	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/health"
	"github.com/cantara/source-code-portal/internal/platform/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/platform/snapshot"
)

// OpenIssues is a repository's open issues, the best count held (design/features.md, P-77), and whether it counts the open pull
// requests too: the issues read when every page was, else GitHub's count less the open pull requests read, else GitHub's count of
// both. A switched-off feature has no facet, so the order needs no switch.
func OpenIssues(e *snapshot.RepoEntry) (n int, withPulls bool) {
	if set, _ := issues.Of(e); set != nil && set.Exact {
		return set.Total, false
	}
	if p := pulls.Open(e); p >= 0 {
		return max(e.Repo.OpenIssues-p, 0), false
	}
	return e.Repo.OpenIssues, true
}

// RepoIssuesAPI puts the best count of open issues on a repository summary.
func RepoIssuesAPI(_ *snapshot.Snapshot, e *snapshot.RepoEntry, r *oapi.Repo) {
	r.OpenIssues, r.OpenIssuesWithPulls = OpenIssues(e)
}

// IssueCount is OpenIssues alone, for the badges.
func IssueCount(e *snapshot.RepoEntry) int {
	n, _ := OpenIssues(e)
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
