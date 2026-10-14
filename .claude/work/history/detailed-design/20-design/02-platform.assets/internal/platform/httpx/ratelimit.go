package httpx

import (
	"context"
	"hash/maphash"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
)

const shardCount = 16

// LimiterOptions configures one keyed token-bucket limiter.
type LimiterOptions struct {
	Rule    Rate
	MaxKeys int           // total across shards; 0 means 50000
	IdleTTL time.Duration // 0 means 10 minutes
	// Class and Rejected make the limiter count every refusal as scp_ratelimit_rejected_total{class}; Rejected may be nil.
	// Counting here (not in the middleware) means a limiter used directly by another unit (webhook's invalid-signature
	// budget) is counted the same way, with no bridge.
	Class    string
	Rejected metric.Int64Counter
}

// Limiter is a sharded keyed token bucket with a hard key cap. When a shard is
// full, unseen keys share that shard's overflow bucket, so an attacker rotating
// keys cannot grow memory and cannot get fresh buckets either.
type Limiter struct {
	class    string
	rejected metric.Int64Counter
	seed     maphash.Seed
	lim      rate.Limit
	burst    int
	ttl      time.Duration
	perCap   int
	shards   [shardCount]shard
}

type shard struct {
	mu       sync.Mutex
	m        map[string]*entry
	overflow *rate.Limiter
}

type entry struct {
	l    *rate.Limiter
	last time.Time
}

func NewLimiter(o LimiterOptions) *Limiter {
	if o.MaxKeys <= 0 {
		o.MaxKeys = 50000
	}
	if o.IdleTTL <= 0 {
		o.IdleTTL = 10 * time.Minute
	}
	l := &Limiter{
		class:    o.Class,
		rejected: o.Rejected,
		seed:     maphash.MakeSeed(),
		lim:      rate.Limit(float64(o.Rule.PerMinute) / 60),
		burst:    o.Rule.Burst,
		ttl:      o.IdleTTL,
		perCap:   max(1, o.MaxKeys/shardCount),
	}
	for i := range l.shards {
		l.shards[i].m = make(map[string]*entry)
		l.shards[i].overflow = rate.NewLimiter(l.lim, l.burst)
	}
	return l
}

func (l *Limiter) limiter(key string, now time.Time) (*rate.Limiter, *shard) {
	s := &l.shards[maphash.String(l.seed, key)%shardCount]
	if e, ok := s.m[key]; ok {
		e.last = now
		return e.l, s
	}
	if len(s.m) >= l.perCap {
		return s.overflow, s
	}
	e := &entry{l: rate.NewLimiter(l.lim, l.burst), last: now}
	s.m[key] = e
	return e.l, s
}

// Allow consumes one token for key and reports whether the request may proceed.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	s := &l.shards[maphash.String(l.seed, key)%shardCount]
	s.mu.Lock()
	rl, _ := l.limiter(key, now)
	ok := rl.AllowN(now, 1)
	s.mu.Unlock()
	if !ok && l.rejected != nil { // outside the shard lock
		l.rejected.Add(context.Background(), 1, metric.WithAttributes(attribute.String("class", l.class)))
	}
	return ok
}

// RetryAfter is the whole seconds until one token is available for key (at
// least 1), for the Retry-After header.
func (l *Limiter) RetryAfter(key string) time.Duration {
	if l.lim <= 0 {
		return time.Minute
	}
	d := time.Duration(float64(time.Second) / float64(l.lim))
	return max(d, time.Second).Round(time.Second)
}

// Len is the number of tracked keys (overflow buckets not counted).
func (l *Limiter) Len() int {
	n := 0
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		n += len(s.m)
		s.mu.Unlock()
	}
	return n
}

// Sweep evicts keys idle for longer than the TTL and returns how many.
func (l *Limiter) Sweep() int {
	cut := time.Now().Add(-l.ttl)
	n := 0
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		for k, e := range s.m {
			if e.last.Before(cut) {
				delete(s.m, k)
				n++
			}
		}
		s.mu.Unlock()
	}
	return n
}

// Run sweeps every ttl/4 until ctx is cancelled. It is a lifecycle worker stage.
func (l *Limiter) Run(ctx context.Context) error {
	t := time.NewTicker(max(l.ttl/4, time.Second))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			l.Sweep()
		}
	}
}

// RunSweepers sweeps every limiter on one ticker until ctx is cancelled: the body of the single "ratelimit-sweeper"
// lifecycle stage. The interval is a quarter of the shortest idle TTL (at least one second).
func RunSweepers(ctx context.Context, ls ...*Limiter) error {
	every := time.Hour
	for _, l := range ls {
		every = min(every, l.ttl/4)
	}
	t := time.NewTicker(max(every, time.Second))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			for _, l := range ls {
				l.Sweep()
			}
		}
	}
}
