// Package clustertest is an in-memory cluster backend for tests: a Hub is the cluster, each Dial a member with its own connection.
package clustertest

import (
	"context"
	"maps"
	"slices"
	gosync "sync"
	"time"

	"github.com/cantara/source-code-portal/internal/cluster"
)

// Hub is one cluster: the leader lock, the state map, the delivery ids and the event topic.
type Hub struct {
	mu       gosync.Mutex
	state    map[string][]byte
	seen     map[string]time.Time
	leader   *Backend
	members  []*Backend
	writes   int
	failPuts int
}

// NewHub returns an empty cluster.
func NewHub() *Hub { return &Hub{state: map[string][]byte{}, seen: map[string]time.Time{}} }

// Dial returns a dial function for cluster.New; every call joins a new member.
func (h *Hub) Dial() func(context.Context) (cluster.Backend, error) {
	return func(context.Context) (cluster.Backend, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		b := &Backend{hub: h, up: true, conn: make(chan bool, 16)}
		h.members = append(h.members, b)
		return b, nil
	}
}

// Members returns the members in the order they joined.
func (h *Hub) Members() []*Backend {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.members)
}

// State returns a copy of the state map.
func (h *Hub) State() map[string][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.state)
}

// Writes returns how many puts and deletes reached the state map.
func (h *Hub) Writes() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.writes
}

// FailPuts makes the next n puts fail.
func (h *Hub) FailPuts(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failPuts = n
}

// Set writes the state map directly, as a member that is not under test would.
func (h *Hub) Set(key string, value []byte) {
	h.mu.Lock()
	h.state[key] = value
	watchers := h.watchers()
	h.mu.Unlock()
	for _, w := range watchers {
		w(key, value)
	}
}

// watchers returns the Watch handlers of the connected members. Callers hold h.mu.
func (h *Hub) watchers() []func(string, []byte) {
	var out []func(string, []byte)
	for _, m := range h.members {
		if m.up && !m.closed {
			out = append(out, m.watch...)
		}
	}
	return out
}

// Backend is one member's connection to the hub.
type Backend struct {
	hub    *Hub
	up     bool
	closed bool
	conn   chan bool
	watch  []func(string, []byte)
	subs   []func([]byte)
}

var _ cluster.Backend = (*Backend)(nil)

// Disconnect cuts the member off: it loses the lock, receives nothing and its calls fail until Reconnect.
func (b *Backend) Disconnect() {
	h := b.hub
	h.mu.Lock()
	b.up = false
	if h.leader == b {
		h.leader = nil
	}
	h.mu.Unlock()
	b.conn <- false
}

// Reconnect restores the member's connection.
func (b *Backend) Reconnect() {
	b.hub.mu.Lock()
	b.up = true
	b.hub.mu.Unlock()
	b.conn <- true
}

// Leads reports whether the member holds the lock.
func (b *Backend) Leads() bool {
	b.hub.mu.Lock()
	defer b.hub.mu.Unlock()
	return b.hub.leader == b
}

// Closed reports whether the member closed its connection.
func (b *Backend) Closed() bool {
	b.hub.mu.Lock()
	defer b.hub.mu.Unlock()
	return b.closed
}

// lock returns the hub locked, or an error when the member is cut off.
func (b *Backend) lock() (*Hub, error) {
	b.hub.mu.Lock()
	if !b.up || b.closed {
		b.hub.mu.Unlock()
		return nil, cluster.ErrDisconnected
	}
	return b.hub, nil
}

func (b *Backend) TryLead(context.Context) (bool, error) {
	h, err := b.lock()
	if err != nil {
		return false, err
	}
	defer h.mu.Unlock()
	if h.leader == nil {
		h.leader = b
	}
	return h.leader == b, nil
}

func (b *Backend) Put(_ context.Context, key string, value []byte) error {
	h, err := b.lock()
	if err != nil {
		return err
	}
	if h.failPuts > 0 {
		h.failPuts--
		h.mu.Unlock()
		return cluster.ErrDisconnected
	}
	h.state[key] = slices.Clone(value)
	h.writes++
	watchers := h.watchers()
	h.mu.Unlock()
	for _, w := range watchers {
		w(key, value)
	}
	return nil
}

func (b *Backend) Delete(_ context.Context, key string) error {
	h, err := b.lock()
	if err != nil {
		return err
	}
	defer h.mu.Unlock()
	delete(h.state, key)
	h.writes++
	return nil
}

func (b *Backend) Keys(context.Context) ([]string, error) {
	h, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer h.mu.Unlock()
	return slices.Sorted(maps.Keys(h.state)), nil
}

func (b *Backend) Entries(context.Context) (map[string][]byte, error) {
	h, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer h.mu.Unlock()
	return maps.Clone(h.state), nil
}

func (b *Backend) Watch(_ context.Context, fn func(string, []byte)) error {
	h, err := b.lock()
	if err != nil {
		return err
	}
	defer h.mu.Unlock()
	b.watch = append(b.watch, fn)
	return nil
}

func (b *Backend) Once(_ context.Context, id string, ttl time.Duration) (bool, error) {
	h, err := b.lock()
	if err != nil {
		return false, err
	}
	defer h.mu.Unlock()
	now := time.Now()
	if at, ok := h.seen[id]; ok && now.Sub(at) < ttl {
		return false, nil
	}
	h.seen[id] = now
	return true, nil
}

func (b *Backend) Publish(_ context.Context, msg []byte) error {
	h, err := b.lock()
	if err != nil {
		return err
	}
	var subs []func([]byte)
	for _, m := range h.members {
		if m.up && !m.closed {
			subs = append(subs, m.subs...)
		}
	}
	h.mu.Unlock()
	for _, s := range subs {
		s(msg)
	}
	return nil
}

func (b *Backend) Subscribe(_ context.Context, fn func([]byte)) error {
	h, err := b.lock()
	if err != nil {
		return err
	}
	defer h.mu.Unlock()
	b.subs = append(b.subs, fn)
	return nil
}

func (b *Backend) Connected() <-chan bool { return b.conn }

func (b *Backend) Close(context.Context) error {
	h := b.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	b.closed = true
	if h.leader == b {
		h.leader = nil
	}
	return nil
}
