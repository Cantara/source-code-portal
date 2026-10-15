package main

import (
	"time"

	"github.com/cantara/source-code-portal/internal/badges"
	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/health"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// dashboardFindings is how many findings the dashboard carries per product.
const dashboardFindings = 5

// failing reports whether SCP keeps failing to read an item: three failures in a row, or no success ever.
func failing(f snapshot.Freshness) bool {
	return f.Attempts >= 3 || (f.CheckedAt.IsZero() && f.State == snapshot.StateError)
}

// assess is a repository's health from the facets of the catalogue, its commits and its release.
func assess(e *snapshot.RepoEntry, now time.Time) health.Repo {
	r := e.Repo
	d, _ := catalog.DescriptorOf(e)
	last, commitsFresh := commits.LastCommit(e)
	s := health.Signals{Archived: r.Archived, Deprecated: d.Lifecycle == "deprecated", Public: r.Visibility == "public",
		LastChange: r.PushedAt, LastCommit: last, License: r.License, OpenIssues: r.OpenIssues,
		Failing: failing(e.Fresh) || failing(commitsFresh)}
	if at, before := commits.LastAuthored(e); !at.IsZero() {
		s.LastChange, s.Automated, s.Before = at, before || last.After(at), before
	}
	if rel, _ := badges.ReleaseOf(e); rel != nil {
		s.Release, s.Released = rel.Tag, rel.Published
	}
	return health.Assess(s, now)
}

// weigh is the health of the repositories keys (def is the default repository) present in the snapshot.
func weigh(s *snapshot.Snapshot, keys []snapshot.RepoKey, def snapshot.RepoKey, now time.Time) health.Product {
	members := make([]health.Member, 0, len(keys))
	for _, k := range keys {
		if e := s.Repos[k]; e != nil {
			members = append(members, health.Member{Address: string(k), Default: def != "" && k == def, Health: assess(e, now)})
		}
	}
	return health.Weigh(members)
}

func groupHealth(s *snapshot.Snapshot, g snapshot.Group, now time.Time) health.Product {
	return weigh(s, g.Members, g.DefaultRepo, now)
}

func repoHealthAPI(e *snapshot.RepoEntry, out *oapi.Repo) {
	r := assess(e, time.Now())
	if !r.Counted {
		return
	}
	h := &oapi.Health{Score: r.Score, Level: oapi.HealthLevel(r.Level), Findings: make([]oapi.Finding, 0, len(r.Findings))}
	for _, f := range r.Findings {
		h.Findings = append(h.Findings, oapi.Finding{Code: oapi.FindingCode(f.Code), Penalty: f.Penalty, Message: f.Message})
	}
	out.Health = h
}

// productAPI is a product's health in the API with at most limit findings (all when limit is 0); nil without a score.
func productAPI(p health.Product, limit int) *oapi.ProductHealth {
	if p.Counted == 0 {
		return nil
	}
	fs := p.Findings
	if limit > 0 && len(fs) > limit {
		fs = fs[:limit]
	}
	out := &oapi.ProductHealth{Score: p.Score, Level: oapi.HealthLevel(p.Level), Counted: p.Counted, Attention: p.Attention, Watch: p.Watch,
		FindingCount: len(p.Findings), Findings: make([]oapi.RepoFinding, 0, len(fs))}
	for _, f := range fs {
		out.Findings = append(out.Findings, oapi.RepoFinding{Repo: f.Repo, Code: oapi.RepoFindingCode(f.Code), Penalty: f.Penalty,
			Message: f.Message})
	}
	return out
}

func dashboardHealthAPI(s *snapshot.Snapshot, out *oapi.Dashboard) {
	now := time.Now()
	for i, g := range s.Groups {
		out.Groups[i].Health = productAPI(groupHealth(s, g, now), dashboardFindings)
	}
	keys := make([]snapshot.RepoKey, 0, len(s.Repos))
	for k := range s.Repos {
		keys = append(keys, k)
	}
	out.Health = productAPI(weigh(s, keys, "", now), dashboardFindings)
}

func groupHealthAPI(s *snapshot.Snapshot, g snapshot.Group, out *oapi.Group) {
	out.Health = productAPI(groupHealth(s, g, time.Now()), 0)
}
