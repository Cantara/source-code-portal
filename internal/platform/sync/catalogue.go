package sync

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
)

// sourceOf is the source an item belongs to: a per-source item's key ends in it, a repository's item follows its repository.
func (e *Engine) sourceOf(it store.Item) string {
	if r, ok := e.repos[it.Repo]; ok {
		return r.ID.Source
	}
	if _, src, ok := strings.Cut(it.Key, "/"); ok && !strings.Contains(src, "/") {
		return src
	}
	return ""
}

func freshness(it store.Item, ttl time.Duration) snapshot.Freshness {
	return snapshot.Freshness{State: it.State, FetchedAt: it.FetchedAt, CheckedAt: it.CheckedAt, Attempts: it.Attempts,
		ErrCode: it.ErrCode, TTL: ttl}
}

// setCatalogue replaces the repositories of source, dropping the entries of those that left or changed identity and adding entries
// for new ones. It returns the keys of the dropped entries. An address another source owns (snapshot.Outranks) is not source's: it is
// left out and told in the log, as the store does, and an address source owns takes over from a source that ranks lower. Callers hold
// e.mu.
func (e *Engine) setCatalogue(source string, repos []store.Repo, now time.Time) (removed []string) {
	next := make(map[snapshot.RepoKey]store.Repo, len(repos))
	kept := make([]store.Repo, 0, len(repos))
	for _, r := range repos {
		if h, held := e.repos[r.Key]; held && h.ID.Source != source && !snapshot.Outranks(source, h.ID.Source) {
			e.log.Warn("catalogue: address held by another source; not tracked", "repo", r.Key, "source", source, "held_by", h.ID.Source)
			continue
		}
		next[r.Key] = r
		kept = append(kept, r)
	}
	for k, r := range e.repos {
		if n, listed := next[k]; (r.ID.Source == source || listed) && (n.Key != r.Key || n.ID != r.ID) { // a visibility change keeps the entries
			delete(e.repos, k)
			for key, en := range e.entries {
				if en.repo == k {
					delete(e.entries, key) // the store deletes its items with the repository
					removed = append(removed, key)
				}
			}
		}
	}
	for _, r := range kept {
		e.repos[r.Key] = r
		e.addRepoEntries(r, nil, now)
	}
	slices.Sort(removed)
	return removed
}

// addRepoEntries makes sure every per-repository spec has an entry for r. Callers hold e.mu.
func (e *Engine) addRepoEntries(r store.Repo, stored map[string]store.Item, now time.Time) {
	for _, s := range e.specs {
		if s.PerSource {
			continue
		}
		key := s.Kind + "/" + string(r.Key)
		if e.entries[key] != nil {
			continue
		}
		e.entries[key] = e.newEntry(s, key, r.ID.Source, r.Key, stored[key], now)
	}
}

func (e *Engine) newEntry(s *Spec, key, source string, repo snapshot.RepoKey, it store.Item, now time.Time) *entry {
	en := &entry{spec: s, key: key, source: source, repo: repo, item: it, due: now, prio: s.Prio}
	if it.Key == "" {
		en.item = store.Item{Key: key, Kind: s.Kind, Repo: repo, State: snapshot.StateNever}
		en.prio = PrioBackfill
		return en
	}
	if !it.NextDue.IsZero() && it.NextDue.After(now) {
		en.due = it.NextDue
		en.backoff = it.Attempts > 0
		if limit := now.Add(e.TTLOf(s, source)); !en.backoff && en.due.After(limit) {
			en.due = limit // the TTL was shortened since the schedule was stored
		}
	}
	return en
}

// warmStart drops rows of removed sources and kinds, loads the rest, publishes it as the first snapshot and restores the schedule.
func (e *Engine) warmStart(ctx context.Context) error {
	kinds := make([]string, 0, len(e.specs))
	for _, s := range e.specs {
		kinds = append(kinds, s.Kind)
	}
	if err := e.store.RetainSources(ctx, e.sources, kinds); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	repos, items, err := e.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	stored := map[string]store.Item{}
	for _, it := range items {
		stored[it.Key] = it
	}
	now := time.Now()
	e.mu.Lock()
	for _, s := range e.specs {
		if !s.PerSource {
			continue
		}
		for _, src := range e.sources {
			key := s.Kind + "/" + src
			e.entries[key] = e.newEntry(s, key, src, "", stored[key], now)
		}
	}
	for _, r := range repos {
		e.repos[r.Key] = r
		e.addRepoEntries(r, stored, now)
	}
	retried := 0
	if e.release != "" {
		last, err := e.store.Meta(ctx, metaRelease)
		if err != nil {
			e.mu.Unlock()
			return fmt.Errorf("store: %w", err)
		}
		if last != e.release {
			for _, en := range e.entries {
				if en.item.Attempts > 0 {
					en.due, en.prio, en.backoff = now, PrioBackfill, false
					retried++
				}
			}
		}
	}
	itemSource := make(map[string]string, len(items)) // the source of each stored item, for its TTL
	for _, it := range items {
		itemSource[it.Key] = e.sourceOf(it)
	}
	e.mu.Unlock()
	if e.release != "" {
		if err := e.store.SetMeta(ctx, metaRelease, e.release); err != nil {
			e.log.Error("store write failed", "key", metaRelease, "err", err)
		}
		if retried > 0 {
			e.log.Info("new release: failed jobs run again", "release", e.release, "jobs", retried)
		}
	}

	e.pub.Update(func(b *snapshot.Builder) {
		for _, s := range e.specs { // registration order: the catalogue before what hangs off it
			for _, it := range items {
				if it.Kind == s.Kind {
					publish(b, s, it, freshness(it, e.TTLOf(s, itemSource[it.Key])), nil)
				}
			}
		}
	})
	e.pub.Flush()
	close(e.warm)
	e.log.Info("warm start", "repos", len(repos), "items", len(items))
	return nil
}
