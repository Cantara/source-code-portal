package media

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

// entry is a cached external image or a remembered failure.
type entry struct {
	key      string
	body     []byte
	kind     Kind
	etag     string
	expires  time.Time
	negative int // HTTP status of a remembered failure, 0 for a body
	size     int64
}

// lru is a byte-bounded LRU with per-entry expiry. Safe for concurrent use. Expired entries are returned as a miss with
// stale=true so a handler may serve them when the refresh fails (stale-while-error, bounded by staleGrace).
type lru struct {
	mu    sync.Mutex
	max   int64
	used  int64
	ll    *list.List
	m     map[string]*list.Element
	grace time.Duration
}

func newLRU(maxBytes int64, grace time.Duration) *lru {
	return &lru{max: maxBytes, ll: list.New(), m: map[string]*list.Element{}, grace: grace}
}

// get returns the entry and whether it is fresh. An expired entry older than grace is dropped.
func (c *lru) get(key string, now time.Time) (e *entry, fresh bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.m[key]
	if !ok {
		return nil, false
	}
	e = el.Value.(*entry)
	if now.After(e.expires.Add(c.grace)) {
		c.removeLocked(el)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return e, !now.After(e.expires)
}

func (c *lru) put(e *entry) {
	e.size = int64(len(e.body)) + int64(len(e.key)) + 128
	if e.size > c.max {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[e.key]; ok {
		c.removeLocked(el)
	}
	c.m[e.key] = c.ll.PushFront(e)
	c.used += e.size
	for c.used > c.max {
		c.removeLocked(c.ll.Back())
	}
}

func (c *lru) removeLocked(el *list.Element) {
	e := c.ll.Remove(el).(*entry)
	delete(c.m, e.key)
	c.used -= e.size
}

func (c *lru) stats() (bytes int64, entries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used, c.ll.Len()
}

// flight collapses concurrent fetches of one key. The leader runs fn inline on its own goroutine with a context that
// survives its caller's cancellation; followers wait on their own context. No goroutine is started.
type flight struct {
	mu sync.Mutex
	m  map[string]*call
}

type call struct {
	done chan struct{}
	e    *entry
	err  error
}

// errFlightPanic is what followers of a leader that panicked receive.
var errFlightPanic = errors.New("media: the leading fetch failed")

func newFlight() *flight { return &flight{m: map[string]*call{}} }

func (f *flight) do(ctx context.Context, key string, fn func() (*entry, error)) (*entry, error, bool) {
	f.mu.Lock()
	if c, ok := f.m[key]; ok {
		f.mu.Unlock()
		select {
		case <-c.done:
			return c.e, c.err, true
		case <-ctx.Done():
			return nil, ctx.Err(), true
		}
	}
	c := &call{done: make(chan struct{})}
	f.m[key] = c
	f.mu.Unlock()
	finished := false
	defer func() {
		if !finished { // fn panicked: followers get an error result, not (nil, nil) (verification V-03); the panic continues
			c.e, c.err = nil, errFlightPanic
		}
		f.mu.Lock()
		delete(f.m, key)
		f.mu.Unlock()
		close(c.done)
	}()
	c.e, c.err = fn()
	finished = true
	return c.e, c.err, false
}

func (f *flight) size() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.m) }

var farFuture = time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
