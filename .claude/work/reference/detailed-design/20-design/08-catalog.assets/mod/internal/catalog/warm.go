package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// WarmInfo reports what Warm loaded (logged by cmd/scp as the "data age" of startup step 5).
type WarmInfo struct {
	Repos      int
	Skipped    int // rows that could not be decoded (logged individually)
	StatsItems int
	Newest     time.Time // newest SeenAt: the age gate `catalogue` of /readyz compares it with readiness.warm_max_age
}

// Warm is startup step 5. It reads the persisted catalogue and the catalog.stats items, restores the in-memory state, and returns the
// function cmd/scp runs inside the single Apply that builds snapshot version 1. It performs the only catalog reads of the store.
func (s *Service) Warm(ctx context.Context) (func(*readmodel.Builder) error, WarmInfo, error) {
	var info WarmInfo
	rows, err := s.d.Store.LoadCatalogue(ctx)
	if err != nil {
		return nil, info, fmt.Errorf("load catalogue: %w", err)
	}
	var tracked []Tracked
	for _, r := range rows {
		t, err := DecodeRow(r)
		if err != nil {
			info.Skipped++
			s.d.Log.WarnContext(ctx, "catalogue row skipped", "err", err)
			continue
		}
		src, ok := s.source(t.Repo.Ref.Source)
		if !ok {
			info.Skipped++ // a source that is no longer configured: its repositories leave with the next reconcile
			continue
		}
		if src.Policy.Admit(t.Repo) == ExcludedVisibility {
			info.Skipped++ // the policy was tightened since the row was written (installation_scope -> public_only): never serve it, not even until the first listing
			continue
		}
		tracked = append(tracked, t)
		if t.SeenAt.After(info.Newest) {
			info.Newest = t.SeenAt
		}
	}
	info.Repos = len(tracked)
	byName := map[store.RepoKey]RepoKey{}
	for _, t := range tracked {
		r := t.Repo.Ref
		byName[store.KeyOf(r)] = KeyOf(r)
	}
	stats := map[RepoKey]scm.RepoStats{}
	fetched := map[RepoKey]time.Time{}
	err = s.d.Store.LoadItems(ctx, func(it store.Item) error {
		if it.Key.Kind != KindStats || it.State != freshness.StateOK {
			return nil
		}
		k, ok := byName[it.Key.Repo]
		if !ok {
			return nil
		}
		st, err := decodeStats(it.Payload)
		if err != nil {
			return nil // a bad payload is a missing item: the next sweep rewrites it
		}
		stats[k] = st
		fetched[k] = it.FetchedAt
		info.StatsItems++
		return nil
	})
	if err != nil {
		return nil, info, fmt.Errorf("load items: %w", err)
	}

	mr, err := s.buildMembership(ctx, tracked)
	if err != nil {
		return nil, info, fmt.Errorf("build membership: %w", err)
	}
	s.mu.Lock()
	s.tracked, s.stats, s.groups = tracked, stats, mr.Groups
	for _, sd := range s.d.Sources {
		st := s.sources[sd.ID]
		for _, t := range tracked {
			if t.Repo.Ref.Source == sd.ID && t.SeenAt.After(st.warmAt) {
				st.warmAt, st.state = t.SeenAt, "warm"
			}
		}
	}
	s.mu.Unlock()
	status := s.warmStatus(tracked, mr)
	s.recordGauges(ctx, tracked, mr)
	aliases, groupAliases := buildAliases(tracked), buildGroupAliases(s.d.Config)
	ttl := s.reconcileTTL()
	return func(b *readmodel.Builder) error {
		for _, t := range tracked {
			id := readmodel.EntryID(t.Repo.Slug)
			v := freshness.Value[scm.Repository]{V: entryRepo(t, mr), Info: freshness.Info{State: freshness.StateOK, FetchedAt: t.SeenAt, CheckedAt: t.SeenAt, TTL: ttl}}
			if err := readmodel.Put(b, id, s.d.Slots.Repo, v); err != nil {
				return err
			}
			if st, ok := stats[KeyOf(t.Repo.Ref)]; ok {
				at := fetched[KeyOf(t.Repo.Ref)]
				sv := freshness.Value[scm.RepoStats]{V: st, Info: freshness.Info{State: freshness.StateOK, FetchedAt: at, CheckedAt: at, ContentSHA: st.HeadSHA, TTL: s.statsTTL(t.Repo.Archived)}}
				if err := readmodel.Put(b, id, s.d.Slots.Stats, sv); err != nil {
					return err
				}
			}
		}
		readmodel.PutGlobal(b, s.d.Slots.Membership, mr.Membership)
		readmodel.PutGlobal(b, s.d.Slots.Aliases, aliases)
		readmodel.PutGlobal(b, s.d.Slots.GroupAliases, groupAliases)
		readmodel.PutGlobal(b, s.d.Slots.Sync, status)
		return nil
	}, info, nil
}

func (s *Service) warmStatus(tracked []Tracked, mr MembershipResult) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{Groups: mr.Groups}
	for _, sd := range s.d.Sources {
		st := s.sources[sd.ID]
		out.Sources = append(out.Sources, SourceStatus{ID: sd.ID, State: st.state, LastSuccess: st.warmAt, Tracked: countSource(tracked, sd.ID)})
	}
	// LastReconcile stays zero until every source has been listed live once: a warm value is "stale by definition" in /status.
	return out
}

// Validator is the catalog part of the snapshot validators (section 4.6): every membership entry resolves to an existence value, every
// alias target exists, and the default repository of a group is one of its members.
func (sl Slots) Validator() readmodel.Validator {
	return func(s *readmodel.Snapshot) error {
		m, ok := readmodel.GetGlobal(s, sl.Membership)
		if !ok {
			return nil // before the first publish
		}
		for _, g := range m.Groups() {
			members := map[scm.Slug]bool{}
			for _, slug := range m.ReposOf(g.ID) {
				members[slug] = true
				if _, ok := readmodel.Get(s, readmodel.EntryID(slug), sl.Repo); !ok {
					return fmt.Errorf("catalog: group %s lists %s, which has no catalog.repo value", g.ID, slug)
				}
			}
			if d, ok := m.DefaultRepo(g.ID); ok && !members[d] {
				return fmt.Errorf("catalog: default repository %s of group %s is not a member", d, g.ID)
			}
		}
		if al, ok := readmodel.GetGlobal(s, sl.Aliases); ok {
			for old, cur := range al {
				if _, ok := readmodel.Get(s, readmodel.EntryID(cur), sl.Repo); !ok {
					return fmt.Errorf("catalog: alias %s points to missing %s", old, cur)
				}
			}
		}
		return nil
	}
}

// Coverage is the readiness probe of the catalog kind: repositories with a good catalog.stats value over all repositories.
func (sl Slots) Coverage(s *readmodel.Snapshot) (have, total int) {
	for _, id := range s.Repos() {
		total++
		if v, ok := readmodel.Get(s, id, sl.Stats); ok && v.Info.State == "ok" {
			have++
		}
	}
	return have, total
}
