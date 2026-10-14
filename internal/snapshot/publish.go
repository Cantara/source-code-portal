package snapshot

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Builder is a private, mutable copy of the current snapshot. Maps are copied; entries are shared and replaced, never mutated.
type Builder struct {
	s *Snapshot
}

func newBuilder(cur *Snapshot) *Builder {
	return &Builder{s: &Snapshot{
		Version:    cur.Version,
		Catalogued: cur.Catalogued,
		Repos:      maps.Clone(cur.Repos),
		Aliases:    maps.Clone(cur.Aliases),
		Groups:     slices.Clone(cur.Groups),
		Sources:    slices.Clone(cur.Sources),
	}}
}

// Repos returns the builder's repositories. Callers must not modify the map or the entries; use ReplaceSource.
func (b *Builder) Repos() map[RepoKey]*RepoEntry { return b.s.Repos }

// ReplaceSource replaces every repository and alias of source with repos and aliases, and records its status.
func (b *Builder) ReplaceSource(source string, repos []*RepoEntry, aliases map[RepoKey]RepoKey, status SourceStatus) {
	for k, e := range b.s.Repos {
		if e.Repo.ID.Source == source {
			delete(b.s.Repos, k)
		}
	}
	for old, cur := range b.s.Aliases {
		if _, ok := b.s.Repos[cur]; !ok {
			delete(b.s.Aliases, old)
		}
	}
	for _, e := range repos {
		b.s.Repos[e.Repo.Key()] = e
	}
	for old, cur := range aliases {
		if _, taken := b.s.Repos[old]; !taken {
			b.s.Aliases[old] = cur
		}
	}
	i := slices.IndexFunc(b.s.Sources, func(s SourceStatus) bool { return s.ID == source })
	if i < 0 {
		b.s.Sources = append(b.s.Sources, status)
		slices.SortFunc(b.s.Sources, func(a, b SourceStatus) int { return strings.Compare(a.ID, b.ID) })
	} else {
		b.s.Sources[i] = status
	}
}

// Sources returns the builder's source statuses. Callers must not modify the slice.
func (b *Builder) Sources() []SourceStatus { return b.s.Sources }

// SetGroups replaces the groups (config order).
func (b *Builder) SetGroups(groups []Group) { b.s.Groups = groups }

// SetCatalogued marks the snapshot as holding a catalogue.
func (b *Builder) SetCatalogued() { b.s.Catalogued = true }

func (b *Builder) finish(now time.Time) *Snapshot {
	s := b.s
	s.Version++
	s.BuiltAt = now
	s.groupsOf = map[RepoKey][]string{}
	for _, g := range s.Groups {
		for _, m := range g.Members {
			s.groupsOf[m] = append(s.groupsOf[m], g.ID)
		}
	}
	return s
}

// Publisher owns the current snapshot. Updates are queued and applied in order at the next publish, which happens Debounce after
// the last update or MaxDelay after the first pending one, whichever comes first.
type Publisher struct {
	debounce, maxDelay time.Duration
	log                *slog.Logger
	cur                atomic.Pointer[Snapshot]

	mu        sync.Mutex
	pending   []func(*Builder)
	firstAt   time.Time
	timer     *time.Timer
	closed    bool
	published chan struct{} // closed and replaced on every publish
}

// NewPublisher returns a publisher holding an empty version-0 snapshot.
func NewPublisher(debounce, maxDelay time.Duration, log *slog.Logger) *Publisher {
	p := &Publisher{debounce: debounce, maxDelay: maxDelay, log: log, published: make(chan struct{})}
	p.cur.Store(&Snapshot{Repos: map[RepoKey]*RepoEntry{}, Aliases: map[RepoKey]RepoKey{}, groupsOf: map[RepoKey][]string{}})
	return p
}

// Current returns the current snapshot. Handlers call it once per request.
func (p *Publisher) Current() *Snapshot { return p.cur.Load() }

// Published returns a channel that is closed at the next publish.
func (p *Publisher) Published() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.published
}

// Update queues fn for the next publish.
func (p *Publisher) Update(fn func(*Builder)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	now := time.Now()
	if len(p.pending) == 0 {
		p.firstAt = now
	}
	p.pending = append(p.pending, fn)
	wait := min(p.debounce, p.firstAt.Add(p.maxDelay).Sub(now))
	if p.timer == nil {
		p.timer = time.AfterFunc(wait, p.publish)
	} else {
		p.timer.Reset(wait)
	}
}

// Flush publishes pending updates now.
func (p *Publisher) Flush() { p.publish() }

// Close drops pending updates and stops publishing.
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.pending = nil
	if p.timer != nil {
		p.timer.Stop()
	}
}

func (p *Publisher) publish() {
	p.mu.Lock()
	batch := p.pending
	p.pending = nil
	p.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	next, err := build(p.cur.Load(), batch)
	if err != nil {
		p.log.Error("snapshot build failed; keeping the current snapshot", "err", err)
		return
	}
	p.cur.Store(next)
	p.mu.Lock()
	close(p.published)
	p.published = make(chan struct{})
	p.mu.Unlock()
}

func build(cur *Snapshot, batch []func(*Builder)) (s *Snapshot, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in snapshot update: %v", r)
		}
	}()
	b := newBuilder(cur)
	for _, fn := range batch {
		fn(b)
	}
	return b.finish(time.Now()), nil
}
