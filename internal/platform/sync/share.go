package sync

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
)

// nextRev returns a revision newer than every one made or seen. Callers hold e.mu.
func (e *Engine) nextRev() int64 {
	e.rev = max(time.Now().UnixNano(), e.rev+1)
	return e.rev
}

// Receive applies an outcome the leader recorded, as if the job had run here: the store row, the schedule and the snapshot. An
// outcome older than what the engine has for its key is ignored; one for a repository not catalogued yet waits for the listing that
// admits it. It waits for the warm start.
func (e *Engine) Receive(ctx context.Context, r Shared) {
	select {
	case <-e.warm:
	case <-ctx.Done():
		return
	}
	now := time.Now()
	e.mu.Lock()
	e.rev = max(e.rev, r.Rev)
	en := e.entries[r.Item.Key]
	if en == nil {
		if _, known := e.repos[r.Item.Repo]; r.Item.Repo != "" && !known && e.spec(r.Item.Kind) != nil {
			if o, ok := e.orphans[r.Item.Key]; !ok || o.Rev < r.Rev {
				e.orphans[r.Item.Key] = r
			}
		}
		e.mu.Unlock()
		return
	}
	if r.Rev <= en.rev {
		e.mu.Unlock()
		return
	}
	en.rev, en.item = r.Rev, r.Item
	en.due, en.backoff, en.prio = r.Item.NextDue, r.Item.Attempts > 0, en.spec.Prio
	if en.due.IsZero() {
		en.due = now
	}
	ch := store.Change{Item: r.Item}
	var adopted []Shared
	var removed []string
	if r.Catalogue != nil {
		ch.Source, ch.Catalogue = r.Source, r.Catalogue
		removed = e.setCatalogue(r.Source, r.Catalogue, now)
		for k, o := range e.orphans {
			switch {
			case e.entries[k] != nil:
				adopted = append(adopted, o)
				delete(e.orphans, k)
			case o.Source == r.Source:
				delete(e.orphans, k) // the listing does not admit its repository
			}
		}
	}
	it, spec := en.item, en.spec
	e.mu.Unlock()

	if err := e.store.Apply(ctx, ch); err != nil {
		e.log.Error("store write failed; serving from memory", "key", it.Key, "err", err)
	}
	fresh := freshness(it, e.TTLOf(spec, en.source))
	e.pub.Update(func(b *snapshot.Builder) { publish(b, spec, it, fresh, removed) })
	slices.SortFunc(adopted, func(a, b Shared) int { return strings.Compare(a.Item.Key, b.Item.Key) })
	for _, o := range adopted {
		e.Receive(ctx, o)
	}
	e.poke()
}

// Records returns every recorded outcome, the listings with their catalogues first, for an instance that takes the lead and fills
// the shared state. Outcomes never shared get a new revision.
func (e *Engine) Records() []Shared {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Shared
	for _, en := range e.entries {
		if en.item.State == snapshot.StateNever {
			continue
		}
		if en.rev == 0 {
			en.rev = e.nextRev()
		}
		r := Shared{Rev: en.rev, Source: en.source, Item: en.item}
		if en.spec.Lists {
			r.Catalogue = []store.Repo{}
			for _, repo := range e.repos {
				if repo.ID.Source == en.source {
					r.Catalogue = append(r.Catalogue, repo)
				}
			}
			slices.SortFunc(r.Catalogue, func(a, b store.Repo) int { return strings.Compare(string(a.Key), string(b.Key)) })
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Shared) int {
		if (a.Catalogue != nil) != (b.Catalogue != nil) {
			if a.Catalogue != nil {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Item.Key, b.Item.Key)
	})
	return out
}
