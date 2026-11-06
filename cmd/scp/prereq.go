package main

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	gosync "sync"

	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/issues"
	"github.com/cantara/source-code-portal/internal/prereq"
	"github.com/cantara/source-code-portal/internal/pulls"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// prereqs checks what each feature needs of GitHub (design/prerequisites.md, P-71): an App's grants from its installations, a
// token's refusals from the jobs' items, and logs when the unmet ones change.
type prereqs struct {
	sources  []string
	source   func(id string) *github.Source
	current  func() *snapshot.Snapshot // the full snapshot, private repositories included
	webhooks bool
	needs    []prereq.Need // of the features switched on
	log      *slog.Logger

	mu   gosync.Mutex
	last string // the unmet ones last logged
}

// refusedShare is the share of a source's repositories whose items of a job GitHub must refuse (403) before the token is told to
// lack the job's permissions: one forbidden repository is that repository's own setting, most of them the credential's.
const refusedShare = 0.5

// freshnessOf is each checked job's freshness of a repository: the jobs prereq.Needs names whose facets cmd/scp can read.
var freshnessOf = map[string]func(*snapshot.RepoEntry) snapshot.Freshness{ //nolint:gochecknoglobals // a constant table
	"commits": func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := commits.Of(e); return f },
	"pulls":   func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := pulls.Of(e); return f },
	"issues":  func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := issues.Of(e); return f },
}

// check is the unmet prerequisites now, logged when they changed.
func (p *prereqs) check() []prereq.Unmet {
	s := p.current()
	var in []prereq.Source
	for _, id := range p.sources {
		src := p.source(id)
		if src == nil {
			continue
		}
		ps := prereq.Source{ID: id, App: src.App()}
		if ps.App {
			ps.Grants, ps.Known = src.Grants()
		} else {
			ps.Refused = refused(s, id)
		}
		in = append(in, ps)
	}
	unmet := prereq.Check(p.needs, in, p.webhooks)
	p.logChange(unmet)
	return unmet
}

// refused is the jobs GitHub refused (403) for most of a source's repositories.
func refused(s *snapshot.Snapshot, source string) map[string]bool {
	if s == nil {
		return nil
	}
	total, forbidden := 0, map[string]int{}
	for _, e := range s.Repos {
		if e.Repo.ID.Source != source {
			continue
		}
		total++
		for job, of := range freshnessOf {
			if of(e).ErrCode == "forbidden" {
				forbidden[job]++
			}
		}
	}
	out := map[string]bool{}
	for job, n := range forbidden {
		if float64(n) >= refusedShare*float64(total) {
			out[job] = true
		}
	}
	return out
}

func (p *prereqs) logChange(unmet []prereq.Unmet) {
	lines := warnings(unmet)
	key := strings.Join(lines, "\n")
	p.mu.Lock()
	changed := key != p.last
	p.last = key
	p.mu.Unlock()
	if !changed {
		return
	}
	if len(lines) == 0 {
		p.log.Info("prerequisites hold", "features", len(p.needs))
		return
	}
	for _, u := range unmet {
		p.log.Warn("prerequisites unmet", "feature", u.Feature, "source", u.Source, "owners", u.Owners, "permissions", u.Permissions,
			"events", u.Events, "refused", u.Refused, "fix", u.Fix())
	}
}

// warnings are the unmet prerequisites as the status's warnings.
func warnings(unmet []prereq.Unmet) []string {
	out := make([]string, 0, len(unmet))
	for _, u := range unmet {
		out = append(out, u.Title+" ("+u.Source+"): "+u.Effect+" "+u.Fix())
	}
	return out
}

// GetPrerequisites serves the unmet prerequisites: the feature and its effect to everyone, what is missing and the fix to
// administrators.
func (p *prereqs) GetPrerequisites(ctx context.Context, _ oapi.GetPrerequisitesRequestObject) (oapi.GetPrerequisitesResponseObject, error) {
	admin := httpapi.ViewerOf(ctx).Admin
	list := oapi.PrerequisiteList{Unmet: []oapi.Prerequisite{}}
	seen := map[prereq.Feature]bool{}
	for _, u := range p.check() {
		if !admin {
			if seen[u.Feature] { // told once per feature, whatever the sources
				continue
			}
			seen[u.Feature] = true
			list.Unmet = append(list.Unmet, oapi.Prerequisite{Feature: oapi.PrerequisiteFeature(u.Feature), Title: u.Title, Effect: u.Effect})
			continue
		}
		perms := names(u.Permissions, prereq.PermissionName)
		events := names(u.Events, prereq.EventName)
		list.Unmet = append(list.Unmet, oapi.Prerequisite{Feature: oapi.PrerequisiteFeature(u.Feature), Title: u.Title, Effect: u.Effect,
			Source: new(u.Source), Owners: new(slices.Clone(u.Owners)), Permissions: &perms, Events: &events, Refused: new(u.Refused),
			Fix: new(u.Fix())})
	}
	return oapi.GetPrerequisites200JSONResponse(list), nil
}

func names(xs []string, name func(string) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = name(x)
	}
	return out
}
