package catalog

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// unknownRepoReconcileEvery rate-limits reconciles triggered by authenticated events about repositories the catalogue does not
// contain (policy-excluded repositories push all the time and must not cause a reconcile per push).
const unknownRepoReconcileEvery = 5 * time.Minute

var (
	reconcileKey = job.GlobalKey(JobReconcile)
	headsKey     = job.GlobalKey(JobHeads)
)

// React is catalog's reaction to a normalised event (mechanism D). It runs synchronously on the webhook goroutine: it makes a cheap
// decision, enqueues without blocking, and for the two security-relevant repository actions removes the repository from the
// snapshot in memory (no I/O). It never retains ev.
func (s *Service) React(ctx context.Context, ev scm.Event, enq job.Enqueuer) error {
	if ev.Repo.Source == "" {
		ev.Repo.Source = ev.Source // webhook events carry the source on the event; the ref may not repeat it
	}
	count := func(result string) {
		s.m.react.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", string(ev.Kind)), attribute.String("result", result)))
	}
	now := time.Now()
	switch ev.Kind {
	case scm.EventInstallation, scm.EventInstallationRepos:
		s.want(ev.Source) // Action "resync" with a zero Repo (refresh design D10) means: list ev.Source now
		count("enqueued")
		return enq.Enqueue(reconcileKey, job.PrioWebhook, now, false)

	case scm.EventRepository:
		switch ev.Action {
		case "privatized", "deleted", "transferred":
			// confidentiality first: hide at once, let the reconcile confirm and persist (a false hint is repaired by it within seconds)
			s.hide(ev.Repo, ev.Source, ev.Action == "privatized" && s.admitsPrivate(ev.Source))
			s.want(ev.Source)
			count("hidden")
			return enq.Enqueue(reconcileKey, job.PrioInteractive, now, true)
		case "created", "renamed", "publicized", "archived", "unarchived", "edited":
			s.want(ev.Source)
			count("enqueued")
			return enq.Enqueue(reconcileKey, job.PrioWebhook, now.Add(2*time.Second), false)
		}
		count("ignored")
		return nil

	case scm.EventRelease, scm.EventCreate:
		t, ok := s.lookupRef(ev.Repo)
		if !ok {
			return s.unknownRepo(ev, enq, count)
		}
		count("enqueued")
		r := t.Repo.Ref
		return enq.Enqueue(job.NewKey(JobStats, r.KeyParts()...), job.PrioWebhook, now, false) // folded parts, like the planner key

	case scm.EventPush:
		if _, ok := s.lookupRef(ev.Repo); !ok {
			return s.unknownRepo(ev, enq, count)
		}
		count("ignored") // commits, docs and manifests react to pushes; the head sweep covers the catalogue's own data
		return nil
	}
	count("ignored")
	return nil
}

// unknownRepo handles an authenticated event for a repository the catalogue does not hold: at most one reconcile per 5 minutes.
func (s *Service) unknownRepo(ev scm.Event, enq job.Enqueuer, count func(string)) error {
	last := s.lastUnknownReconcile.Load()
	n := time.Now().UnixNano()
	if n-last < int64(unknownRepoReconcileEvery) || !s.lastUnknownReconcile.CompareAndSwap(last, n) {
		count("unknown_repo")
		return nil
	}
	s.want(ev.Source)
	count("enqueued")
	return enq.Enqueue(reconcileKey, job.PrioWebhook, time.Now().Add(10*time.Second), false)
}

// hide removes a repository from the catalogue's in-memory state and from the snapshot at once (no store write, no I/O): the membership
// and alias globals are recomputed in the same Apply so the snapshot validators hold. The reconcile enqueued next re-adds the repository
// if the hint was wrong and persists the removal otherwise.
func (s *Service) hide(ref scm.RepoRef, src scm.SourceID, demote bool) {
	ref.Source = src
	s.pubMu.Lock() // serialises with commit's in-memory phase: tracked and the snapshot change together or not at all
	defer s.pubMu.Unlock()
	t, ok := s.lookupRef(ref)
	if !ok {
		return
	}
	k := KeyOf(t.Repo.Ref)
	s.mu.Lock()
	next := make([]Tracked, 0, len(s.tracked))
	for _, x := range s.tracked {
		switch {
		case KeyOf(x.Repo.Ref) != k:
			next = append(next, x)
		case demote: // installation_scope keeps private repositories: the entry stays, as private, and only ViewAll principals see it
			x.Repo.Visibility = scm.VisPrivate
			next = append(next, x)
		}
	}
	s.tracked = next
	s.hiddenAt[k] = hideHint{at: time.Now().UTC(), demote: demote}
	s.hideGen++
	s.mu.Unlock()
	mr, err := s.buildMembership(context.Background(), next)
	if err != nil {
		s.d.Log.Error("hide failed", "repo", t.Repo.Slug, "err", err)
		return
	}
	aliases := buildAliases(next)
	s.mu.Lock()
	s.groups = mr.Groups
	s.mu.Unlock()
	status := s.buildStatus(next, mr.Groups, nil)
	_, err = s.d.Publisher.Apply(func(b *readmodel.Builder) error {
		if demote {
			old, ok := readmodel.Get(b.View(), readmodel.EntryID(t.Repo.Slug), s.d.Slots.Repo)
			if !ok {
				return nil
			}
			old.V = old.V.Clone()
			old.V.Visibility = scm.VisPrivate
			if err := readmodel.Put(b, readmodel.EntryID(t.Repo.Slug), s.d.Slots.Repo, old); err != nil {
				return err
			}
		} else {
			b.Remove(readmodel.EntryID(t.Repo.Slug))
		}
		for _, err := range []error{readmodel.PutGlobal(b, s.d.Slots.Membership, mr.Membership), readmodel.PutGlobal(b, s.d.Slots.Aliases, aliases),
			readmodel.PutGlobal(b, s.d.Slots.Sync, status)} {
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.d.Log.Error("hide failed", "repo", t.Repo.Slug, "err", err)
	}
}
