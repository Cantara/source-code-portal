package main

import (
	"github.com/cantara/source-code-portal/internal/badges"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// sharedBy is the share of a group's members a value must have to be said once for the group instead of on every repository.
const sharedBy = 0.8

// dashboardSummaryAPI says once per group what most of its members share: the licence and the language (from the catalogue and the
// manifests), and how many members have a release.
func dashboardSummaryAPI(s *snapshot.Snapshot, out *oapi.Dashboard) {
	for i := range out.Groups {
		g := &out.Groups[i]
		licences, languages := map[string]int{}, map[string]int{}
		n := 0
		for _, m := range g.Members {
			e := s.Repos[snapshot.RepoKey(m.Address)]
			if e == nil {
				continue
			}
			n++
			licences[e.Repo.License]++
			var r oapi.Repo
			manifests.Repo(s, e, &r)
			languages[r.Language]++
			if rel, _ := badges.ReleaseOf(e); rel != nil {
				g.Summary.Released++
			}
		}
		g.Summary.License, g.Summary.Language = shared(licences, n), shared(languages, n)
	}
}

// shared is the non-empty value at least sharedBy of n have; nil when there is none.
func shared(counts map[string]int, n int) *string {
	for v, c := range counts {
		if v != "" && float64(c) >= sharedBy*float64(n) {
			return &v
		}
	}
	return nil
}
