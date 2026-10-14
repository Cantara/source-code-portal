package manifests

import (
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Enabled reports manifests.enabled. It is part of the consumer interfaces of overview (ManifestsSource) and badges (Deps.Enabled), both
// satisfied through cmd/scp/internal/bridge.
func (s *Service) Enabled() bool { return s.cfg.Enabled }

// TechnologyView is the digest of one repository's manifest facts for the repository header (unit 07 schema TechnologySummary). The bridge to
// overview adds the portal link and the contract's byte counts. Counts cover primary units only (fixtures and examples are excluded).
type TechnologyView struct {
	Info                 freshness.Info // of the facts value (freshness of the "manifests" section)
	Languages            []LangShare    // primary first
	Ecosystems           []string       // sorted
	BuildTools           []string       // sorted
	Toolchain            map[string]string
	Units                int
	DirectDependencies   int
	InternalDependencies int // edges to artifacts produced by tracked units the viewer's class may see
	Diagnostics          int // repository-level plus unit-level
	ParsedAt             time.Time
}

// Technology returns the digest of one repository, or false when the repository has no facts yet. class selects the graph variant that decides
// which internal dependencies exist (a viewer never counts a producer it cannot see).
func (s *Service) Technology(snap *readmodel.Snapshot, slug scm.Slug, class scm.ViewClass) (TechnologyView, bool) {
	f, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.Slots.Facts)
	if !ok || !f.Info.HasValue() {
		return TechnologyView{}, false
	}
	var g *Graph
	if gg, ok := readmodel.GetGlobal(snap, s.d.Slots.Graph); ok {
		g = gg.For(class)
	}
	ri, haveRepo := RepoIdx(0), false
	if g != nil {
		ri, haveRepo = g.RepoOf(slug)
	}
	v := TechnologyView{Info: f.Info, Toolchain: f.V.Technology.Toolchain, ParsedAt: f.V.ParsedAt, Diagnostics: len(f.V.Diagnostics)}
	primary := map[string]int{}
	for ui, u := range f.V.Units {
		v.Diagnostics += len(u.Diagnostics)
		if u.Role != RolePrimary {
			continue
		}
		v.Units++
		v.DirectDependencies += len(u.Dependencies)
		if haveRepo {
			v.InternalDependencies += unitInternal(g, ri, ui)
		}
		if sp, ok := s.d.Registry.Spec(u.Ecosystem); ok && sp.Language != "" {
			primary[sp.Language]++
		}
	}
	v.Languages = languageShares(f.V.Technology.Languages, primary)
	v.BuildTools = f.V.Technology.BuildTools
	for _, e := range f.V.Technology.Ecosystems {
		v.Ecosystems = append(v.Ecosystems, string(e))
	}
	return v, true
}

// Health is what the operator status page reads (ops.ManifestsView, bridged).
type Health struct {
	Enabled                     bool
	RepositoriesWithDiagnostics int
	DisabledParsers             []string // parser ids whose circuit is open (disabled until restart)
	DanglingUnits               int      // graph units whose repository is no longer in the snapshot
	StaleRepos                  int
}

// Health summarises the feature from one snapshot. It is the same computation the observable gauges use.
func (s *Service) Health(snap *readmodel.Snapshot, now time.Time) Health {
	h, _ := s.health(snap, now)
	return h
}

func (s *Service) health(snap *readmodel.Snapshot, now time.Time) (Health, map[EcosystemID]int64) {
	h := Health{Enabled: s.cfg.Enabled}
	units := map[EcosystemID]int64{}
	live := map[scm.Slug]bool{}
	for _, id := range snap.Repos() {
		live[scm.Slug(id)] = true
		f, ok := readmodel.Get(snap, id, s.d.Slots.Facts)
		if !ok {
			continue
		}
		if len(f.V.Diagnostics) > 0 || hasUnitDiagnostics(f.V.Units) {
			h.RepositoriesWithDiagnostics++
		}
		for _, u := range f.V.Units {
			units[u.Ecosystem]++
		}
		if f.Info.Derive(now) != freshness.StatusFresh {
			h.StaleRepos++
		}
	}
	if g, ok := readmodel.GetGlobal(snap, s.d.Slots.Graph); ok && g.All != nil {
		for i := 0; i < g.All.NumUnits(); i++ {
			if !live[g.All.Repo(g.All.Unit(UnitIdx(i)).Repo).Slug] {
				h.DanglingUnits++
			}
		}
	}
	for _, id := range s.d.Registry.ParserIDs() {
		if s.wd.Open(id) {
			h.DisabledParsers = append(h.DisabledParsers, id)
		}
	}
	return h, units
}

func hasUnitDiagnostics(us []Unit) bool {
	for _, u := range us {
		if len(u.Diagnostics) > 0 {
			return true
		}
	}
	return false
}

// Related lists the repositories the given one depends on and the repositories that depend on it, from the viewer's class of the graph (sorted by
// slug). overview merges dependsOn with the descriptor's declared edges (architecture 14.2, provenance kept by the caller).
func (s *Service) Related(snap *readmodel.Snapshot, slug scm.Slug, class scm.ViewClass) (dependsOn, usedBy []scm.Slug) {
	gg, ok := readmodel.GetGlobal(snap, s.d.Slots.Graph)
	if !ok {
		return nil, nil
	}
	g := gg.For(class)
	return g.DependsOnRepos(slug), g.UsedByRepos(slug)
}
