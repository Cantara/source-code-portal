package readmodel

import (
	"maps"
	"slices"
)

// Builder is a single-use copy-on-write view of the previous snapshot. One goroutine only (the Apply callback).
type Builder struct {
	reg      *Registry
	prev     *Snapshot
	repos    map[EntryID]*entry
	globals  []any
	fresh    map[EntryID]struct{} // entries created or cloned by this builder: private, mutable until publish
	touched  map[EntryID]struct{}
	gtouched map[int]struct{}
	members  bool
	names    []EntryID
}

func newBuilder(prev *Snapshot) *Builder {
	return &Builder{
		reg: prev.reg, prev: prev,
		repos:    maps.Clone(prev.repos),
		globals:  slices.Clone(prev.globals),
		fresh:    map[EntryID]struct{}{},
		touched:  map[EntryID]struct{}{},
		gtouched: map[int]struct{}{},
	}
}

// Put stores v in the slot of entry id and TAKES OWNERSHIP of v: the caller must not mutate it afterwards.
// It returns ErrUnknownRepo (an *UnknownRepoError, which is job.Gone) when id has no value in the existence slot yet and
// slot is not the existence slot, and ErrNilValue for a nil interface value.
func Put[T any](b *Builder, id EntryID, slot Slot[T], v T) error {
	checkReg(b.reg, slot.reg)
	if any(v) == nil {
		return ErrNilValue
	}
	e := b.repos[id]
	switch {
	case e == nil:
		if slot.id != b.reg.existence {
			return &UnknownRepoError{ID: id, Slot: slot.Name()}
		}
		e = &entry{slots: make([]any, len(b.reg.slots))}
		b.repos[id] = e
		b.fresh[id] = struct{}{}
		b.members = true
		b.names = nil
	default:
		if _, ok := b.fresh[id]; !ok {
			e = &entry{slots: slices.Clone(e.slots)}
			b.repos[id] = e
			b.fresh[id] = struct{}{}
		}
	}
	e.slots[slot.id] = v
	b.touched[id] = struct{}{}
	return nil
}

// PutGlobal stores a snapshot-wide value (takes ownership).
func PutGlobal[T any](b *Builder, g Global[T], v T) error {
	checkReg(b.reg, g.reg)
	if any(v) == nil {
		return ErrNilValue
	}
	b.globals[g.id] = v
	b.gtouched[g.id] = struct{}{}
	return nil
}

// Remove deletes the whole entry (all slots). Removing an absent id is a no-op.
func (b *Builder) Remove(id EntryID) {
	if _, ok := b.repos[id]; !ok {
		return
	}
	delete(b.repos, id)
	delete(b.fresh, id)
	b.members = true
	b.names = nil
	b.touched[id] = struct{}{}
}

func (b *Builder) sortedNames() []EntryID {
	if !b.members {
		return b.prev.names
	}
	if b.names == nil {
		b.names = slices.Sorted(maps.Keys(b.repos))
	}
	return b.names
}

// View lets projectors read the candidate (including this builder's changes). The returned snapshot is not publishable.
func (b *Builder) View() *Snapshot {
	return &Snapshot{reg: b.reg, version: b.prev.version + 1, repos: b.repos, globals: b.globals, names: b.sortedNames()}
}
