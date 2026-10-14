package catalog

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves groups and repositories from the current snapshot. It implements the catalogue's methods of the strict server. What
// other features show on these answers they add through the hooks, wired in cmd/scp.
type API struct {
	Current   func(context.Context) *snapshot.Snapshot // the viewer's view
	Dashboard []func(*snapshot.Snapshot, *oapi.Dashboard)
	Repo      []func(*snapshot.RepoEntry, *oapi.Repo)
	Detail    []func(*snapshot.RepoEntry, *oapi.RepoDetail)
}

func notFound(detail string) oapi.NotFoundApplicationProblemPlusJSONResponse {
	return oapi.NotFoundApplicationProblemPlusJSONResponse(oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", detail))
}

func (a API) GetDashboard(ctx context.Context, _ oapi.GetDashboardRequestObject) (oapi.GetDashboardResponseObject, error) {
	s := a.Current(ctx)
	out := oapi.Dashboard{Groups: make([]oapi.DashboardGroup, 0, len(s.Groups)), RepoCount: len(s.Repos), Latest: []oapi.Commit{}}
	for _, g := range s.Groups {
		def, _ := defaultRepo(g)
		out.Groups = append(out.Groups, oapi.DashboardGroup{Id: g.ID, Name: g.Name, Description: g.Description, DefaultRepo: def,
			RepoCount: len(g.Members)})
	}
	for k := range s.Repos {
		if len(s.GroupsOf(k)) == 0 {
			out.UngroupedCount++
		}
	}
	for _, fn := range a.Dashboard {
		fn(s, &out)
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
		DefaultRepoState: oapi.GroupDefaultRepoState(state), RepoCount: len(g.Members), Repos: a.repos(s, g.Members, time.Now())}}, nil
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
	return oapi.ListRepos200JSONResponse{Body: oapi.RepoList{Repos: a.repos(s, keys, time.Now())}}, nil
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
	return oapi.GetRepo200JSONResponse{Body: a.detail(s, e, time.Now())}, nil
}

func (a API) detail(s *snapshot.Snapshot, e *snapshot.RepoEntry, now time.Time) oapi.RepoDetail {
	d, fresh := DescriptorOf(e)
	out := oapi.RepoDetail{Repo: a.repo(s, e, now), Title: e.Repo.Name, Commits: []oapi.Commit{},
		CommitsFreshness: oapi.FreshnessOf(snapshot.Freshness{}, now),
		Descriptor: oapi.Descriptor{Present: d.Present, File: d.File, Title: d.Title, Description: d.Description,
			Lifecycle: oapi.DescriptorLifecycle(d.Lifecycle), Owners: nonNil(d.Owners), Tags: nonNil(d.Tags),
			Links: make([]oapi.Link, 0, len(d.Links)), Problems: make([]oapi.FileProblem, 0, len(d.Problems)),
			Freshness: oapi.FreshnessOf(fresh, now)}}
	if d.Title != "" {
		out.Title = d.Title
	}
	for _, l := range d.Links {
		out.Descriptor.Links = append(out.Descriptor.Links, oapi.Link{Title: l.Title, Url: l.URL})
	}
	for _, p := range d.Problems {
		out.Descriptor.Problems = append(out.Descriptor.Problems, oapi.FileProblem{Pointer: p.Pointer, Code: p.Code, Message: p.Message})
	}
	for _, fn := range a.Detail {
		fn(e, &out)
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

func (a API) repos(s *snapshot.Snapshot, keys []snapshot.RepoKey, now time.Time) []oapi.Repo {
	out := make([]oapi.Repo, 0, len(keys))
	for _, k := range keys {
		if e := s.Repos[k]; e != nil {
			out = append(out, a.repo(s, e, now))
		}
	}
	return out
}

func (a API) repo(s *snapshot.Snapshot, e *snapshot.RepoEntry, now time.Time) oapi.Repo {
	r := e.Repo
	out := oapi.Repo{
		Address: string(r.Key()), Owner: r.Owner, Name: r.Name, Description: r.Description, HtmlUrl: r.HTMLURL, Homepage: r.Homepage,
		DefaultBranch: r.DefaultBranch, License: r.License, Visibility: r.Visibility, Topics: nonNil(r.Topics), Archived: r.Archived,
		Fork: r.Fork, Stars: r.Stars, OpenIssues: r.OpenIssues, PushedAt: r.PushedAt.UTC(), Groups: nonNil(s.GroupsOf(r.Key())),
		Freshness: oapi.FreshnessOf(e.Fresh, now),
	}
	for _, fn := range a.Repo {
		fn(e, &out)
	}
	return out
}

func nonNil(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}
