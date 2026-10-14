// Package readmodel is a compile-checked prototype of the feature-agnostic snapshot kernel.
// It knows nothing about commits, READMEs or manifests: features own typed slots.
package readmodel

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type RepoID string

// Slot is a typed handle to one per-repository value kind (for example commits.View).
// T must be treated as immutable once Put.
type Slot[T any] struct {
	id   int
	name string
}

// Global is a typed handle to one snapshot-wide value (catalogue, derived indexes).
type Global[T any] struct {
	id   int
	name string
}

// Registry is filled once in the composition root (cmd/scp), then frozen. No init(), no globals.
type Registry struct {
	slots   []string
	globals []string
	frozen  bool
}

func Register[T any](r *Registry, name string) Slot[T] {
	if r.frozen {
		panic("readmodel: Register after Freeze")
	}
	r.slots = append(r.slots, name)
	return Slot[T]{id: len(r.slots) - 1, name: name}
}

func RegisterGlobal[T any](r *Registry, name string) Global[T] {
	if r.frozen {
		panic("readmodel: RegisterGlobal after Freeze")
	}
	r.globals = append(r.globals, name)
	return Global[T]{id: len(r.globals) - 1, name: name}
}

func (r *Registry) Freeze() { r.frozen = true }

// entry is immutable; Builder replaces whole entries (structural sharing at repository granularity).
type entry struct {
	slots []any
	rev   uint64 // snapshot version at which this entry last changed: per-repository ETag / SSE granularity
}

type Snapshot struct {
	version uint64
	builtAt time.Time
	repos   map[RepoID]*entry
	names   []RepoID // sorted, shared between snapshots while membership is unchanged
	globals []any
	changed ChangeSet
}

// ChangeSet describes what differs from the previous snapshot (drives SSE and ETags).
type ChangeSet struct {
	Version uint64
	Repos   []RepoID
	Globals []string
	Resync  bool // too much changed, or first snapshot: clients must refetch everything
}

func (s *Snapshot) Version() uint64    { return s.version }
func (s *Snapshot) BuiltAt() time.Time { return s.builtAt }
func (s *Snapshot) Repos() []RepoID    { return s.names } // callers must not mutate
func (s *Snapshot) Changed() ChangeSet { return s.changed }

// Rev returns the snapshot version at which the repository entry last changed (0 if absent).
func (s *Snapshot) Rev(id RepoID) uint64 {
	if e := s.repos[id]; e != nil {
		return e.rev
	}
	return 0
}

func (s *Snapshot) Has(id RepoID) bool { _, ok := s.repos[id]; return ok }

func Get[T any](s *Snapshot, id RepoID, slot Slot[T]) (v T, ok bool) {
	e := s.repos[id]
	if e == nil || slot.id >= len(e.slots) || e.slots[slot.id] == nil {
		return v, false
	}
	v, ok = e.slots[slot.id].(T)
	return v, ok
}

func GetGlobal[T any](s *Snapshot, g Global[T]) (v T, ok bool) {
	if g.id >= len(s.globals) || s.globals[g.id] == nil {
		return v, false
	}
	v, ok = s.globals[g.id].(T)
	return v, ok
}

// Builder is a single-use, single-goroutine copy-on-write view over the previous snapshot.
type Builder struct {
	reg      *Registry
	prev     *Snapshot
	repos    map[RepoID]*entry
	globals  []any
	touched  map[RepoID]struct{}
	gtouched map[string]struct{}
	members  bool // membership changed -> names must be rebuilt
}

func Put[T any](b *Builder, id RepoID, slot Slot[T], v T) {
	old := b.repos[id]
	n := &entry{slots: make([]any, len(b.reg.slots))}
	if old != nil {
		copy(n.slots, old.slots)
	} else {
		b.members = true
	}
	n.slots[slot.id] = v
	b.repos[id] = n
	b.touched[id] = struct{}{}
}

func PutGlobal[T any](b *Builder, g Global[T], v T) {
	b.globals[g.id] = v
	b.gtouched[g.name] = struct{}{}
}

func (b *Builder) Remove(id RepoID) {
	if _, ok := b.repos[id]; ok {
		delete(b.repos, id)
		b.members = true
		b.touched[id] = struct{}{}
	}
}

// View lets projectors read the snapshot being built (including this batch's changes).
func (b *Builder) View() *Snapshot {
	return &Snapshot{repos: b.repos, globals: b.globals, names: b.names()}
}

func (b *Builder) names() []RepoID {
	if !b.members && b.prev != nil {
		return b.prev.names
	}
	return slices.Sorted(maps.Keys(b.repos))
}

// Validator runs on every candidate snapshot before publication; a failing snapshot is never published.
type Validator func(*Snapshot) error

// Projector derives cross-repository indexes (latest N, reverse deps) from the candidate snapshot.
type Projector func(b *Builder) error

type Publisher struct {
	reg        *Registry
	cur        atomic.Pointer[Snapshot]
	mu         sync.Mutex // serialises builders (single logical writer)
	projectors []Projector
	validators []Validator

	subMu sync.Mutex
	subs  map[*sub]struct{}
	ring  []ChangeSet // last N change sets for Last-Event-ID resume
}

type sub struct{ ch chan struct{} }

func NewPublisher(reg *Registry, projectors []Projector, validators []Validator) *Publisher {
	reg.Freeze()
	p := &Publisher{reg: reg, projectors: projectors, validators: validators, subs: map[*sub]struct{}{}}
	p.cur.Store(&Snapshot{repos: map[RepoID]*entry{}, globals: make([]any, len(reg.globals)), builtAt: time.Now()})
	return p
}

// Current is wait-free; handlers call it once per request and pass the *Snapshot down.
func (p *Publisher) Current() *Snapshot { return p.cur.Load() }

var ErrInvalidSnapshot = errors.New("readmodel: candidate snapshot failed validation")

// Apply builds, validates and atomically publishes the next snapshot. fn may mutate via Put/Remove.
func (p *Publisher) Apply(fn func(*Builder) error) (*Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := p.cur.Load()
	b := &Builder{reg: p.reg, prev: prev, repos: maps.Clone(prev.repos), globals: slices.Clone(prev.globals),
		touched: map[RepoID]struct{}{}, gtouched: map[string]struct{}{}}
	if err := fn(b); err != nil {
		return prev, err
	}
	for _, pr := range p.projectors {
		if err := pr(b); err != nil {
			return prev, fmt.Errorf("projector: %w", err)
		}
	}
	for id := range b.touched { // entries are unpublished here, so stamping them is safe
		if e := b.repos[id]; e != nil {
			e.rev = prev.version + 1
		}
	}
	next := &Snapshot{version: prev.version + 1, builtAt: time.Now(), repos: b.repos, names: b.names(), globals: b.globals}
	next.changed = ChangeSet{Version: next.version, Repos: slices.Sorted(maps.Keys(b.touched)), Globals: slices.Sorted(maps.Keys(b.gtouched)), Resync: prev.version == 0}
	for _, v := range p.validators {
		if err := v(next); err != nil {
			return prev, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
		}
	}
	p.cur.Store(next) // the only publication point
	p.notify(next.changed)
	return next, nil
}

func (p *Publisher) notify(cs ChangeSet) {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	p.ring = append(p.ring, cs)
	if len(p.ring) > 256 {
		p.ring = p.ring[len(p.ring)-256:]
	}
	for s := range p.subs {
		select { // level-triggered wake-up: a slow SSE client never blocks the publisher and never queues more than one wake
		case s.ch <- struct{}{}:
		default:
		}
	}
}

// Subscribe returns a coalescing wake-up channel. The subscriber reads Current() when woken.
func (p *Publisher) Subscribe() (wake <-chan struct{}, cancel func()) {
	s := &sub{ch: make(chan struct{}, 1)}
	p.subMu.Lock()
	p.subs[s] = struct{}{}
	p.subMu.Unlock()
	return s.ch, func() { p.subMu.Lock(); delete(p.subs, s); p.subMu.Unlock() }
}

// ChangesSince serves Last-Event-ID resume; ok=false means the client is too far behind: send a resync event.
func (p *Publisher) ChangesSince(version uint64) (changes []ChangeSet, ok bool) {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	if len(p.ring) == 0 {
		return nil, version == p.cur.Load().version
	}
	if version+1 < p.ring[0].Version {
		return nil, false
	}
	for _, cs := range p.ring {
		if cs.Version > version {
			changes = append(changes, cs)
		}
	}
	return changes, true
}
