package catalog

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves groups and repositories from the current snapshot. It implements the catalogue's methods of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
}

// dashboardCommits is how many of the latest commits the dashboard shows.
const dashboardCommits = 10

func notFound(detail string) oapi.NotFoundApplicationProblemPlusJSONResponse {
	return oapi.NotFoundApplicationProblemPlusJSONResponse(oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", detail))
}

func (a API) GetDashboard(ctx context.Context, _ oapi.GetDashboardRequestObject) (oapi.GetDashboardResponseObject, error) {
	s, now := a.Current(ctx), time.Now()
	out := oapi.Dashboard{Groups: make([]oapi.DashboardGroup, 0, len(s.Groups)), RepoCount: len(s.Repos),
		Latest: make([]oapi.Commit, 0, dashboardCommits)}
	for _, g := range s.Groups {
		def, _ := defaultRepo(g)
		dg := oapi.DashboardGroup{Id: g.ID, Name: g.Name, Description: g.Description, DefaultRepo: def, RepoCount: len(g.Members)}
		if feed, _ := s.Feed(g.ID); len(feed) > 0 {
			dg.LastActivity = oapi.TimeOrNil(feed[0].Commit.CommittedAt)
		}
		out.Groups = append(out.Groups, dg)
	}
	for k, e := range s.Repos {
		if len(s.GroupsOf(k)) == 0 {
			out.UngroupedCount++
		}
		switch e.CommitsFresh.Derive(now) {
		case snapshot.StatusFresh:
			out.Freshness.Fresh++
		case snapshot.StatusStale:
			out.Freshness.Stale++
		case snapshot.StatusError:
			out.Freshness.Error++
		default:
			out.Freshness.Unknown++
		}
	}
	for _, r := range s.Latest[:min(dashboardCommits, len(s.Latest))] {
		out.Latest = append(out.Latest, oapi.CommitOf(r.Repo, r.Commit))
	}
	return oapi.GetDashboard200JSONResponse{Body: out}, nil
}

func (a API) ListGroups(ctx context.Context, _ oapi.ListGroupsRequestObject) (oapi.ListGroupsResponseObject, error) {
	s := a.Current(ctx)
	out := oapi.GroupList{Groups: make([]oapi.GroupSummary, 0, len(s.Groups))}
	for _, g := range s.Groups {
		def, state := defaultRepo(g)
		out.Groups = append(out.Groups, oapi.GroupSummary{Id: g.ID, Name: g.Name, Description: g.Description, DefaultRepo: def,
			DefaultRepoState: oapi.GroupSummaryDefaultRepoState(state), RepoCount: len(g.Members)})
	}
	return oapi.ListGroups200JSONResponse{Body: out}, nil
}

func (a API) GetGroup(ctx context.Context, req oapi.GetGroupRequestObject) (oapi.GetGroupResponseObject, error) {
	s := a.Current(ctx)
	g, ok := s.Group(req.Group)
	if !ok {
		return oapi.GetGroup404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such group")}, nil
	}
	def, state := defaultRepo(g)
	return oapi.GetGroup200JSONResponse{Body: oapi.Group{Id: g.ID, Name: g.Name, Description: g.Description, DefaultRepo: def,
		DefaultRepoState: oapi.GroupDefaultRepoState(state), RepoCount: len(g.Members), Repos: repos(s, g.Members, time.Now())}}, nil
}

func (a API) ListRepos(ctx context.Context, req oapi.ListReposRequestObject) (oapi.ListReposResponseObject, error) {
	s := a.Current(ctx)
	var keys []snapshot.RepoKey
	if req.Params.Group != nil {
		g, ok := s.Group(*req.Params.Group)
		if !ok {
			return oapi.ListRepos404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such group")}, nil
		}
		keys = g.Members
	} else {
		for k := range s.Repos {
			keys = append(keys, k)
		}
		slices.Sort(keys)
	}
	return oapi.ListRepos200JSONResponse{Body: oapi.RepoList{Repos: repos(s, keys, time.Now())}}, nil
}

func (a API) GetRepo(ctx context.Context, req oapi.GetRepoRequestObject) (oapi.GetRepoResponseObject, error) {
	s := a.Current(ctx)
	e, moved := s.Repo(snapshot.KeyOf(req.Owner, req.Name))
	switch {
	case e == nil:
		return oapi.GetRepo404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such repository")}, nil
	case moved:
		return oapi.GetRepo308Response{Headers: oapi.MovedResponseHeaders{Location: "/api/v1/repos/" + string(e.Repo.Key())}}, nil
	}
	return oapi.GetRepo200JSONResponse{Body: detail(s, e, time.Now())}, nil
}

func detail(s *snapshot.Snapshot, e *snapshot.RepoEntry, now time.Time) oapi.RepoDetail {
	d := e.Descriptor
	out := oapi.RepoDetail{Repo: repo(s, e, now), Title: e.Repo.Name, Commits: make([]oapi.Commit, 0, len(e.Commits)),
		CommitsFreshness: oapi.FreshnessOf(e.CommitsFresh, now),
		Descriptor: oapi.Descriptor{Present: d.Present, File: d.File, Title: d.Title, Description: d.Description,
			Lifecycle: oapi.DescriptorLifecycle(d.Lifecycle), Owners: nonNil(d.Owners), Tags: nonNil(d.Tags),
			Links: make([]oapi.Link, 0, len(d.Links)), Problems: make([]oapi.FileProblem, 0, len(d.Problems)),
			Freshness: oapi.FreshnessOf(e.DescriptorFresh, now)}}
	if d.Title != "" {
		out.Title = d.Title
	}
	for _, l := range d.Links {
		out.Descriptor.Links = append(out.Descriptor.Links, oapi.Link{Title: l.Title, Url: l.URL})
	}
	for _, p := range d.Problems {
		out.Descriptor.Problems = append(out.Descriptor.Problems, oapi.FileProblem{Pointer: p.Pointer, Code: p.Code, Message: p.Message})
	}
	key := e.Repo.Key()
	for i := range e.Commits {
		out.Commits = append(out.Commits, oapi.CommitOf(key, &e.Commits[i]))
	}
	return out
}

func defaultRepo(g snapshot.Group) (*string, string) {
	switch {
	case g.DefaultMissing:
		return nil, "missing"
	case g.DefaultRepo == "": // private and hidden from this viewer
		return nil, "ok"
	}
	d := string(g.DefaultRepo)
	return &d, "ok"
}

func repos(s *snapshot.Snapshot, keys []snapshot.RepoKey, now time.Time) []oapi.Repo {
	out := make([]oapi.Repo, 0, len(keys))
	for _, k := range keys {
		if e := s.Repos[k]; e != nil {
			out = append(out, repo(s, e, now))
		}
	}
	return out
}

func repo(s *snapshot.Snapshot, e *snapshot.RepoEntry, now time.Time) oapi.Repo {
	r := e.Repo
	return oapi.Repo{
		Address: string(r.Key()), Owner: r.Owner, Name: r.Name, Description: r.Description, HtmlUrl: r.HTMLURL, Homepage: r.Homepage,
		DefaultBranch: r.DefaultBranch, License: r.License, Visibility: r.Visibility, Topics: nonNil(r.Topics), Archived: r.Archived,
		Fork: r.Fork, Stars: r.Stars, OpenIssues: r.OpenIssues, PushedAt: r.PushedAt.UTC(), Groups: nonNil(s.GroupsOf(r.Key())),
		Freshness: oapi.FreshnessOf(e.Fresh, now),
	}
}

func nonNil(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}
