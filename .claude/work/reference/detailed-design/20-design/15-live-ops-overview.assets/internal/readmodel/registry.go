package readmodel

import "fmt"

// EntryID is the portal slug as a string; named EntryID so it never collides with scm.RepoID (int64).
type EntryID string

type slotConfig struct{ existence bool }

// SlotOption configures Register.
type SlotOption func(*slotConfig)

// Existence marks THE slot whose presence means "this repository exists" (catalog.repo). Exactly one per registry.
func Existence() SlotOption { return func(c *slotConfig) { c.existence = true } }

// Registry is filled once in cmd/scp, then frozen. Not safe for concurrent use before Freeze; read-only afterwards.
type Registry struct {
	slots        []string
	globals      []string
	existence    int
	hasExistence bool
	frozen       bool
}

// Slot is a typed handle to one per-repository value kind. T must be treated as immutable once Put.
type Slot[T any] struct {
	reg *Registry
	id  int
}

func (s Slot[T]) Name() string { return s.reg.slots[s.id] }

// Global is a typed handle to one snapshot-wide value.
type Global[T any] struct {
	reg *Registry
	id  int
}

func (g Global[T]) Name() string { return g.reg.globals[g.id] }

func has(names []string, n string) bool {
	for _, x := range names {
		if x == n {
			return true
		}
	}
	return false
}

// Register adds a per-repository slot. It panics (wiring error, found at the first start) on: empty or duplicate name,
// registration after Freeze, a second Existence slot.
func Register[T any](r *Registry, name string, opts ...SlotOption) Slot[T] {
	if r.frozen {
		panic("readmodel: Register after Freeze: " + name)
	}
	if name == "" || has(r.slots, name) {
		panic(fmt.Sprintf("readmodel: empty or duplicate slot name %q", name))
	}
	var c slotConfig
	for _, o := range opts {
		o(&c)
	}
	r.slots = append(r.slots, name)
	id := len(r.slots) - 1
	if c.existence {
		if r.hasExistence {
			panic("readmodel: second Existence slot: " + name)
		}
		r.existence, r.hasExistence = id, true
	}
	return Slot[T]{reg: r, id: id}
}

// RegisterGlobal adds a snapshot-wide value.
func RegisterGlobal[T any](r *Registry, name string) Global[T] {
	if r.frozen {
		panic("readmodel: RegisterGlobal after Freeze: " + name)
	}
	if name == "" || has(r.globals, name) {
		panic(fmt.Sprintf("readmodel: empty or duplicate global name %q", name))
	}
	r.globals = append(r.globals, name)
	return Global[T]{reg: r, id: len(r.globals) - 1}
}

// Freeze ends registration. It panics when no Existence slot was registered. Idempotent.
func (r *Registry) Freeze() {
	if r.frozen {
		return
	}
	if !r.hasExistence {
		panic("readmodel: no Existence slot registered")
	}
	r.frozen = true
}

// SlotNames and GlobalNames list registered names in registration order (for /status and tests).
func (r *Registry) SlotNames() []string   { return append([]string(nil), r.slots...) }
func (r *Registry) GlobalNames() []string { return append([]string(nil), r.globals...) }
