package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// HeadChange is the pure comparison of two probes of one repository.
type HeadChange struct {
	First      bool // no previous observation
	HeadMoved  bool // HEAD sha differs from a previous, non-empty sha
	StatsMoved bool // anything in RepoStats differs (payload is rewritten)
}

// DiffHeads compares the previous observation (nil on first sight) with the new one. Only HeadMoved produces an event.
func DiffHeads(prev *scm.RepoStats, next scm.RepoStats) HeadChange {
	if prev == nil {
		return HeadChange{First: true, StatsMoved: true}
	}
	return HeadChange{HeadMoved: prev.HeadSHA != "" && next.HeadSHA != "" && prev.HeadSHA != next.HeadSHA, StatsMoved: !sameStats(*prev, next)}
}

// HeadEvent builds the synthetic event of a moved HEAD. The delivery id is deterministic, so a retried sweep produces the same id.
func HeadEvent(r scm.RepoRef, branch string, prev, next scm.RepoStats) scm.Event {
	short := next.HeadSHA
	if len(short) > 12 {
		short = short[:12]
	}
	return scm.Event{Kind: scm.EventHeadChanged, Delivery: fmt.Sprintf("head-%d-%s", r.ID, short), Source: r.Source, Repo: r,
		Ref: "refs/heads/" + branch, Before: prev.HeadSHA, After: next.HeadSHA, HeadTreeOID: next.HeadTreeOID, PathsComplete: false}
}

// runHeads is the engine job catalog.heads (global). It probes every tracked, non-archived repository in batches, starting where the
// previous sweep stopped, so a sweep that keeps failing midway cannot starve the tail of the catalogue.
func (s *Service) runHeads(ctx context.Context, _ job.Key) (job.Outcome, error) {
	ctx, span := s.d.Tracer.Start(ctx, "catalog.heads")
	defer span.End()
	start := time.Now()
	defer func() { s.m.headsSecs.Record(ctx, time.Since(start).Seconds()) }()

	s.mu.Lock()
	var refs []scm.RepoRef
	for _, t := range s.tracked {
		if !t.Repo.Archived {
			refs = append(refs, t.Repo.Ref)
		}
	}
	slices.SortFunc(refs, func(a, b scm.RepoRef) int { return int(a.ID - b.ID) })
	if n := len(refs); n > 0 {
		s.headPos %= n
		refs = append(refs[s.headPos:], refs[:s.headPos]...)
	}
	s.mu.Unlock()

	changed := false
	size := s.d.Config.Heads.BatchSize
	for i := 0; i < len(refs); i += size {
		batch := refs[i:min(i+size, len(refs))]
		res, err := s.d.Prober.ProbeHeads(ctx, batch)
		if err != nil {
			s.m.headsProbe.Add(ctx, int64(len(batch)), metric.WithAttributes(attribute.String("result", "error")))
			return job.Outcome{}, err
		}
		ch, err := s.applyStats(ctx, batch, res)
		changed = changed || ch
		s.mu.Lock()
		s.headPos += len(batch)
		s.mu.Unlock()
		if err != nil {
			return job.Outcome{}, err
		}
	}
	return job.Outcome{Unchanged: !changed}, nil
}

// runStats is the per-repository job catalog.stats: the 24 h cadence of archived repositories and the accelerator of release/create events.
func (s *Service) runStats(ctx context.Context, k job.Key) (job.Outcome, error) {
	p := k.Parts()
	if len(p) != 3 {
		return job.Outcome{}, permanentError{fmt.Errorf("bad key %q", k)}
	}
	t, ok := s.lookupRef(scm.RepoRef{Source: scm.SourceID(p[0]), Owner: p[1], Name: p[2]})
	if !ok {
		return job.Outcome{}, goneError{string(k)}
	}
	res, err := s.d.Prober.ProbeHeads(ctx, []scm.RepoRef{t.Repo.Ref})
	if err != nil {
		return job.Outcome{}, err
	}
	changed, err := s.applyStats(ctx, []scm.RepoRef{t.Repo.Ref}, res)
	return job.Outcome{Unchanged: !changed}, err
}

// applyStats records one batch: diff against the previous observation, persist, publish once, then emit events (outside every lock).
func (s *Service) applyStats(ctx context.Context, batch []scm.RepoRef, res map[scm.RepoID]scm.RepoStats) (changed bool, err error) {
	now := time.Now().UTC()
	type upd struct {
		t      Tracked
		stats  scm.RepoStats
		change HeadChange
		prev   scm.RepoStats
	}
	var upds []upd
	s.mu.Lock()
	byKey := map[RepoKey]Tracked{}
	for _, t := range s.tracked {
		byKey[KeyOf(t.Repo.Ref)] = t
	}
	for _, r := range batch {
		k := KeyOf(r)
		t, still := byKey[k]
		if !still {
			continue // removed since the batch was cut
		}
		st, ok := res[r.ID]
		if !ok {
			s.m.headsProbe.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "missing")))
			continue // never a removal signal: only the reconcile removes
		}
		var prev *scm.RepoStats
		if p, had := s.stats[k]; had {
			prev = &p
		}
		c := DiffHeads(prev, st)
		u := upd{t: t, stats: st, change: c}
		if prev != nil {
			u.prev = *prev
		}
		upds = append(upds, u)
		s.m.headsProbe.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "ok")))
	}
	s.mu.Unlock()

	// persist: a changed payload is rewritten, an unchanged one is only revalidated (no payload write)
	for _, u := range upds {
		ik := store.ItemKey{Repo: store.KeyOf(u.t.Repo.Ref), Kind: KindStats}
		var perr error
		if u.change.StatsMoved {
			var payload []byte
			if payload, perr = encodeStats(u.stats); perr == nil {
				perr = s.d.Store.PutItem(ctx, store.Item{Key: ik, State: freshness.StateOK, FetchedAt: now, CheckedAt: now, ContentSHA: u.stats.HeadSHA, Payload: payload})
			}
		} else {
			perr = s.d.Store.Revalidated(ctx, ik, now)
		}
		if perr != nil && !errors.Is(perr, store.ErrNoRepo) {
			s.m.persistErr.Add(ctx, 1, metric.WithAttributes(attribute.String("op", "stats")))
			s.d.Log.WarnContext(ctx, "stats not persisted", "repo", u.t.Repo.Slug, "err", perr)
		}
	}

	// publish: one Apply per batch; a repository removed in the meantime is skipped (ErrUnknownRepo), never resurrected
	if len(upds) > 0 {
		_, err = s.d.Publisher.Apply(func(b *readmodel.Builder) error {
			for _, u := range upds {
				if cur, ok := readmodel.Get(b.View(), readmodel.EntryID(u.t.Repo.Slug), s.d.Slots.Repo); !ok || cur.V.Ref.ID != u.t.Repo.Ref.ID {
					continue // removed or renamed since the batch was cut: the slug is gone or belongs to another repository now
				}
				v := freshness.Value[scm.RepoStats]{V: u.stats, Info: freshness.Info{State: freshness.StateOK, FetchedAt: now, CheckedAt: now, ContentSHA: u.stats.HeadSHA, TTL: s.statsTTL(u.t.Repo.Archived)}}
				if err := readmodel.Put(b, readmodel.EntryID(u.t.Repo.Slug), s.d.Slots.Stats, v); err != nil && !errors.Is(err, readmodel.ErrUnknownRepo) {
					return err
				}
			}
			return nil
		})
		if errors.Is(err, readmodel.ErrInvalidSnapshot) {
			err = permanentError{err}
		}
		if err != nil {
			return false, err
		}
	}
	s.mu.Lock() // remembered only after the publish succeeded, so a failed publish is retried by the next sweep
	for _, u := range upds {
		s.stats[KeyOf(u.t.Repo.Ref)] = u.stats
	}
	s.mu.Unlock()

	// events: after the publish, so a reactor that looks at the snapshot sees the new HEAD
	for _, u := range upds {
		if u.change.StatsMoved {
			changed = true
		}
		if !u.change.HeadMoved || s.d.Events == nil {
			continue
		}
		s.m.headChanges.Add(ctx, 1)
		branch := u.t.Repo.DefaultBranch
		if err := s.d.Events.Handle(ctx, HeadEvent(u.t.Repo.Ref, branch, u.prev, u.stats)); err != nil {
			s.m.headEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "error")))
			if errors.Is(err, job.ErrClosed) {
				return changed, err
			}
			s.d.Log.WarnContext(ctx, "head_changed not fully handled", "repo", u.t.Repo.Slug, "err", err)
			continue
		}
		s.m.headEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "emitted")))
	}
	return changed, nil
}

// statsTTL is the freshness TTL of a catalog.stats value: the sweep interval for repositories the sweep probes, the per-repository job's
// interval for archived repositories (which the sweep skips). Without the distinction an archived repository would be "stale" 8 minutes after every refresh.
func (s *Service) statsTTL(archived bool) time.Duration {
	if archived {
		return s.d.Config.Heads.ArchivedInterval
	}
	return s.d.Config.Heads.Interval
}
