// Package cluster lets several instances serve one portal. One instance leads and runs the sync jobs; it shares every recorded
// outcome through the backend (Hazelcast), and every instance applies what it receives as if the job had run there, so all of them
// serve the same data. Webhook deliveries are de-duplicated and their events delivered to every instance through the backend too.
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

const (
	leadInterval = 2 * time.Second  // how often an instance that does not lead tries the lock
	dialMin      = time.Second      // first wait after a failed dial; doubles to dialMax
	dialMax      = 30 * time.Second //
	retryWrite   = time.Second      // wait after a failed write
	callTimeout  = 2 * time.Second  // for calls made while serving a request
	closeTimeout = 10 * time.Second
)

// ErrDisconnected is returned by calls made while the instance has no connection to the cluster.
var ErrDisconnected = errors.New("cluster: not connected")

// Backend is the shared state of the cluster: Hazelcast in production, memory in tests (clustertest).
type Backend interface {
	// TryLead takes the leader lock without waiting and reports whether this instance holds it. The lock is released when the
	// instance closes or loses its connection.
	TryLead(ctx context.Context) (bool, error)
	// Put, Delete, Keys and Entries work on the state map: job key to encoded outcome.
	Put(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Keys(ctx context.Context) ([]string, error)
	Entries(ctx context.Context) (map[string][]byte, error)
	// Watch calls fn for every put to the state map, by any instance, this one included. fn must not block.
	Watch(ctx context.Context, fn func(key string, value []byte)) error
	// Once reports whether id is seen for the first time within ttl.
	Once(ctx context.Context, id string, ttl time.Duration) (bool, error)
	// Publish sends msg to every instance's Subscribe handler. fn must not block.
	Publish(ctx context.Context, msg []byte) error
	Subscribe(ctx context.Context, fn func(msg []byte)) error
	// Connected receives false when the connection is lost and true when it is back.
	Connected() <-chan bool
	Close(ctx context.Context) error
}

// Engine is what a member drives: the sync engine.
type Engine interface {
	Warm() <-chan struct{}
	Lead(on bool)
	Receive(ctx context.Context, r sync.Shared)
	Records() []sync.Shared
	Notify(ev sync.Event, repo snapshot.RepoKey)
	NotifySource(ev sync.Event, source string)
}

// Member is this instance's part in the cluster.
type Member struct {
	dial func(context.Context) (Backend, error)
	log  *slog.Logger

	leader    atomic.Bool
	connected atomic.Bool
	backend   atomic.Pointer[backendRef]

	out, in queue // writes to the state map; received entries to apply
}

type backendRef struct{ Backend }

// New returns a member that connects with dial.
func New(dial func(context.Context) (Backend, error), log *slog.Logger) *Member {
	return &Member{dial: dial, log: log, out: newQueue(), in: newQueue()}
}

// Leader reports whether this instance runs the jobs.
func (m *Member) Leader() bool { return m.leader.Load() }

// Connected reports whether this instance has a connection to the cluster.
func (m *Member) Connected() bool { return m.connected.Load() }

// Run connects, applies the shared state to e, takes part in the leader election and keeps e in step with the leader until ctx ends.
// It waits for e's warm start first, and keeps trying to connect when the cluster is unreachable.
func (m *Member) Run(ctx context.Context, e Engine) error {
	select {
	case <-e.Warm():
	case <-ctx.Done():
		return nil
	}
	for {
		b := m.connect(ctx)
		if b == nil {
			return nil
		}
		err := m.session(ctx, b, e)
		if ctx.Err() != nil {
			return nil
		}
		m.log.Warn("cluster session ended; reconnecting", "err", err)
	}
}

// session takes part in the cluster over b until ctx ends or a subscription fails, then closes b.
func (m *Member) session(ctx context.Context, b Backend, e Engine) error {
	defer func() {
		m.stepDown(e)
		m.connected.Store(false)
		m.backend.Store(nil)
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()
		if err := b.Close(cctx); err != nil {
			m.log.Warn("cluster close failed", "err", err)
		}
	}()
	var wg gosync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg.Go(func() { m.write(ctx, b) })
	wg.Go(func() { m.apply(ctx, e) })
	if err := b.Watch(ctx, m.in.put); err != nil {
		return err
	}
	if err := b.Subscribe(ctx, func(msg []byte) { deliver(e, msg) }); err != nil {
		return err
	}
	m.backend.Store(&backendRef{b})
	m.connected.Store(true)
	m.resync(ctx, b, e)

	tick := time.NewTicker(leadInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case up := <-b.Connected():
			switch {
			case !up && m.connected.Load():
				m.connected.Store(false)
				m.stepDown(e)
				m.log.Warn("cluster connection lost")
			case up && !m.connected.Load():
				m.connected.Store(true)
				m.log.Info("cluster connection back")
				m.resync(ctx, b, e)
			}
		case <-tick.C:
			if !m.connected.Load() || m.leader.Load() {
				continue
			}
			ok, err := b.TryLead(ctx)
			if err != nil {
				m.log.Warn("cluster leader lock failed", "err", err)
				continue
			}
			if ok {
				m.seed(ctx, b, e)
				m.leader.Store(true)
				e.Lead(true)
				m.log.Info("leading the cluster")
			}
		}
	}
}

// connect dials until it succeeds or ctx ends (nil).
func (m *Member) connect(ctx context.Context) Backend {
	wait := dialMin
	for {
		b, err := m.dial(ctx)
		if err == nil {
			m.log.Info("cluster connected")
			return b
		}
		m.log.Warn("cluster unreachable; serving the last good data", "err", err, "retry_in", wait.String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		wait = min(2*wait, dialMax)
	}
}

// stepDown stops e and drops the writes not yet made: the next leader's are newer.
func (m *Member) stepDown(e Engine) {
	m.out.clear()
	if m.leader.Swap(false) {
		e.Lead(false)
		m.log.Info("no longer leading the cluster")
	}
}

// resync applies the whole state map, listings first, so an instance that joins or reconnects has everything the leader recorded.
func (m *Member) resync(ctx context.Context, b Backend, e Engine) {
	entries, err := b.Entries(ctx)
	if err != nil {
		m.log.Warn("cluster state not loaded", "err", err)
		return
	}
	recs := make([]sync.Shared, 0, len(entries))
	for k, v := range entries {
		r, err := decode(v)
		if err != nil {
			m.log.Warn("cluster state entry unreadable", "key", k, "err", err)
			continue
		}
		recs = append(recs, r)
	}
	slices.SortFunc(recs, func(a, b sync.Shared) int {
		if (a.Catalogue != nil) != (b.Catalogue != nil) {
			if a.Catalogue != nil {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Item.Key, b.Item.Key)
	})
	for _, r := range recs {
		e.Receive(ctx, r)
	}
	m.log.Info("cluster state loaded", "items", len(recs))
}

// seed makes the state map mirror the engine when this instance takes the lead: what the map lacks is written, what the engine has
// no outcome for is deleted.
func (m *Member) seed(ctx context.Context, b Backend, e Engine) {
	keys, err := b.Keys(ctx)
	if err != nil {
		m.log.Warn("cluster state keys not read", "err", err)
		return
	}
	stale := map[string]bool{}
	for _, k := range keys {
		stale[k] = true
	}
	added := 0
	for _, r := range e.Records() {
		if !stale[r.Item.Key] {
			m.out.put(r.Item.Key, encode(r))
			added++
		}
		delete(stale, r.Item.Key)
	}
	for k := range stale {
		m.out.put(k, nil)
	}
	if added > 0 || len(stale) > 0 {
		m.log.Info("cluster state filled", "written", added, "deleted", len(stale))
	}
}

// Share queues an outcome recorded by this instance for the state map. It never blocks; a newer outcome for the same key replaces a
// queued one.
func (m *Member) Share(r sync.Shared) {
	m.out.put(r.Item.Key, encode(r))
	for _, k := range r.Removed {
		m.out.put(k, nil)
	}
}

// First reports whether a webhook delivery id is seen for the first time by any instance.
func (m *Member) First(ctx context.Context, id string) (bool, error) {
	b := m.current()
	if b == nil {
		return false, ErrDisconnected
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return b.Once(ctx, "delivery/"+id, 15*time.Minute)
}

// Event sends a webhook event to every instance's engine: per repository when repo is set, else per source.
func (m *Member) Event(ctx context.Context, ev sync.Event, repo snapshot.RepoKey, source string) error {
	b := m.current()
	if b == nil {
		return ErrDisconnected
	}
	msg, _ := json.Marshal(event{Event: ev, Repo: repo, Source: source}) // fixed struct of strings and an integer
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return b.Publish(ctx, msg)
}

func (m *Member) current() Backend {
	if ref := m.backend.Load(); ref != nil && m.connected.Load() {
		return ref.Backend
	}
	return nil
}

type event struct {
	Event  sync.Event       `json:"event"`
	Repo   snapshot.RepoKey `json:"repo,omitempty"`
	Source string           `json:"source,omitempty"`
}

func deliver(e Engine, msg []byte) {
	var ev event
	if json.Unmarshal(msg, &ev) != nil {
		return
	}
	if ev.Repo != "" {
		e.Notify(ev.Event, ev.Repo)
	} else if ev.Source != "" {
		e.NotifySource(ev.Event, ev.Source)
	}
}

// write drains the outgoing queue into the state map, in order, retrying failed writes until ctx ends.
func (m *Member) write(ctx context.Context, b Backend) {
	for {
		key, value, ok := m.out.take(ctx)
		if !ok {
			return
		}
		var err error
		if value == nil {
			err = b.Delete(ctx, key)
		} else {
			err = b.Put(ctx, key, value)
		}
		if err != nil && ctx.Err() == nil && m.leader.Load() {
			m.log.Warn("cluster write failed; retrying", "key", key, "err", err)
			m.out.retry(key, value)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryWrite):
			}
		}
	}
}

// apply drains received entries into the engine.
func (m *Member) apply(ctx context.Context, e Engine) {
	for {
		key, value, ok := m.in.take(ctx)
		if !ok {
			return
		}
		r, err := decode(value)
		if err != nil {
			m.log.Warn("cluster state entry unreadable", "key", key, "err", err)
			continue
		}
		e.Receive(ctx, r)
	}
}

// queue is a first-in first-out queue of keys with the latest value of each: a key queued again keeps its place and takes the new
// value. A nil value is a deletion.
type queue struct {
	mu    gosync.Mutex
	vals  map[string][]byte
	order []string
	wake  chan struct{}
}

func newQueue() queue { return queue{vals: map[string][]byte{}, wake: make(chan struct{}, 1)} }

func (q *queue) put(key string, value []byte) {
	q.mu.Lock()
	if _, queued := q.vals[key]; !queued {
		q.order = append(q.order, key)
	}
	q.vals[key] = value
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) clear() {
	q.mu.Lock()
	clear(q.vals)
	q.order = nil
	q.mu.Unlock()
}

// retry queues a failed write again unless a newer value was queued meanwhile.
func (q *queue) retry(key string, value []byte) {
	q.mu.Lock()
	_, queued := q.vals[key]
	q.mu.Unlock()
	if !queued {
		q.put(key, value)
	}
}

func (q *queue) take(ctx context.Context) (string, []byte, bool) {
	for {
		q.mu.Lock()
		if len(q.order) > 0 {
			key := q.order[0]
			q.order = q.order[1:]
			value := q.vals[key]
			delete(q.vals, key)
			q.mu.Unlock()
			return key, value, true
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", nil, false
		case <-q.wake:
		}
	}
}

// record is the encoding of a shared outcome in the state map.
type record struct {
	Rev       int64       `json:"rev"`
	Source    string      `json:"source"`
	Item      item        `json:"item"`
	Catalogue *[]repoJSON `json:"catalogue,omitempty"` // present for listings, even when empty
}

type item struct {
	Key        string           `json:"key"`
	Kind       string           `json:"kind"`
	Repo       snapshot.RepoKey `json:"repo,omitempty"`
	State      snapshot.State   `json:"state"`
	FetchedAt  time.Time        `json:"fetchedAt"`
	CheckedAt  time.Time        `json:"checkedAt"`
	NextDue    time.Time        `json:"nextDue"`
	Attempts   int              `json:"attempts,omitempty"`
	ErrCode    string           `json:"error,omitempty"`
	ContentSHA string           `json:"contentSha,omitempty"`
	Payload    []byte           `json:"payload,omitempty"`
}

type repoJSON struct {
	Key      snapshot.RepoKey `json:"key"`
	Source   string           `json:"source"`
	GitHubID int64            `json:"githubId"`
}

func encode(r sync.Shared) []byte {
	it := r.Item
	rec := record{Rev: r.Rev, Source: r.Source, Item: item{Key: it.Key, Kind: it.Kind, Repo: it.Repo, State: it.State, FetchedAt: it.FetchedAt,
		CheckedAt: it.CheckedAt, NextDue: it.NextDue, Attempts: it.Attempts, ErrCode: it.ErrCode, ContentSHA: it.ContentSHA, Payload: it.Payload}}
	if r.Catalogue != nil {
		repos := make([]repoJSON, 0, len(r.Catalogue))
		for _, c := range r.Catalogue {
			repos = append(repos, repoJSON{Key: c.Key, Source: c.ID.Source, GitHubID: c.ID.GitHubID})
		}
		rec.Catalogue = &repos
	}
	b, _ := json.Marshal(rec) // strings, integers, times and bytes always marshal
	return b
}

func decode(b []byte) (sync.Shared, error) {
	var rec record
	if err := json.Unmarshal(b, &rec); err != nil {
		return sync.Shared{}, err
	}
	if rec.Item.Key == "" || rec.Item.Kind == "" {
		return sync.Shared{}, errors.New("cluster: record without key or kind")
	}
	it := rec.Item
	r := sync.Shared{Rev: rec.Rev, Source: rec.Source, Item: store.Item{Key: it.Key, Kind: it.Kind, Repo: it.Repo, State: it.State,
		FetchedAt: it.FetchedAt, CheckedAt: it.CheckedAt, NextDue: it.NextDue, Attempts: it.Attempts, ErrCode: it.ErrCode,
		ContentSHA: it.ContentSHA, Payload: it.Payload}}
	if rec.Catalogue != nil {
		r.Catalogue = make([]store.Repo, 0, len(*rec.Catalogue))
		for _, c := range *rec.Catalogue {
			r.Catalogue = append(r.Catalogue, store.Repo{Key: c.Key, ID: snapshot.RepoID{Source: c.Source, GitHubID: c.GitHubID}})
		}
	}
	return r, nil
}
