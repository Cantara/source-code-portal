package manifests

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// runGraph rebuilds the org graph from the current snapshot (one variant per ViewClass) and publishes it when the content changed.
// The graph is advisory: the job reads whatever facts exist, skips repositories without facts, and never blocks catalogue changes.
func (s *Service) runGraph(ctx context.Context, _ job.Key) (job.Outcome, error) {
	ctx, span := s.d.Tracer.Start(ctx, "manifests.graph")
	defer span.End()
	t0 := time.Now()
	snap := s.d.Publisher.Current()
	var pub, all []GraphInput
	for _, id := range snap.Repos() {
		f, ok := readmodel.Get(snap, id, s.d.Slots.Facts)
		if !ok || !f.Info.HasValue() || len(f.V.Units) == 0 {
			continue
		}
		repo, ok := readmodel.Get(snap, id, s.d.Slots.Repos)
		if !ok {
			continue
		}
		facts := f.V // value copy: Units slice shared, immutable by contract
		key, _ := purl.RepoKey(repo.V.HTMLURL)
		in := GraphInput{Slug: repo.V.Slug, Key: key, Facts: &facts}
		all = append(all, in)
		if repo.V.VisibleTo(scm.ViewPublic) {
			pub = append(pub, in)
		}
		if err := ctx.Err(); err != nil {
			return job.Outcome{}, err
		}
	}
	n := s.graphN.Add(1)
	maxEdges := s.cfg.Graph.MaxEdges
	gAll := BuildGraph(all, n, maxEdges)
	gPub := gAll
	if len(pub) != len(all) {
		gPub = BuildGraph(pub, n, maxEdges)
	}
	s.m.graphRebuild.Record(ctx, time.Since(t0).Seconds())
	fp := gPub.Fingerprint() + "/" + gAll.Fingerprint()
	if prev := s.lastFP.Load(); prev != nil && *prev == fp {
		s.m.inc(s.m.graphPublish, "result", "unchanged")
		return job.Outcome{Unchanged: true}, nil
	}
	_, err := s.d.Publisher.Apply(func(b *readmodel.Builder) error {
		return readmodel.PutGlobal(b, s.d.Slots.Graph, scm.Classed[*Graph]{Public: gPub, All: gAll})
	})
	if err != nil {
		return job.Outcome{}, err
	}
	s.lastFP.Store(&fp)
	s.m.inc(s.m.graphPublish, "result", "published")
	s.d.Log.Info("manifests graph published", slog.Uint64("build", n), slog.Int("units", gAll.Stats.Units), slog.Int("edges", gAll.Stats.Edges), slog.Int("cycles", gAll.Stats.Cycles))
	return job.Outcome{}, nil
}

// React is the reactor body for scm events: it decides whether a refresh is needed and enqueues it. It never calls the network.
func (s *Service) React(ctx context.Context, ev scm.Event, enq job.Enqueuer) error {
	decide := func(result string, prio job.Priority, force bool) error {
		s.m.inc(s.m.reactorTotal, "event", string(ev.Kind), "result", result)
		if result == "ignored" || result == "dropped" {
			return nil
		}
		return enq.Enqueue(refreshKey(ev.Repo), prio, time.Time{}, force)
	}
	snap := s.d.Publisher.Current()
	rk := store.KeyOf(ev.Repo)
	slug, known := s.idx.lookup(snap, s.d.Slots.Repos, rk)
	var repo scm.Repository
	if known {
		if r, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.Slots.Repos); ok {
			repo = r.V
		}
	}
	switch ev.Kind {
	case scm.EventPush:
		if !known || ev.Deleted || (repo.DefaultBranch != "" && !ev.IsPushTo(repo.DefaultBranch)) {
			return decide("ignored", 0, false)
		}
		if ev.Created || ev.Forced || !ev.PathsComplete {
			s.markRecheck(rk)
			return decide("enqueued_full", job.PrioWebhook, true)
		}
		for _, pth := range ev.ChangedPaths {
			if s.d.Registry.Interesting(pth, toSet(s.cfg.Acquisition.ExcludeSegments)) {
				s.markRecheck(rk)
				return decide("enqueued_paths", job.PrioWebhook, true)
			}
		}
		if ev.HeadTreeOID != "" { // the push provably touched no manifest: remember that this tree is equivalent
			s.mu.Lock()
			s.cleanOID[rk] = ev.HeadTreeOID
			s.mu.Unlock()
		}
		return decide("dropped", 0, false)
	case scm.EventHeadChanged:
		if !known {
			return decide("ignored", 0, false)
		}
		fv, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.Slots.Facts)
		s.mu.Lock()
		clean := s.cleanOID[rk]
		s.mu.Unlock()
		if ok && fv.Info.HasValue() && ev.HeadTreeOID != "" && (fv.V.TreeSHA == ev.HeadTreeOID || clean == ev.HeadTreeOID) {
			return decide("dropped", 0, false)
		}
		return decide("enqueued_head", job.PrioScheduled, true)
	case scm.EventCreate: // the tracked branch came into being (a new repository, a recreated branch); tags and other branches change no tree we follow
		if !known || repo.DefaultBranch == "" || ev.Ref != "refs/heads/"+repo.DefaultBranch {
			return decide("ignored", 0, false)
		}
		s.markRecheck(rk)
		return decide("enqueued_create", job.PrioWebhook, true)
	case scm.EventRepository:
		switch ev.Action {
		case "deleted", "archived":
			return decide("ignored", 0, false)
		}
		return decide("enqueued_repo", job.PrioWebhook, false)
	}
	return decide("ignored", 0, false)
}

// NoteLockMiss is called by the API when a locked-components lookup misses the cache (evicted): the next refresh of that repository re-parses.
// A repository is forced at most once per refresh.republish_after: under cache pressure an unbounded loop (miss, refresh, eviction of another
// repository's entry, miss) would otherwise burn the API budget; the suppressed misses are counted.
func (s *Service) NoteLockMiss(rk store.RepoKey) {
	now := time.Now()
	s.mu.Lock()
	if last, ok := s.lockMissAt[rk]; ok && now.Sub(last) < s.cfg.Refresh.RepublishAfter {
		s.mu.Unlock()
		s.m.inc(s.m.lockedCache, "result", "miss_suppressed")
		return
	}
	s.lockMissAt[rk] = now
	s.lockMiss[rk] = struct{}{}
	s.mu.Unlock()
	if p := s.enq.Load(); p != nil {
		_ = (*p).Enqueue(job.NewKey(KindRefresh, rk.Source, rk.Owner, rk.Name), job.PrioScheduled, time.Time{}, true)
	}
}

// registerGauges wires the observable gauges to the current snapshot; called once from New.
func (s *Service) registerGauges() error {
	_, err := s.d.Meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		snap := s.d.Publisher.Current()
		h, units := s.health(snap, time.Now())
		for e, n := range units {
			o.ObserveInt64(s.m.units, n, attrs("ecosystem", string(e)))
		}
		o.ObserveInt64(s.m.stale, int64(h.StaleRepos))
		if g, ok := readmodel.GetGlobal(snap, s.d.Slots.Graph); ok && g.All != nil {
			o.ObserveInt64(s.m.gUnits, int64(g.All.Stats.Units))
			o.ObserveInt64(s.m.gEdges, int64(g.All.Stats.Edges))
			o.ObserveInt64(s.m.gCycles, int64(g.All.Stats.Cycles))
			o.ObserveInt64(s.m.gDangling, int64(h.DanglingUnits))
		}
		o.ObserveInt64(s.m.inflight, s.wd.Budget.InUse())
		for _, id := range h.DisabledParsers {
			o.ObserveInt64(s.m.circuit, 1, attrs("parser", id))
		}
		return nil
	}, s.m.units, s.m.stale, s.m.gUnits, s.m.gEdges, s.m.gCycles, s.m.gDangling, s.m.inflight, s.m.circuit)
	return err
}

var _ = strings.TrimSpace
var _ freshness.State
