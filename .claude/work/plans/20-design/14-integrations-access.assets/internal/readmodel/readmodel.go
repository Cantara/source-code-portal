// Package readmodel: STUB with the semantics unit 14 relies on (architecture 8.4). Not the real implementation.
package readmodel

import (
	"errors"
	"sync"
	"sync/atomic"
)

type EntryID string
type Slot[T any] struct{ name string }
type Global[T any] struct{ name string }
type Registry struct{ existence string }

// SlotOption mirrors the real option type of unit 03 (CD-IF-11: the stand-in used a bool, the real Register takes options).
type SlotOption func(*slotConfig)
type slotConfig struct{ existence bool }

func Existence() SlotOption { return func(c *slotConfig) { c.existence = true } }

func Register[T any](r *Registry, name string, opts ...SlotOption) Slot[T] {
	var c slotConfig
	for _, o := range opts {
		o(&c)
	}
	if c.existence {
		r.existence = name
	}
	return Slot[T]{name: name}
}
func RegisterGlobal[T any](r *Registry, name string) Global[T] { return Global[T]{name: name} }

var ErrUnknownRepo = errors.New("readmodel: unknown repo")

type Snapshot struct {
	version uint64
	repos   map[EntryID]map[string]any
	globals map[string]any
	exist   string
}

func (s *Snapshot) Version() uint64 { return s.version }
func (s *Snapshot) Repos() []EntryID {
	out := make([]EntryID, 0, len(s.repos))
	for k := range s.repos {
		out = append(out, k)
	}
	return out
}
func Get[T any](s *Snapshot, id EntryID, slot Slot[T]) (T, bool) {
	var zero T
	m, ok := s.repos[id]
	if !ok {
		return zero, false
	}
	v, ok := m[slot.name]
	if !ok {
		return zero, false
	}
	return v.(T), true
}
func GetGlobal[T any](s *Snapshot, g Global[T]) (T, bool) {
	var zero T
	v, ok := s.globals[g.name]
	if !ok {
		return zero, false
	}
	return v.(T), true
}

type Builder struct {
	s *Snapshot
}

func Put[T any](b *Builder, id EntryID, slot Slot[T], v T) error {
	m, ok := b.s.repos[id]
	if !ok {
		if slot.name != b.s.exist {
			return ErrUnknownRepo
		}
		m = map[string]any{}
		b.s.repos[id] = m
	}
	m[slot.name] = v
	return nil
}
func PutGlobal[T any](b *Builder, g Global[T], v T) { b.s.globals[g.name] = v }
func (b *Builder) Remove(id EntryID)                { delete(b.s.repos, id) }
func (b *Builder) View() *Snapshot                  { return b.s }

type Publisher struct {
	mu    sync.Mutex
	cur   atomic.Pointer[Snapshot]
	exist string
}

func NewPublisher(r *Registry) *Publisher {
	p := &Publisher{exist: r.existence}
	p.cur.Store(&Snapshot{repos: map[EntryID]map[string]any{}, globals: map[string]any{}, exist: r.existence})
	return p
}
func (p *Publisher) Current() *Snapshot { return p.cur.Load() }
func (p *Publisher) Apply(fn func(*Builder) error) (*Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.cur.Load()
	n := &Snapshot{version: old.version + 1, repos: map[EntryID]map[string]any{}, globals: map[string]any{}, exist: old.exist}
	for id, m := range old.repos {
		c := make(map[string]any, len(m))
		for k, v := range m {
			c[k] = v
		}
		n.repos[id] = c
	}
	for k, v := range old.globals {
		n.globals[k] = v
	}
	if err := fn(&Builder{s: n}); err != nil {
		return old, err
	}
	p.cur.Store(n)
	return n, nil
}

var ErrInvalidSnapshot = errors.New("readmodel: invalid snapshot")
