package manifests

import (
	"maps"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Manifest is one package manifest of a repository (go.mod, package.json, pom.xml) with what it declares.
type Manifest struct {
	Path         string             `json:"path"`
	Ecosystem    string             `json:"ecosystem"` // go | npm | maven
	Name         string             `json:"name"`      // Go module path, npm package name, Maven groupId:artifactId
	Version      string             `json:"version,omitempty"`
	Language     string             `json:"language"`
	BuildTool    string             `json:"buildTool"`
	Toolchain    []Toolchain        `json:"toolchain,omitempty"`
	Dependencies []Dependency       `json:"dependencies,omitempty"`
	Problems     []snapshot.Problem `json:"problems,omitempty"`
}

// Toolchain is a toolchain version a manifest states.
type Toolchain struct {
	Name    string `json:"name"` // go | node | java
	Version string `json:"version"`
}

// Dependency is one declared dependency.
type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"` // the requirement as written
	Scope   string `json:"scope"`             // runtime dev peer optional provided test indirect tool parent import
}

// pkg is a package name within its ecosystem.
type pkg struct{ ecosystem, name string }

// graph is the dependency index: who declares a package name, and who depends on whom.
type graph struct {
	providers map[pkg][]snapshot.RepoKey // sorted
	usedBy    map[snapshot.RepoKey][]snapshot.RepoKey
}

const (
	facet      snapshot.Facet[[]Manifest] = "manifests" // by path
	graphIndex snapshot.Index[graph]      = "manifests"
)

// Of returns the manifests of e, by path, with their freshness.
func Of(e *snapshot.RepoEntry) ([]Manifest, snapshot.Freshness) { return facet.Get(e) }

// providers returns the repositories with a manifest that declares the package name in ecosystem, sorted.
func providers(s *snapshot.Snapshot, ecosystem, name string) []snapshot.RepoKey {
	return graphIndex.Get(s).providers[pkg{ecosystem, name}]
}

// usedBy returns the repositories that declare a dependency on a package key provides, sorted.
func usedBy(s *snapshot.Snapshot, key snapshot.RepoKey) []snapshot.RepoKey {
	return graphIndex.Get(s).usedBy[key]
}

// Graph is the indexer of the dependencies between the repositories, for snapshot.NewPublisher.
func Graph() snapshot.Indexer {
	return graphIndex.Build(func(s *snapshot.Snapshot) graph {
		keys := slices.Sorted(maps.Keys(s.Repos))
		g := graph{providers: map[pkg][]snapshot.RepoKey{}}
		for _, k := range keys {
			ms, _ := Of(s.Repos[k])
			for _, m := range ms {
				if p := (pkg{m.Ecosystem, m.Name}); m.Name != "" && !slices.Contains(g.providers[p], k) {
					g.providers[p] = append(g.providers[p], k)
				}
			}
		}
		users := map[snapshot.RepoKey]map[snapshot.RepoKey]bool{}
		for _, k := range keys {
			ms, _ := Of(s.Repos[k])
			for _, m := range ms {
				for _, d := range m.Dependencies {
					for _, p := range g.providers[pkg{m.Ecosystem, d.Name}] {
						if p != k {
							if users[p] == nil {
								users[p] = map[snapshot.RepoKey]bool{}
							}
							users[p][k] = true
						}
					}
				}
			}
		}
		g.usedBy = make(map[snapshot.RepoKey][]snapshot.RepoKey, len(users))
		for p, us := range users {
			g.usedBy[p] = slices.Sorted(maps.Keys(us))
		}
		return g
	})
}

// Repo adds the language of the first manifest that names one to a repository summary.
func Repo(e *snapshot.RepoEntry, r *oapi.Repo) {
	ms, _ := Of(e)
	for _, m := range ms {
		if m.Language != "" {
			r.Language = m.Language
			return
		}
	}
}

// Summary is what the manifests of a repository declare, for the Markdown surface.
type Summary struct {
	Technology   oapi.Technology
	Uses, UsedBy []snapshot.RepoKey // the other catalogued repositories it depends on and that depend on it, sorted
}

// Summarise sums up the manifests of e in s.
func Summarise(s *snapshot.Snapshot, e *snapshot.RepoEntry) Summary {
	v := view(s, e, time.Now())
	out := Summary{Technology: v.Technology, UsedBy: usedBy(s, e.Repo.Key())}
	for _, u := range v.Uses {
		out.Uses = append(out.Uses, snapshot.RepoKey(u.Address))
	}
	return out
}
