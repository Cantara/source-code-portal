package webhook

import (
	"container/list"
	"sync"
	"time"
)

// dedupe is a bounded delivery-id set: insertion-ordered (all entries share one TTL, so insertion order
// is expiry order), lazily evicted on every call, no goroutine. A hit does not refresh the entry: a
// replayed id must not extend its own life. Exactness is not required (webhooks are hints).
type dedupe struct {
	mu  sync.Mutex
	ttl time.Duration
	max int
	l   *list.List // front = oldest
	m   map[string]*list.Element
}

type ddRec struct {
	key string
	at  time.Time
}

func newDedupe(ttl time.Duration, max int) *dedupe {
	return &dedupe{ttl: ttl, max: max, l: list.New(), m: make(map[string]*list.Element, 64)}
}

func (d *dedupe) evict(now time.Time) {
	for e := d.l.Front(); e != nil; e = d.l.Front() {
		if now.Sub(e.Value.(ddRec).at) < d.ttl {
			return
		}
		d.l.Remove(e)
		delete(d.m, e.Value.(ddRec).key)
	}
}

// Seen reports whether key was recorded within the TTL.
func (d *dedupe) Seen(key string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.evict(now)
	_, ok := d.m[key]
	return ok
}

// Record stores key after a successful enqueue; the oldest entry goes when the cap is reached.
func (d *dedupe) Record(key string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.evict(now)
	if _, ok := d.m[key]; ok {
		return
	}
	d.m[key] = d.l.PushBack(ddRec{key, now})
	for d.l.Len() > d.max {
		e := d.l.Front()
		d.l.Remove(e)
		delete(d.m, e.Value.(ddRec).key)
	}
}

func (d *dedupe) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.l.Len()
}
