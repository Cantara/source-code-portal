package main

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/badges"
	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/health"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/packages"
	"github.com/cantara/source-code-portal/internal/pulls"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// dashboardFindings is how many findings the dashboard carries per product.
const dashboardFindings = 5

// failing reports whether SCP keeps failing to read an item: three failures in a row, or no success ever.
func failing(f snapshot.Freshness) bool {
	return f.Attempts >= 3 || (f.CheckedAt.IsZero() && f.State == snapshot.StateError)
}

// assess is a repository's health in the view s from the facets of the catalogue, its commits and its release.
func assess(s *snapshot.Snapshot, e *snapshot.RepoEntry, now time.Time) health.Repo {
	r := e.Repo
	d, _ := catalog.DescriptorOf(e)
	last, commitsFresh := commits.LastCommit(e)
	sig := health.Signals{Archived: r.Archived, Deprecated: d.Lifecycle == "deprecated", Public: r.Visibility == "public",
		LastChange: r.PushedAt, LastCommit: last, License: r.License,
		Failing: failing(e.Fresh) || failing(commitsFresh)}
	sig.OpenIssues, sig.WithPulls = openIssues(e)
	sig.ReviewIdle, sig.OldUpdates = pulls.Waiting(e, health.UpdatesAfter, now)
	if at, before := commits.LastAuthored(s, r.Key(), commits.WorkTeam); !at.IsZero() {
		sig.LastChange, sig.Automated, sig.Before = at, before || last.After(at), before
	}
	if tag, at, ok := released(e); ok {
		sig.Release, sig.Released = tag, at
	}
	return health.Assess(sig, now)
}

// released is a repository's newest release: its GitHub release or the newest version of a package it publishes, whichever is
// newer (a package whose registry gives no date counts when there is no GitHub release).
func released(e *snapshot.RepoEntry) (string, time.Time, bool) {
	rel, _ := badges.ReleaseOf(e)
	pk := packages.Newest(e)
	switch {
	case pk != nil && (rel == nil || pk.Latest.Published.After(rel.Published)):
		return pk.Latest.Version, pk.Latest.Published, true
	case rel != nil:
		return rel.Tag, rel.Published, true
	}
	return "", time.Time{}, false
}

// weigh is the health of the repositories keys (def is the default repository) present in the snapshot.
func weigh(s *snapshot.Snapshot, keys []snapshot.RepoKey, def snapshot.RepoKey, now time.Time) health.Product {
	members := make([]health.Member, 0, len(keys))
	for _, k := range keys {
		if e := s.Repos[k]; e != nil {
			members = append(members, health.Member{Address: string(k), Default: def != "" && k == def, Health: assess(s, e, now)})
		}
	}
	return health.Weigh(members)
}

func groupHealth(s *snapshot.Snapshot, g snapshot.Group, now time.Time) health.Product {
	return weigh(s, g.Members, g.DefaultRepo, now)
}

func repoHealthAPI(s *snapshot.Snapshot, e *snapshot.RepoEntry, out *oapi.Repo) {
	r := assess(s, e, time.Now())
	if !r.Counted {
		return
	}
	h := &oapi.Health{Score: r.Score, Level: oapi.HealthLevel(r.Level), Findings: make([]oapi.Finding, 0, len(r.Findings))}
	for _, f := range r.Findings {
		h.Findings = append(h.Findings, oapi.Finding{Code: f.Code, Penalty: f.Penalty, Message: f.Message})
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
		out.Findings = append(out.Findings, oapi.RepoFinding{Repo: f.Repo, Code: f.Code, Penalty: f.Penalty,
			Message: f.Message})
	}
	return out
}

// dashboardHealthAPI adds the health of the products, their members and the fleet, each member's activity (by the last change of
// the work that counts, which the commits hook filled).
func dashboardHealthAPI(s *snapshot.Snapshot, out *oapi.Dashboard) {
	now := time.Now()
	for i, g := range s.Groups {
		out.Groups[i].Health = productAPI(groupHealth(s, g, now), dashboardFindings)
		for j := range out.Groups[i].Members {
			m := &out.Groups[i].Members[j]
			var last time.Time
			if m.LastAuthoredAt != nil {
				last = *m.LastAuthoredAt
			}
			m.Activity = oapi.ActivityLevel(health.ActivityOf(last, m.Archived, now))
			if e := s.Repos[snapshot.RepoKey(m.Address)]; e != nil {
				if r := assess(s, e, now); r.Counted {
					m.Level = new(oapi.HealthLevel(r.Level))
				}
			}
		}
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

// rulesAPI serves the rules the portal judges by (health.Describe, less a switched-off feature's) and the switches, so every page
// explains the signals in the server's terms and shows only the parts switched on.
type rulesAPI struct{ features config.Features }

func (a rulesAPI) GetRules(context.Context, oapi.GetRulesRequestObject) (oapi.GetRulesResponseObject, error) {
	return oapi.GetRules200JSONResponse{Body: rulesFor(a.features)}, nil
}

// rulesFor is the rules and the switches as /rules tells them.
func rulesFor(f config.Features) oapi.Rules {
	r := rulesOf(judged(f))
	r.Features.Pulls, r.Features.Issues = f.Pulls, f.Issues
	return r
}

// rulesOf is the rules in the contract's terms.
func rulesOf(r health.Rules) oapi.Rules {
	out := oapi.Rules{WindowDays: r.WindowDays, Levels: []oapi.LevelRule{}, Severities: []oapi.SeverityRule{},
		Activity: []oapi.ActivityRule{}, Findings: []oapi.FindingKind{}}
	for _, l := range r.Levels {
		out.Levels = append(out.Levels, oapi.LevelRule{Level: oapi.HealthLevel(l.Level), Label: l.Label, From: l.From})
	}
	for _, v := range r.Severities {
		out.Severities = append(out.Severities, oapi.SeverityRule{Severity: oapi.SeverityRuleSeverity(v.Severity), Label: v.Label, From: v.From})
	}
	for _, a := range r.Activity {
		rule := oapi.ActivityRule{Level: oapi.ActivityLevel(a.Activity), Label: a.Label, About: a.About}
		if a.WithinDays > 0 {
			rule.WithinDays = new(a.WithinDays)
		}
		out.Activity = append(out.Activity, rule)
	}
	for _, k := range r.Findings {
		kind := oapi.FindingKind{Code: k.Code, Label: k.Label, About: k.About, Penalties: make([]oapi.Penalty, 0, len(k.Penalties)),
			Action: oapi.FindingAction{Label: k.Action.Label, Path: k.Action.Path}}
		for _, p := range k.Penalties {
			kind.Penalties = append(kind.Penalties, oapi.Penalty{Points: p.Points, When: p.When})
		}
		out.Findings = append(out.Findings, kind)
	}
	return out
}
