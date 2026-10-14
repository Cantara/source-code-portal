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
		Catalogued: cur.Catalogued,
		marks:      maps.Clone(cur.marks),
		sum:        cur.sum,
		Repos:      maps.Clone(cur.Repos),
		Aliases:    maps.Clone(cur.Aliases),
		Groups:     slices.Clone(cur.Groups),
		Sources:    slices.Clone(cur.Sources),
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

// Mark records the digest of the published item at key, 0 when it is gone; the version is the sum of the digests.
func (b *Builder) Mark(key string, digest uint64) {
	if b.s.marks == nil {
		b.s.marks = map[string]uint64{}
	}
	b.s.sum -= b.s.marks[key]
	if digest == 0 {
		delete(b.s.marks, key)
	} else {
		b.s.marks[key] = digest
	}
	b.s.sum += digest
}

// SetGroups replaces the groups (config order).
func (b *Builder) SetGroups(groups []Group) { b.s.Groups = groups }

// SetCatalogued marks the snapshot as holding a catalogue.
func (b *Builder) SetCatalogued() { b.s.Catalogued = true }

func (b *Builder) finish(now time.Time) *Snapshot {
	s := b.s
	s.Version = s.sum & (1<<53 - 1)
	s.BuiltAt = now
	s.index()
	s.public = s.publicView()
	return s
}

// index builds the derived lookups: group membership, commit feeds and the dependency indexes.
func (s *Snapshot) index() {
	s.groupsOf = map[RepoKey][]string{}
	for _, g := range s.Groups {
		for _, m := range g.Members {
			s.groupsOf[m] = append(s.groupsOf[m], g.ID)
		}
	}
	keys := slices.Sorted(maps.Keys(s.Repos))
	s.Latest = feed(s.Repos, keys)
	s.latestByGroup = make(map[string][]CommitRef, len(s.Groups))
	for _, g := range s.Groups {
		s.latestByGroup[g.ID] = feed(s.Repos, g.Members)
	}
	s.providers, s.usedBy = dependencies(s.Repos, keys)
}

// publicView is s without its private and internal repositories, or s itself when it has none.
func (s *Snapshot) publicView() *Snapshot {
	if !slices.ContainsFunc(slices.Collect(maps.Values(s.Repos)), func(e *RepoEntry) bool { return !e.Repo.Public() }) {
		return s
	}
	p := &Snapshot{Version: s.Version, BuiltAt: s.BuiltAt, Catalogued: s.Catalogued, Repos: map[RepoKey]*RepoEntry{}, Aliases: map[RepoKey]RepoKey{}}
	count := map[string]int{}
	for k, e := range s.Repos {
		if e.Repo.Public() {
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
			g.DefaultRepo = "" // private: not shown, and not reported missing either
		}
		p.Groups = append(p.Groups, g)
	}
	for _, src := range s.Sources {
		src.Repos = count[src.ID]
		p.Sources = append(p.Sources, src)
	}
	p.index()
	p.public = p
	return p
}

// dependencies indexes the package names the manifests of keys declare, and inverts the dependencies between the repositories.
func dependencies(repos map[RepoKey]*RepoEntry, keys []RepoKey) (map[pkg][]RepoKey, map[RepoKey][]RepoKey) {
	providers := map[pkg][]RepoKey{}
	for _, k := range keys {
		for _, m := range repos[k].Manifests {
			if p := (pkg{m.Ecosystem, m.Name}); m.Name != "" && !slices.Contains(providers[p], k) {
				providers[p] = append(providers[p], k)
			}
		}
	}
	users := map[RepoKey]map[RepoKey]bool{}
	for _, k := range keys {
		for _, m := range repos[k].Manifests {
			for _, d := range m.Dependencies {
				for _, p := range providers[pkg{m.Ecosystem, d.Name}] {
					if p != k {
						if users[p] == nil {
							users[p] = map[RepoKey]bool{}
						}
						users[p][k] = true
					}
				}
			}
		}
	}
	usedBy := make(map[RepoKey][]RepoKey, len(users))
	for p, us := range users {
		usedBy[p] = slices.Sorted(maps.Keys(us))
	}
	return providers, usedBy
}

// feed merges the commits of keys, newest first.
func feed(repos map[RepoKey]*RepoEntry, keys []RepoKey) []CommitRef {
	var out []CommitRef
	for _, k := range keys {
		if e := repos[k]; e != nil {
			for i := range e.Commits {
				out = append(out, CommitRef{Repo: k, Commit: &e.Commits[i]})
			}
		}
	}
	slices.SortFunc(out, CompareRefs)
	return out
}

// CompareRefs orders a feed: committedAt desc, repo asc, sha desc.
func CompareRefs(a, b CommitRef) int {
	if c := b.Commit.CommittedAt.Compare(a.Commit.CommittedAt); c != 0 {
		return c
	}
	if c := strings.Compare(string(a.Repo), string(b.Repo)); c != 0 {
		return c
	}
	return strings.Compare(b.Commit.SHA, a.Commit.SHA)
}

// Publisher owns the current snapshot. Updates are queued and applied in order at the next publish, which happens Debounce after
// the last update or MaxDelay after the first pending one, whichever comes first.
type Publisher struct {
	debounce, maxDelay time.Duration
	log                *slog.Logger
	cur                atomic.Pointer[Snapshot]
	building           sync.Mutex // one build at a time: a build starts from the snapshot the previous one stored

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
	p.building.Lock()
	defer p.building.Unlock()
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
