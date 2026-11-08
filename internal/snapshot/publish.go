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
	s    *Snapshot
	prev *Snapshot // the snapshot it was copied from: an index that cannot be built is kept as it was there
	log  *slog.Logger
}

func newBuilder(cur *Snapshot, log *slog.Logger) *Builder {
	return &Builder{prev: cur, log: log, s: &Snapshot{
		Catalogued: cur.Catalogued,
		marks:      maps.Clone(cur.marks),
		sum:        cur.sum,
		Repos:      maps.Clone(cur.Repos),
		Aliases:    maps.Clone(cur.Aliases),
		Groups:     slices.Clone(cur.Groups),
		Sources:    slices.Clone(cur.Sources),
		hidden:     cur.hidden,
		indexers:   cur.indexers,
	}}
}

// Repos returns the builder's repositories. Callers must not modify the map or the entries; use ReplaceSource.
func (b *Builder) Repos() map[RepoKey]*RepoEntry { return b.s.Repos }

// ReplaceSource replaces every repository and alias of source with repos and aliases, and records its status. Only Repo and Fresh
// of the given entries are used: what other jobs contributed is kept for a repository that stays at the same address.
func (b *Builder) ReplaceSource(source string, repos []*RepoEntry, aliases map[RepoKey]RepoKey, status SourceStatus) {
	old := map[RepoKey]*RepoEntry{}
	for k, e := range b.s.Repos {
		if e.Repo.ID.Source == source {
			old[k] = e
			delete(b.s.Repos, k)
		}
	}
	for old, cur := range b.s.Aliases {
		if _, ok := b.s.Repos[cur]; !ok {
			delete(b.s.Aliases, old)
		}
	}
	for _, e := range repos {
		k := e.Repo.Key()
		if held := b.s.Repos[k]; held != nil && held.Repo.ID.Source != source && !Outranks(source, held.Repo.ID.Source) {
			continue // another source owns the address (every entry of source itself was removed above)
		}
		if prev := old[k]; prev != nil && prev.Repo.ID == e.Repo.ID {
			merged := *prev
			merged.Repo, merged.Fresh = e.Repo, e.Fresh
			e = &merged
		}
		b.s.Repos[k] = e
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

// UpdateRepo replaces the entry at key with a copy changed by fn. It reports false, and does nothing, when key is not catalogued.
func (b *Builder) UpdateRepo(key RepoKey, fn func(*RepoEntry)) bool {
	cur, ok := b.s.Repos[key]
	if !ok {
		return false
	}
	next := *cur
	fn(&next)
	b.s.Repos[key] = &next
	return true
}

// Sources returns the builder's source statuses. Callers must not modify the slice.
func (b *Builder) Sources() []SourceStatus { return b.s.Sources }

// Mark records the digests of the published item at key, of the repository repo ("" for a source's items), 0 when it is gone: digest
// is what its publish shows (the version is the sum of them), content what it holds (Snapshot.Digest).
func (b *Builder) Mark(key string, repo RepoKey, digest, content uint64) {
	if b.s.marks == nil {
		b.s.marks = map[string]mark{}
	}
	b.s.sum -= b.s.marks[key].digest
	if digest == 0 {
		delete(b.s.marks, key)
	} else {
		b.s.marks[key] = mark{repo: repo, digest: digest, content: content}
	}
	b.s.sum += digest
}

// SetGroups replaces the groups (config order).
func (b *Builder) SetGroups(groups []Group) { b.s.Groups = groups }

// SetHidden names the forks the default views leave out (no source shows them and no group pins them). The set is not modified
// afterwards; an empty one means none.
func (b *Builder) SetHidden(keys map[RepoKey]bool) {
	b.s.hidden = nil
	if len(keys) > 0 {
		b.s.hidden = keys
	}
}

// SetCatalogued marks the snapshot as holding a catalogue.
func (b *Builder) SetCatalogued() { b.s.Catalogued = true }

func (b *Builder) finish(now time.Time) *Snapshot {
	s := b.s
	s.Version = s.sum & (1<<53 - 1)
	s.BuiltAt = now
	s.digests = make(map[RepoKey]uint64, len(s.Repos))
	for _, m := range s.marks {
		if m.repo != "" {
			s.digests[m.repo] += m.content
		}
	}
	s.index(b.prev.indexes, b.log)
	s.views(b.prev, b.log)
	return s
}

// views derives the other three views from the complete one A (s): B without the private repositories, C without the hidden forks,
// D without both. A view that would hold what another holds is that view, so a portal without private repositories or hidden forks
// builds nothing more. Every view knows its siblings: public (private left out, forks as it has them) and lean (forks left out).
func (s *Snapshot) views(prev *Snapshot, log *slog.Logger) {
	var pb, pc, pd *Snapshot // the views of the snapshot before, whose indexes are kept when one cannot be built
	if prev != nil {
		pb, pc = prev.public, prev.lean
		if pc != nil {
			pd = pc.public
		}
	}
	b := s.derive(func(e *RepoEntry) bool { return e.Repo.Public() }, pb, log)
	c := s
	if slices.ContainsFunc(slices.Collect(maps.Values(s.Repos)), func(e *RepoEntry) bool { return s.hidden[e.Repo.Key()] }) {
		c = s.derive(func(e *RepoEntry) bool { return !s.hidden[e.Repo.Key()] }, pc, log)
	}
	d := b
	if c != s {
		d = c.derive(func(e *RepoEntry) bool { return e.Repo.Public() }, pd, log)
	}
	s.public, s.lean = b, c
	b.public, b.lean = b, d
	c.public, c.lean = d, c
	d.public, d.lean = d, d
	s.fat, b.fat, c.fat, d.fat = s, b, s, b
	b.hidden, c.hidden, d.hidden = s.hidden, s.hidden, s.hidden
}

// index builds the derived lookups: group membership, then every feature's index. An index whose builder panics keeps what the
// snapshot before had (before): that feature shows its last good state while the rest of the batch is published, and the panic is
// logged on every publish until it is fixed.
func (s *Snapshot) index(before map[string]any, log *slog.Logger) {
	s.groupsOf = map[RepoKey][]string{}
	for _, g := range s.Groups {
		for _, m := range g.Members {
			s.groupsOf[m] = append(s.groupsOf[m], g.ID)
		}
	}
	s.indexes = make(map[string]any, len(s.indexers))
	for _, ix := range s.indexers {
		v, err := buildIndex(ix, s)
		if err != nil {
			prev, ok := before[ix.name]
			if !ok { // nothing to keep (the first build, a new index): no half-built snapshot is served; the build fails as a whole
				panic(fmt.Sprintf("index %s: %v", ix.name, err))
			}
			log.Error("index not built; the last good one is kept", "index", ix.name, "err", err)
			v = prev
		}
		s.indexes[ix.name] = v
	}
}

// buildIndex runs one indexer, a panic of it as an error.
func buildIndex(ix Indexer, s *Snapshot) (v any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return ix.build(s), nil
}

// derive is s with the repositories keep refuses taken out, or s itself when it refuses none. before is the sibling view of the
// snapshot before, whose indexes are kept when one cannot be built.
func (s *Snapshot) derive(keep func(*RepoEntry) bool, before *Snapshot, log *slog.Logger) *Snapshot {
	if !slices.ContainsFunc(slices.Collect(maps.Values(s.Repos)), func(e *RepoEntry) bool { return !keep(e) }) {
		return s
	}
	p := &Snapshot{BuiltAt: s.BuiltAt, Catalogued: s.Catalogued, Repos: map[RepoKey]*RepoEntry{}, Aliases: map[RepoKey]RepoKey{},
		indexers: s.indexers}
	count := map[string]int{}
	for k, e := range s.Repos {
		if keep(e) {
			p.Repos[k] = e
			count[e.Repo.ID.Source]++
		}
	}
	for old, cur := range s.Aliases {
		if _, ok := p.Repos[cur]; ok {
			p.Aliases[old] = cur
		}
	}
	for _, g := range s.Groups {
		g.Members = slices.DeleteFunc(slices.Clone(g.Members), func(k RepoKey) bool { return p.Repos[k] == nil })
		if g.DefaultRepo != "" && p.Repos[g.DefaultRepo] == nil {
			// Private: told as a default that does not exist. An empty "ok" would tell that it exists and is private, since every
			// group names a default.
			g.DefaultRepo, g.DefaultMissing = "", true
		}
		p.Groups = append(p.Groups, g)
	}
	for _, src := range s.Sources {
		src.Repos = count[src.ID]
		p.Sources = append(p.Sources, src)
	}
	var sum uint64
	for _, m := range s.marks {
		if e, _ := s.Repo(m.repo); e == nil || keep(e) {
			sum += m.digest
		}
	}
	p.Version = sum & (1<<53 - 1)
	p.digests = s.digests
	var prior map[string]any
	if before != nil {
		prior = before.indexes
	}
	p.index(prior, log)
	return p
}

// Publisher owns the current snapshot. Updates are queued and applied in order at the next publish, which happens Debounce after
// the last update or MaxDelay after the first pending one, whichever comes first.
type Publisher struct {
	debounce, maxDelay time.Duration
	log                *slog.Logger
	indexers           []Indexer
	cur                atomic.Pointer[Snapshot]
	building           sync.Mutex // one build at a time: a build starts from the snapshot the previous one stored

	mu        sync.Mutex
	pending   []func(*Builder)
	firstAt   time.Time
	timer     *time.Timer
	closed    bool
	published chan struct{} // closed and replaced on every publish
}

// NewPublisher returns a publisher holding an empty version-0 snapshot whose publishes build the features' indexes.
func NewPublisher(debounce, maxDelay time.Duration, log *slog.Logger, indexers ...Indexer) *Publisher {
	p := &Publisher{debounce: debounce, maxDelay: maxDelay, log: log, indexers: indexers, published: make(chan struct{})}
	p.cur.Store(&Snapshot{Repos: map[RepoKey]*RepoEntry{}, Aliases: map[RepoKey]RepoKey{}, groupsOf: map[RepoKey][]string{},
		indexers: indexers})
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
	p.building.Lock()
	defer p.building.Unlock()
	p.mu.Lock()
	batch := p.pending
	p.pending = nil
	p.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	next, err := build(p.cur.Load(), batch, p.log)
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

// build applies the batch to a copy of cur. An update that panics is dropped alone (what it changed before the panic stays), an
// index that panics keeps its last good value; a panic elsewhere while finishing fails the build.
func build(cur *Snapshot, batch []func(*Builder), log *slog.Logger) (s *Snapshot, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while indexing the snapshot: %v", r)
		}
	}()
	b := newBuilder(cur, log)
	for _, fn := range batch {
		apply(b, fn, log)
	}
	return b.finish(time.Now()), nil
}

func apply(b *Builder, fn func(*Builder), log *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("snapshot update panicked; dropped", "panic", fmt.Sprint(r))
		}
	}()
	fn(b)
}
