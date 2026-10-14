// Package readmodel is a faithful-enough in-memory stand-in of the architecture's section 8.4 (signatures identical) so the
// manifests unit can be compiled and exercised. The real package is designed in another unit.
package readmodel

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type EntryID string
type Slot[T any] struct{ name string }
type Global[T any] struct{ name string }
type Registry struct{}

// SlotOption, Existence, Projector, Validator and Deps mirror the real constructor signatures (03-readmodel-store.md) so tests run unchanged on both.
type SlotOption func()

func Existence() SlotOption { return func() {} }

type Projector func(b *Builder) error
type Validator func(*Snapshot) error
type Deps struct {
	Meter  any
	Tracer any
	Log    any
}

func Register[T any](r *Registry, name string, _ ...SlotOption) Slot[T] { return Slot[T]{name} }
func RegisterGlobal[T any](r *Registry, name string) Global[T]          { return Global[T]{name} }

var ErrUnknownRepo, ErrInvalidSnapshot = errors.New("unknown repo"), errors.New("invalid snapshot")

const existence = "catalog.repo"

type Snapshot struct {
	version uint64
	ids     []EntryID
	repos   map[EntryID]map[string]any
	revs    map[EntryID]uint64
	globals map[string]any
}

func (s *Snapshot) Version() uint64       { return s.version }
func (s *Snapshot) BuiltAt() time.Time    { return time.Time{} }
func (s *Snapshot) Repos() []EntryID      { return s.ids }
func (s *Snapshot) Rev(id EntryID) uint64 { return s.revs[id] }
func Get[T any](s *Snapshot, id EntryID, slot Slot[T]) (T, bool) {
	var z T
	if s == nil {
		return z, false
	}
	v, ok := s.repos[id][slot.name].(T)
	if !ok {
		return z, false
	}
	return v, true
}
func GetGlobal[T any](s *Snapshot, g Global[T]) (T, bool) {
	var z T
	if s == nil {
		return z, false
	}
	v, ok := s.globals[g.name].(T)
	return v, ok
}

type Builder struct {
	s *Snapshot
}

func (b *Builder) clone(id EntryID) map[string]any {
	m := maps(b.s.repos[id])
	b.s.repos[id] = m
	b.s.revs[id] = b.s.version
	return m
}

func maps(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func Put[T any](b *Builder, id EntryID, slot Slot[T], v T) error {
	if _, ok := b.s.repos[id]; !ok {
		if slot.name != existence {
			return ErrUnknownRepo
		}
		b.s.ids = append(slices.Clone(b.s.ids), id)
		slices.Sort(b.s.ids)
	}
	b.clone(id)[slot.name] = v
	return nil
}

// PutGlobal returns an error like the real package (checkReg / nil value); callers must propagate it.
func PutGlobal[T any](b *Builder, g Global[T], v T) error { b.s.globals[g.name] = v; return nil }
func (b *Builder) Remove(id EntryID) {
	delete(b.s.repos, id)
	b.s.ids = slices.DeleteFunc(slices.Clone(b.s.ids), func(x EntryID) bool { return x == id })
}

type Publisher struct {
	mu  sync.Mutex
	cur atomic.Pointer[Snapshot]
}

func NewPublisher(_ *Registry, _ []Projector, _ []Validator, _ Deps) *Publisher {
	p := &Publisher{}
	p.cur.Store(&Snapshot{repos: map[EntryID]map[string]any{}, revs: map[EntryID]uint64{}, globals: map[string]any{}})
	return p
}
func (p *Publisher) Current() *Snapshot { return p.cur.Load() }
func (p *Publisher) Apply(fn func(*Builder) error) (*Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.cur.Load()
	n := &Snapshot{version: old.version + 1, ids: old.ids, repos: maps2(old.repos), revs: maps3(old.revs), globals: maps(old.globals)}
	if err := fn(&Builder{n}); err != nil {
		return old, err // like the real package: the previous snapshot is returned together with the error
	}
	p.cur.Store(n)
	return n, nil
}
func maps2(in map[EntryID]map[string]any) map[EntryID]map[string]any {
	out := make(map[EntryID]map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func maps3(in map[EntryID]uint64) map[EntryID]uint64 {
	out := make(map[EntryID]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
