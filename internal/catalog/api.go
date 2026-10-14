package catalog

import (
	"context"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves groups and repositories from the current snapshot. It implements the catalogue's methods of the strict server.
type API struct {
	Current func() *snapshot.Snapshot
}

func notFound(detail string) oapi.NotFoundApplicationProblemPlusJSONResponse {
	return oapi.NotFoundApplicationProblemPlusJSONResponse{Type: "urn:scp:problem:not-found", Title: "Not Found", Status: 404,
		Code: "not-found", Detail: &detail}
}

func (a API) ListGroups(context.Context, oapi.ListGroupsRequestObject) (oapi.ListGroupsResponseObject, error) {
	s := a.Current()
	out := oapi.GroupList{Groups: make([]oapi.GroupSummary, 0, len(s.Groups))}
	for _, g := range s.Groups {
		def, state := defaultRepo(g)
		out.Groups = append(out.Groups, oapi.GroupSummary{Id: g.ID, Name: g.Name, Description: g.Description, DefaultRepo: def,
			DefaultRepoState: oapi.GroupSummaryDefaultRepoState(state), RepoCount: len(g.Members)})
	}
	return oapi.ListGroups200JSONResponse{Body: out}, nil
}

func (a API) GetGroup(_ context.Context, req oapi.GetGroupRequestObject) (oapi.GetGroupResponseObject, error) {
	s := a.Current()
	g, ok := s.Group(req.Group)
	if !ok {
		return oapi.GetGroup404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such group")}, nil
	}
	def, state := defaultRepo(g)
	return oapi.GetGroup200JSONResponse{Body: oapi.Group{Id: g.ID, Name: g.Name, Description: g.Description, DefaultRepo: def,
		DefaultRepoState: oapi.GroupDefaultRepoState(state), RepoCount: len(g.Members), Repos: repos(s, g.Members, time.Now())}}, nil
}

func (a API) ListRepos(_ context.Context, req oapi.ListReposRequestObject) (oapi.ListReposResponseObject, error) {
	s := a.Current()
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

func (a API) GetRepo(_ context.Context, req oapi.GetRepoRequestObject) (oapi.GetRepoResponseObject, error) {
	s := a.Current()
	e, moved := s.Repo(snapshot.KeyOf(req.Owner, req.Name))
	switch {
	case e == nil:
		return oapi.GetRepo404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such repository")}, nil
	case moved:
		return oapi.GetRepo308Response{Headers: oapi.GetRepo308ResponseHeaders{Location: "/api/v1/repos/" + string(e.Repo.Key())}}, nil
	}
	return oapi.GetRepo200JSONResponse{Body: repo(s, e, time.Now())}, nil
}

func defaultRepo(g snapshot.Group) (*string, string) {
	if g.DefaultMissing {
		return nil, "missing"
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
