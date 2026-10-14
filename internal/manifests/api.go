package manifests

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves the manifests from the current snapshot. It implements the manifests method of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
}

func (a API) GetManifests(ctx context.Context, req oapi.GetManifestsRequestObject) (oapi.GetManifestsResponseObject, error) {
	s := a.Current(ctx)
	e, moved := s.Repo(snapshot.KeyOf(req.Owner, req.Name))
	switch {
	case e == nil:
		return oapi.GetManifests404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
			oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", "no such repository"))}, nil
	case moved:
		return oapi.GetManifests308Response{Headers: oapi.MovedResponseHeaders{Location: "/api/v1/repos/" + string(e.Repo.Key()) + "/manifests"}}, nil
	}
	return oapi.GetManifests200JSONResponse{Body: view(s, e, time.Now())}, nil
}

// view is the manifests answer for the entry e of s.
func view(s *snapshot.Snapshot, e *snapshot.RepoEntry, now time.Time) oapi.Manifests {
	self := e.Repo.Key()
	ms, fresh := Of(e)
	out := oapi.Manifests{Manifests: make([]oapi.Manifest, 0, len(ms)), Freshness: oapi.FreshnessOf(fresh, now),
		Technology: technology(ms), Uses: []oapi.RepoLink{}, UsedBy: []oapi.RepoLink{}}
	uses := map[snapshot.RepoKey][]string{}
	for _, m := range ms {
		om := oapi.Manifest{Path: m.Path, Ecosystem: oapi.ManifestEcosystem(m.Ecosystem), Name: m.Name, Version: m.Version, Language: m.Language,
			BuildTool: m.BuildTool, Toolchain: make([]oapi.Toolchain, 0, len(m.Toolchain)), Dependencies: make([]oapi.Dependency, 0, len(m.Dependencies)),
			Problems: make([]oapi.FileProblem, 0, len(m.Problems))}
		for _, t := range m.Toolchain {
			om.Toolchain = append(om.Toolchain, oapi.Toolchain{Name: oapi.ToolchainName(t.Name), Version: t.Version})
		}
		for _, d := range m.Dependencies {
			repos := []string{}
			for _, p := range providers(s, m.Ecosystem, d.Name) {
				if p != self {
					repos = append(repos, string(p))
					if !slices.Contains(uses[p], d.Name) {
						uses[p] = append(uses[p], d.Name)
					}
				}
			}
			om.Dependencies = append(om.Dependencies, oapi.Dependency{Name: d.Name, Version: d.Version, Scope: oapi.DependencyScope(d.Scope), Repos: repos})
		}
		for _, p := range m.Problems {
			om.Problems = append(om.Problems, oapi.FileProblem{Pointer: p.Pointer, Code: p.Code, Message: p.Message})
		}
		out.Manifests = append(out.Manifests, om)
	}
	for _, k := range slices.Sorted(maps.Keys(uses)) {
		out.Uses = append(out.Uses, link(s, k, uses[k]))
	}
	for _, k := range usedBy(s, self) {
		u, _ := s.Repo(k)
		if u == nil {
			continue
		}
		var via []string
		ums, _ := Of(u)
		for _, m := range ums {
			for _, d := range m.Dependencies {
				if slices.Contains(providers(s, m.Ecosystem, d.Name), self) && !slices.Contains(via, d.Name) {
					via = append(via, d.Name)
				}
			}
		}
		out.UsedBy = append(out.UsedBy, link(s, k, via))
	}
	return out
}

func link(s *snapshot.Snapshot, k snapshot.RepoKey, via []string) oapi.RepoLink {
	slices.Sort(via)
	l := oapi.RepoLink{Address: string(k), Via: via}
	if e, _ := s.Repo(k); e != nil {
		l.Owner, l.Name = e.Repo.Owner, e.Repo.Name
	}
	return l
}

// technology sums up the languages, build tools and toolchain versions of the manifests, in order of first appearance.
func technology(ms []Manifest) oapi.Technology {
	t := oapi.Technology{Languages: []string{}, BuildTools: []string{}, Toolchains: []oapi.ToolchainVersions{}}
	for _, m := range ms {
		if m.Language != "" && !slices.Contains(t.Languages, m.Language) {
			t.Languages = append(t.Languages, m.Language)
		}
		if m.BuildTool != "" && !slices.Contains(t.BuildTools, m.BuildTool) {
			t.BuildTools = append(t.BuildTools, m.BuildTool)
		}
		for _, tc := range m.Toolchain {
			i := slices.IndexFunc(t.Toolchains, func(v oapi.ToolchainVersions) bool { return string(v.Name) == tc.Name })
			if i < 0 {
				t.Toolchains = append(t.Toolchains, oapi.ToolchainVersions{Name: oapi.ToolchainVersionsName(tc.Name), Versions: []string{}})
				i = len(t.Toolchains) - 1
			}
			if !slices.Contains(t.Toolchains[i].Versions, tc.Version) {
				t.Toolchains[i].Versions = append(t.Toolchains[i].Versions, tc.Version)
			}
		}
	}
	return t
}
