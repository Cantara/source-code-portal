// Package memory is an in-memory store.Store for unit tests and synctest bubbles. It implements the same semantics as
// store/sqlite (the conformance suite runs on both) without any I/O, goroutine or clock.
package memory

import (
	"bytes"
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

type Options struct {
	// CacheNamespaces maps a namespace to its byte cap. A namespace not listed is rejected (store.ErrUnknownNamespace).
	CacheNamespaces map[string]int64
}

type repoRow struct {
	repo  store.Repo
	items map[string]*itemRow // by kind
	log   []logRow            // newest first
}

type logRow struct {
	c  store.CommitRow
	id uint64
}

type itemRow struct {
	it      store.Item
	nextDue time.Time
	streak  int
	hasPlan bool
}

type cacheEntry struct {
	data []byte
	seq  uint64
}

type Store struct {
	mu      sync.Mutex
	closed  bool
	repos   map[store.RepoKey]*repoRow
	tasks   map[string]store.TaskState
	parked  map[string]store.Parked
	caps    map[string]int64
	cache   map[string]map[string]*cacheEntry
	bytes   map[string]int64
	seq     uint64
	health  store.Health
	failNxt map[string]error
}

func New(o Options) *Store {
	s := &Store{repos: map[store.RepoKey]*repoRow{}, tasks: map[string]store.TaskState{}, parked: map[string]store.Parked{},
		caps: map[string]int64{}, cache: map[string]map[string]*cacheEntry{}, bytes: map[string]int64{},
		health: store.Health{OK: true}, failNxt: map[string]error{}}
	for ns, c := range o.CacheNamespaces {
		s.caps[ns] = c
		s.cache[ns] = map[string]*cacheEntry{}
	}
	return s
}

// FailNext makes the next call of op (the method name, for example "PutItem") return err, for fault injection in tests.
func (s *Store) FailNext(op string, err error) { s.mu.Lock(); s.failNxt[op] = err; s.mu.Unlock() }

// SetHealth forces the health state; while not OK every write returns store.ErrDegraded.
func (s *Store) SetHealth(h store.Health) { s.mu.Lock(); s.health = h; s.mu.Unlock() }

func (s *Store) Optimize(context.Context) error { return nil }

func (s *Store) enter(ctx context.Context, op string, write bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return store.ErrClosed
	}
	if err := s.failNxt[op]; err != nil {
		delete(s.failNxt, op)
		return err
	}
	if write && !s.health.OK {
		return store.ErrDegraded
	}
	return nil
}

func sec(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return time.Unix(t.Unix(), 0).UTC()
}

func (s *Store) LoadCatalogue(ctx context.Context) ([]store.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "LoadCatalogue", false); err != nil {
		return nil, err
	}
	out := make([]store.Repo, 0, len(s.repos))
	for _, r := range s.repos {
		c := r.repo
		c.Meta = bytes.Clone(c.Meta)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (s *Store) orderedRepos() []*repoRow {
	rs := make([]*repoRow, 0, len(s.repos))
	for _, r := range s.repos {
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].repo.Key.String() < rs[j].repo.Key.String() })
	return rs
}

// LoadItems copies the rows under the lock and invokes fn WITHOUT it, so a callback that (against the contract) calls the
// store does not deadlock.
func (s *Store) LoadItems(ctx context.Context, fn func(store.Item) error) error {
	s.mu.Lock()
	if err := s.enter(ctx, "LoadItems", false); err != nil {
		s.mu.Unlock()
		return err
	}
	var items []store.Item
	for _, r := range s.orderedRepos() {
		kinds := make([]string, 0, len(r.items))
		for k := range r.items {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			it := r.items[k].it
			it.Payload = bytes.Clone(it.Payload)
			items = append(items, it)
		}
	}
	s.mu.Unlock()
	for _, it := range items {
		if err := fn(it); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadPlans(ctx context.Context, fn func(store.PlanRow) error) error {
	s.mu.Lock()
	if err := s.enter(ctx, "LoadPlans", false); err != nil {
		s.mu.Unlock()
		return err
	}
	var rows []store.PlanRow
	for _, r := range s.orderedRepos() {
		for _, ir := range r.items {
			if ir.hasPlan {
				rows = append(rows, store.PlanRow{Key: ir.it.Key, NextDue: ir.nextDue, UnchangedStreak: ir.streak})
			}
		}
	}
	s.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i].Key, rows[j].Key
		if a.Repo != b.Repo {
			return a.Repo.String() < b.Repo.String()
		}
		return a.Kind < b.Kind
	})
	for _, r := range rows {
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadCommits(ctx context.Context, repo store.RepoKey, limit int) ([]store.CommitRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "LoadCommits", false); err != nil {
		return nil, err
	}
	r := s.repos[repo]
	if r == nil || limit <= 0 {
		return nil, nil
	}
	n := min(limit, len(r.log))
	out := make([]store.CommitRow, n)
	for i := range out {
		out[i] = r.log[i].c
		out[i].Payload = bytes.Clone(out[i].Payload)
	}
	return out, nil
}

func (s *Store) ReplaceCatalogue(ctx context.Context, repos []store.Repo) ([]store.RepoKey, error) {
	if err := store.ValidateCatalogue(repos); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "ReplaceCatalogue", true); err != nil {
		return nil, err
	}
	next := map[store.RepoKey]*repoRow{}
	used := map[*repoRow]bool{}
	byGID := map[[2]any]*repoRow{}
	for _, r := range s.repos {
		if r.repo.GitHubID != 0 {
			byGID[[2]any{r.repo.Key.Source, r.repo.GitHubID}] = r
		}
	}
	match := make([]*repoRow, len(repos))
	for i, in := range repos { // pass 1: by GitHub id (rename or transfer keeps the rows)
		if in.GitHubID != 0 {
			if r := byGID[[2]any{in.Key.Source, in.GitHubID}]; r != nil && !used[r] {
				used[r], match[i] = true, r
			}
		}
	}
	for i, in := range repos { // pass 2: by key, never onto a row that belongs to another repository (different non-zero id)
		if match[i] != nil {
			continue
		}
		if r := s.repos[in.Key]; r != nil && !used[r] && (r.repo.GitHubID == 0 || in.GitHubID == 0 || r.repo.GitHubID == in.GitHubID) {
			used[r], match[i] = true, r
		}
	}
	for i, in := range repos {
		in.Meta = bytes.Clone(in.Meta)
		in.SeenAt = sec(in.SeenAt)
		if r := match[i]; r != nil {
			if in.GitHubID == 0 {
				in.GitHubID = r.repo.GitHubID // an unknown id on the incoming side never erases a known one
			}
			r.repo = in
			for _, ir := range r.items {
				ir.it.Key.Repo = in.Key
			}
			next[in.Key] = r
			continue
		}
		next[in.Key] = &repoRow{repo: in, items: map[string]*itemRow{}}
		used[next[in.Key]] = true
	}
	var removed []store.RepoKey
	for k, r := range s.repos {
		if !used[r] {
			removed = append(removed, k)
			tail := "/" + k.String()
			for pk := range s.parked {
				if strings.HasSuffix(pk, tail) {
					delete(s.parked, pk)
				}
			}
		}
	}
	slices.SortFunc(removed, func(a, b store.RepoKey) int { return strings.Compare(a.String(), b.String()) })
	s.repos = next
	return removed, nil
}

func (s *Store) row(k store.RepoKey) (*repoRow, error) {
	r := s.repos[k]
	if r == nil {
		return nil, &store.NoRepoError{Key: k}
	}
	return r, nil
}

func (s *Store) PutItem(ctx context.Context, it store.Item) error {
	if err := store.ValidateItem(it); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "PutItem", true); err != nil {
		return err
	}
	r, err := s.row(it.Key.Repo)
	if err != nil {
		return err
	}
	it.Attempts, it.Error = 0, ""
	it.FetchedAt, it.CheckedAt = sec(it.FetchedAt), sec(it.CheckedAt)
	it.Payload = bytes.Clone(it.Payload)
	if ir := r.items[it.Key.Kind]; ir != nil {
		ir.it = it
	} else {
		r.items[it.Key.Kind] = &itemRow{it: it}
	}
	return nil
}

func (s *Store) Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error {
	if err := k.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "Revalidated", true); err != nil {
		return err
	}
	r, err := s.row(k.Repo)
	if err != nil {
		return err
	}
	ir := r.items[k.Kind]
	if ir == nil || ir.it.State == "error" || ir.it.FetchedAt.IsZero() {
		return store.ErrNoItem
	}
	ir.it.FetchedAt, ir.it.CheckedAt, ir.it.Attempts, ir.it.Error = sec(now), sec(now), 0, ""
	return nil
}

func (s *Store) RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error {
	if err := store.ValidateFailure(k, f); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "RecordFailure", true); err != nil {
		return err
	}
	r, err := s.row(k.Repo)
	if err != nil {
		return err
	}
	ir := r.items[k.Kind]
	if ir == nil {
		r.items[k.Kind] = &itemRow{it: store.Item{Key: k, State: "error", CheckedAt: sec(f.At), Attempts: 1, Error: f.Code}}
		return nil
	}
	if ir.it.FetchedAt.IsZero() {
		ir.it.State = "error"
	}
	ir.it.CheckedAt, ir.it.Attempts, ir.it.Error = sec(f.At), ir.it.Attempts+1, f.Code
	return nil
}

func (s *Store) SavePlans(ctx context.Context, rows []store.PlanRow) error {
	for _, p := range rows {
		if err := store.ValidatePlan(p); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "SavePlans", true); err != nil {
		return err
	}
	for _, p := range rows {
		if p.Key.Repo.IsZero() {
			continue // global kinds are never persisted
		}
		if r := s.repos[p.Key.Repo]; r != nil {
			if ir := r.items[p.Key.Kind]; ir != nil {
				ir.nextDue, ir.streak, ir.hasPlan = sec(p.NextDue), p.UnchangedStreak, true
			}
		}
	}
	return nil
}

func (s *Store) AppendCommits(ctx context.Context, repo store.RepoKey, cs []store.CommitRow, keep int) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if keep <= 0 {
		return &store.InvalidError{Field: "keep", Reason: "must be positive"}
	}
	for _, c := range cs { // validate everything before the first mutation: the call is atomic, like the SQLite transaction
		if err := store.ValidateCommit(c); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "AppendCommits", true); err != nil {
		return err
	}
	r, err := s.row(repo)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, l := range r.log {
		have[l.c.SHA] = true
	}
	for _, c := range cs {
		if !have[c.SHA] {
			have[c.SHA] = true
			s.seq++
			c.CommittedAt, c.Payload = sec(c.CommittedAt), bytes.Clone(c.Payload)
			r.log = append(r.log, logRow{c: c, id: s.seq})
		}
	}
	// "ORDER BY committed_at DESC, id DESC": a later insertion has the larger id.
	sort.Slice(r.log, func(i, j int) bool {
		a, b := r.log[i], r.log[j]
		if !a.c.CommittedAt.Equal(b.c.CommittedAt) {
			return a.c.CommittedAt.After(b.c.CommittedAt)
		}
		return a.id > b.id
	})
	if len(r.log) > keep {
		r.log = r.log[:keep]
	}
	return nil
}

func (s *Store) SaveTask(ctx context.Context, name string, t store.TaskState) error {
	if name == "" || len(name) > 128 {
		return &store.InvalidError{Field: "task", Reason: "name required, at most 128 bytes"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "SaveTask", true); err != nil {
		return err
	}
	t.LastAttempt, t.LastSuccess = sec(t.LastAttempt), sec(t.LastSuccess)
	s.tasks[name] = t
	return nil
}

func (s *Store) LoadTasks(ctx context.Context) (map[string]store.TaskState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "LoadTasks", false); err != nil {
		return nil, err
	}
	out := make(map[string]store.TaskState, len(s.tasks))
	for k, v := range s.tasks {
		out[k] = v
	}
	return out, nil
}

func (s *Store) Park(ctx context.Context, key, reason string, retryAt time.Time) error {
	if key == "" || len(key) > 600 || retryAt.IsZero() {
		return &store.InvalidError{Field: "park", Reason: "key and retry_at required"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "Park", true); err != nil {
		return err
	}
	p, ok := s.parked[key]
	if !ok {
		p.Since = sec(time.Now())
	}
	p.Key, p.Reason, p.RetryAt = key, reason, sec(retryAt)
	s.parked[key] = p
	return nil
}

func (s *Store) Unpark(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "Unpark", true); err != nil {
		return err
	}
	delete(s.parked, key)
	return nil
}

func (s *Store) LoadParked(ctx context.Context) ([]store.Parked, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "LoadParked", false); err != nil {
		return nil, err
	}
	out := make([]store.Parked, 0, len(s.parked))
	for _, p := range s.parked {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (s *Store) GetCache(ctx context.Context, ns, key string) ([]byte, bool, error) {
	if err := store.ValidateCacheArgs(ns, key); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "GetCache", false); err != nil {
		return nil, false, err
	}
	m, ok := s.cache[ns]
	if !ok {
		return nil, false, store.ErrUnknownNamespace
	}
	e := m[key]
	if e == nil {
		return nil, false, nil
	}
	s.seq++
	e.seq = s.seq
	return bytes.Clone(e.data), true, nil
}

func (s *Store) PutCache(ctx context.Context, ns, key string, data []byte) error {
	if err := store.ValidateCacheArgs(ns, key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter(ctx, "PutCache", true); err != nil {
		return err
	}
	m, ok := s.cache[ns]
	if !ok {
		return store.ErrUnknownNamespace
	}
	if int64(len(data)) > s.caps[ns] {
		return store.ErrTooLarge
	}
	if old := m[key]; old != nil {
		s.bytes[ns] -= int64(len(old.data))
	}
	s.seq++
	m[key] = &cacheEntry{data: append([]byte{}, data...), seq: s.seq} // nil is stored (and read back) as empty, like store/sqlite
	s.bytes[ns] += int64(len(data))
	if s.bytes[ns] > s.caps[ns] {
		target := s.caps[ns] - s.caps[ns]/10 // evict down to 90% (hysteresis), exactly like the SQLite implementation
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return m[keys[i]].seq < m[keys[j]].seq })
		for _, k := range keys {
			if s.bytes[ns] <= target || k == key {
				break
			}
			s.bytes[ns] -= int64(len(m[k].data))
			delete(m, k)
		}
	}
	return nil
}

func (s *Store) CacheBytes(ns string) int64 { s.mu.Lock(); defer s.mu.Unlock(); return s.bytes[ns] }

func (s *Store) Check(ctx context.Context, full bool) error    { return ctx.Err() }
func (s *Store) Backup(ctx context.Context, path string) error { return ctx.Err() }
func (s *Store) Health() store.Health                          { s.mu.Lock(); defer s.mu.Unlock(); return s.health }
func (s *Store) Run(ctx context.Context) error                 { <-ctx.Done(); return nil }
func (s *Store) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

var _ store.Store = (*Store)(nil)
