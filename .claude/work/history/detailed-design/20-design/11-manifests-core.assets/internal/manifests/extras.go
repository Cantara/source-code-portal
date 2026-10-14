package manifests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/manifests/internal/version"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// WarmItem decodes one persisted `manifests.facts` row at start-up into the slot value. cmd/scp calls it from the Store.LoadItems callback
// (which must not call the store) and writes the result with one Publisher.Apply. A row it cannot read (unknown schema, corrupt JSON) is an error
// the caller logs and skips: the row is rebuilt by the first refresh, SQLite being a rebuildable cache.
func (s *Service) WarmItem(it store.Item) (freshness.Value[RepoFacts], error) {
	v, err := s.warmItem(it)
	switch {
	case err == nil:
		s.m.inc(s.m.warmTotal, "result", "ok")
	case errors.Is(err, ErrNoFacts):
		s.m.inc(s.m.warmTotal, "result", "no_payload")
	default:
		s.m.inc(s.m.warmTotal, "result", "undecodable")
	}
	return v, err
}

// ErrNoFacts is returned by WarmItem for a row that has no payload: the store creates such a row when the very first attempt for a repository failed
// (RecordFailure). The caller skips it silently; the planner treats the item as due.
var ErrNoFacts = errors.New("manifests: the row holds no facts yet (only a failed first attempt)")

func (s *Service) warmItem(it store.Item) (freshness.Value[RepoFacts], error) {
	if it.Key.Kind != ItemKind {
		return freshness.Value[RepoFacts]{}, fmt.Errorf("manifests: not a %s row: %q", ItemKind, it.Key.Kind)
	}
	if len(it.Payload) == 0 {
		return freshness.Value[RepoFacts]{}, ErrNoFacts
	}
	f, err := DecodeFacts(it.Payload)
	if err != nil {
		return freshness.Value[RepoFacts]{}, fmt.Errorf("manifests: warm %s/%s/%s: %w", it.Key.Repo.Source, it.Key.Repo.Owner, it.Key.Repo.Name, err)
	}
	f.Units = cloneUnitsExact(f.Units)
	info := freshness.Info{State: it.State, FetchedAt: it.FetchedAt, CheckedAt: it.CheckedAt, Attempts: it.Attempts, ErrCode: it.Error, ContentSHA: it.ContentSHA, TTL: s.cfg.Refresh.IdleBackoffMax}
	if it.Attempts > 0 && it.Error != "" { // the store keeps the result state while a good value exists (RecordFailure); in memory the same situation is StateError
		info.State = freshness.StateError
	}
	return freshness.Value[RepoFacts]{V: f, Info: info}, nil
}

// BadgeFacts is what the badges feature reads through its own consumer interface (bridged in cmd/scp: badges.RepoFacts is filled from it).
type BadgeFacts struct {
	Info          freshness.Info // of the facts value: the bridge copies it into every freshness.Value it builds (unit 09 section 2.4)
	Recognised    bool           // at least one unit was found; false maps to state "unsupported" in the bridge
	Languages     []string       // primary first
	BuildTool     string         // "" when unknown
	LatestVersion string         // highest version of the primary unit's artifact, "" when unknown
	InternalDeps  int            // distinct internal components the repository depends on
	Stale         bool
}

// BadgeView computes the badge facts of one repository from the snapshot. The caller passes the ViewClass of the request so the internal count
// never includes components produced by repositories the viewer cannot see.
func (s *Service) BadgeView(snap *readmodel.Snapshot, slug scm.Slug, class scm.ViewClass) (BadgeFacts, bool) {
	f, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.Slots.Facts)
	if !ok || !f.Info.HasValue() {
		return BadgeFacts{}, false
	}
	b := BadgeFacts{Info: f.Info, Recognised: len(f.V.Units) > 0, Languages: f.V.Technology.Languages, Stale: f.Info.Derive(time.Now()) != freshness.StatusFresh}
	if len(f.V.Technology.BuildTools) > 0 {
		b.BuildTool = f.V.Technology.BuildTools[0]
	}
	var vers []string
	var sys version.System
	for _, u := range f.V.Units {
		if u.Role != RolePrimary {
			continue
		}
		for _, a := range u.Provides {
			if a.Version != "" && !strings.Contains(a.Version, "${") {
				vers, sys = append(vers, a.Version), version.Of(string(a.ID.Ecosystem))
			}
		}
		break // the root-most unit (Units are sorted with "." first) names the repository's version
	}
	if v, ok := version.Max(sys, vers); ok {
		b.LatestVersion = v
	}
	if g, ok := readmodel.GetGlobal(snap, s.d.Slots.Graph); ok {
		if gr := g.For(class); gr != nil {
			if ri, ok := gr.RepoOf(slug); ok {
				seen := map[CompIdx]struct{}{}
				for ui := gr.repoStart[ri]; ui < gr.repoStart[ri+1]; ui++ {
					for _, e := range gr.Edges(ui) {
						if e.Flags&EdgeInternal != 0 {
							seen[e.Comp] = struct{}{}
						}
					}
				}
				b.InternalDeps = len(seen)
			}
		}
	}
	return b, true
}

// PomSource fetches a POM from a public Maven repository for parent and BOM resolution outside the organisation (opt-in, manifests.pom_source.enabled;
// architecture 13.2). The satisfier lives in cmd/scp (a hardened HTTP client restricted to manifests.pom_source.hosts); this package never opens a
// connection. The caller (the Maven assembler's lookup closure) asks PrivacyGuard.Check first and never calls Fetch for a private coordinate.
type PomSource interface {
	Fetch(ctx context.Context, id ComponentID, version string) ([]byte, error)
}

// ErrPrivateCoordinate is returned by PrivacyGuard.Check for a coordinate that must not leave the organisation.
var ErrPrivateCoordinate = errors.New("manifests: coordinate is private to the organisation and is not looked up externally")

// PrivacyGuard decides whether a Maven coordinate may be sent to an external PomSource (ADR-021 amendment, m5): never a coordinate produced by an
// organisation unit, never one under a configured private group prefix. Both rules apply because the graph only knows the repositories that were scanned.
type PrivacyGuard struct {
	prefixes []string
	internal func(ComponentID) bool
}

// NewPrivacyGuard builds a guard; internal reports whether some organisation unit produces the component (nil = nothing is known as internal).
func NewPrivacyGuard(cfg PomSourceConfig, internal func(ComponentID) bool) PrivacyGuard {
	ps := make([]string, 0, len(cfg.PrivateGroupPrefixes))
	for _, p := range cfg.PrivateGroupPrefixes {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			ps = append(ps, p)
		}
	}
	return PrivacyGuard{ps, internal}
}

// Check returns nil when the coordinate may be looked up.
func (p PrivacyGuard) Check(id ComponentID) error {
	g := strings.ToLower(id.Namespace)
	for _, pre := range p.prefixes {
		if g == pre || strings.HasPrefix(g, strings.TrimSuffix(pre, ".")+".") {
			return ErrPrivateCoordinate
		}
	}
	if p.internal != nil && p.internal(id) {
		return ErrPrivateCoordinate
	}
	return nil
}

// Allow is Check as a predicate.
func (p PrivacyGuard) Allow(id ComponentID) bool { return p.Check(id) == nil }

// InternalFunc returns a predicate over the graph variant a request class may see, for NewPrivacyGuard.
func InternalFunc(g *Graph) func(ComponentID) bool {
	if g == nil {
		return nil
	}
	return func(id ComponentID) bool { return len(g.Producers(id)) > 0 }
}
