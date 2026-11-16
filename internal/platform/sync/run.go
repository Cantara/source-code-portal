package sync

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
)

// Run loads the store into the snapshot, then runs jobs until ctx ends. Jobs in flight at the end are cancelled; their rows are
// untouched, so they are due again after a restart.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.warmStart(ctx); err != nil {
		return err
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	running := 0
	var lastStart time.Time
	for {
		e.beat.Store(time.Now().UnixNano())
		wait := idleWake
		if running < e.n && e.leader.Load() {
			if next := lastStart.Add(pace); time.Now().Before(next) {
				wait = time.Until(next)
			} else if en, at := e.next(time.Now()); en != nil {
				e.start(ctx, en)
				running++
				lastStart = time.Now()
				wait = pace
			} else if !at.IsZero() {
				wait = min(wait, max(time.Until(at), 0))
			}
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			for ; running > 0; running-- {
				<-e.done
			}
			return nil
		case o := <-e.done:
			running--
			e.finish(ctx, o)
		case <-e.wake:
		case <-timer.C:
		}
	}
}

// next returns the most urgent startable entry, or when the earliest one becomes startable.
func (e *Engine) next(now time.Time) (*entry, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var best *entry
	var at time.Time
	later := func(t time.Time) {
		if at.IsZero() || t.Before(at) {
			at = t
		}
	}
	for _, en := range e.entries {
		if en.running {
			continue
		}
		if en.due.After(now) {
			later(en.due)
			continue
		}
		if p := e.pauses[en.source]; p != nil && p.until.After(now) {
			later(p.until)
			continue
		}
		if e.budget != nil {
			if frac, reset := e.budget.Remaining(en.source, en.repo.Owner()); frac < en.prio.reserve() {
				later(reset)
				continue
			}
		}
		if best == nil || en.prio < best.prio || en.prio == best.prio && (en.due.Before(best.due) || en.due.Equal(best.due) && en.key < best.key) {
			best = en
		}
	}
	if best != nil {
		best.running, best.dirty, best.started = true, false, now
	}
	return best, at
}

func (e *Engine) start(ctx context.Context, en *entry) {
	j := Job{Key: en.key, Source: en.source, Repo: en.repo, Prev: en.item, Event: en.prio == PrioEvent}
	go func() {
		started := time.Now()
		jctx, cancel := context.WithTimeout(ctx, en.spec.Timeout)
		defer cancel()
		res, err := safeRun(jctx, en.spec, j)
		e.done <- outcome{e: en, res: res, err: err, started: started}
	}()
}

func safeRun(ctx context.Context, s *Spec, j Job) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = permanentError{fmt.Errorf("panic in %s: %v", j.Key, r)}
		}
	}()
	return s.Run(ctx, j)
}

type permanentError struct{ error }

func (permanentError) Permanent() bool   { return true }
func (permanentError) ErrorCode() string { return "panic" }
func (p permanentError) Unwrap() error   { return p.error }

// finish records an outcome: the store row and schedule, the in-memory entry, pauses and the snapshot.
func (e *Engine) finish(ctx context.Context, o outcome) {
	en, now := o.e, time.Now()
	if ctx.Err() != nil {
		return // shutting down: the row stays as it was
	}
	e.mu.Lock()
	en.running = false
	if e.entries[en.key] != en || !e.leader.Load() {
		e.mu.Unlock()
		return // the repository left the catalogue while the job ran, or another instance leads now
	}
	class, retryAt := classify(o.err)
	result := "error"
	switch {
	case class == classOK && o.res.Unchanged:
		result = "not_modified"
	case class == classOK:
		result = "ok"
	case class == classRate:
		result = "rate_limited"
	}
	e.observe(en.spec.Kind, result, now.Sub(o.started))
	switch class {
	case classOK:
		e.succeeded(en, o.res, now)
	case classRate, classAuth:
		if until := e.pauseSource(en.source, retryAt, class == classAuth, now); !until.IsZero() {
			e.log.Warn("source paused", "source", en.source, "until", until, "err", o.err)
		}
	case classPermanent:
		en.item = failed(en.item, o.err)
		en.due, en.backoff = now.Add(permanentRetry), true
	case classTransient:
		en.item = failed(en.item, o.err)
		en.due, en.backoff = now.Add(backoff(en.item.Attempts)), true
	case classWaiting: // what it holds stays; an event (a credential arrived) runs it again at once
		en.due = now.Add(min(waitRetry, e.TTLOf(en.spec, en.source)))
	}
	if en.dirty && !en.backoff {
		en.due, en.prio = en.settled(now), PrioEvent
	}
	en.dirty = false
	if class == classRate || class == classAuth {
		e.mu.Unlock()
		return // not an attempt: the row is untouched and the job runs after the pause
	}
	en.item.NextDue = en.due
	it, spec := en.item, en.spec
	ch := store.Change{Item: it}
	var removed []string
	if class == classOK && o.res.Catalogue != nil {
		ch.Source, ch.Catalogue = en.source, o.res.Catalogue
		removed = e.setCatalogue(en.source, o.res.Catalogue, now)
	}
	var shared Shared
	if e.share != nil {
		en.rev = e.nextRev()
		shared = Shared{Rev: en.rev, Source: en.source, Item: it, Catalogue: ch.Catalogue, Removed: removed}
	}
	e.mu.Unlock()

	if o.err != nil {
		e.log.Warn("job failed", "key", en.key, "attempts", it.Attempts, "err", o.err)
	}
	if err := e.store.Apply(ctx, ch); err != nil {
		e.log.Error("store write failed; serving from memory", "key", en.key, "err", err)
	}
	fresh := freshness(it, e.TTLOf(spec, en.source))
	e.pub.Update(func(b *snapshot.Builder) { publish(b, spec, it, fresh, removed) })
	if e.share != nil {
		e.share(shared)
	}
}

// publish sets an item in the snapshot with its digest and drops the digests of the items that left.
func publish(b *snapshot.Builder, spec *Spec, it store.Item, fresh snapshot.Freshness, removed []string) {
	spec.Publish(b, it, fresh)
	b.Mark(it.Key, it.Repo, digest(it), content(it))
	for _, k := range removed {
		b.Mark(k, "", 0, 0)
	}
}

// content is the digest of what the item holds: its state, content and payload, not when it was fetched or checked, so a check that
// found nothing new leaves it as it was (Snapshot.Digest).
func content(it store.Item) uint64 {
	h := fnv.New64a()
	for _, s := range []string{it.Key, string(it.State), it.ErrCode, it.ContentSHA} {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	_, _ = h.Write(it.Payload)
	return max(h.Sum64(), 1)
}

// digest is the item's contribution to the snapshot version: what its publish shows, in the precision the store keeps.
func digest(it store.Item) uint64 {
	h := fnv.New64a()
	for _, s := range []string{it.Key, string(it.State), it.ErrCode, it.ContentSHA} {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(it.CheckedAt.Unix())) //nolint:gosec // G115: bits of a digest
	binary.BigEndian.PutUint64(b[8:], uint64(it.FetchedAt.Unix())) //nolint:gosec // G115: bits of a digest
	_, _ = h.Write(b[:])
	_, _ = h.Write(it.Payload)
	return max(h.Sum64(), 1)
}
