package live

import (
	"context"
	"errors"
	"sync"
)

var (
	errPerKey   = errors.New("live: per-client stream limit reached")
	errTotal    = errors.New("live: process stream limit reached")
	errDraining = errors.New("live: draining")
)

// broker counts open streams (per client key and in total) and owns the shutdown signal that every stream selects on.
// It starts no goroutine.
type broker struct {
	maxPerKey, maxTotal int

	mu       sync.Mutex
	perKey   map[string]int
	total    int
	draining bool
	idle     chan struct{} // closed when total reaches 0 while draining
	done     chan struct{} // closed by Drain: every stream wakes up
}

func newBroker(maxPerKey, maxTotal int) *broker {
	return &broker{maxPerKey: maxPerKey, maxTotal: maxTotal, perKey: map[string]int{}, done: make(chan struct{})}
}

// acquire reserves one stream slot. The returned release is idempotent.
func (b *broker) acquire(key string) (release func(), err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.draining:
		return nil, errDraining
	case b.total >= b.maxTotal:
		return nil, errTotal
	case b.perKey[key] >= b.maxPerKey:
		return nil, errPerKey
	}
	b.total++
	b.perKey[key]++
	var once sync.Once
	return func() { once.Do(func() { b.release(key) }) }, nil
}

func (b *broker) release(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total--
	if b.perKey[key]--; b.perKey[key] <= 0 {
		delete(b.perKey, key)
	}
	if b.draining && b.total == 0 && b.idle != nil {
		close(b.idle)
		b.idle = nil
	}
}

func (b *broker) open() int { b.mu.Lock(); defer b.mu.Unlock(); return b.total }

// drain tells every stream to finish and waits until the last one has returned or ctx ends. Idempotent.
func (b *broker) drain(ctx context.Context) error {
	b.mu.Lock()
	if !b.draining {
		b.draining = true
		close(b.done)
	}
	if b.total == 0 {
		b.mu.Unlock()
		return nil
	}
	if b.idle == nil {
		b.idle = make(chan struct{})
	}
	idle := b.idle
	b.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
