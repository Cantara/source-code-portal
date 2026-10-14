package readmodel

import "time"

// entry is immutable once published; the Builder replaces whole entries (structural sharing per repository).
type entry struct {
	slots []any
	rev   uint64
}

// Snapshot is immutable. Handlers call Publisher.Current once per request and pass the *Snapshot down.
type Snapshot struct {
	reg     *Registry
	version uint64
	builtAt time.Time
	epoch   string
	repos   map[EntryID]*entry
	names   []EntryID
	globals []any
}

func (s *Snapshot) Version() uint64    { return s.version }
func (s *Snapshot) BuiltAt() time.Time { return s.builtAt }

// Epoch is the per-process random token chosen by cmd/scp before the publisher starts (07 T-16, Q-13). Versions restart at 1
// after every restart, so every weak tag and the SSE hello carry the epoch to stay unique across restarts (CD-IF-03).
func (s *Snapshot) Epoch() string { return s.epoch }

// Repos returns the sorted entry ids. The slice is shared between snapshots while membership is unchanged: read-only.
func (s *Snapshot) Repos() []EntryID { return s.names }

// Has reports whether the entry exists (a value in the existence slot).
func (s *Snapshot) Has(id EntryID) bool { _, ok := s.repos[id]; return ok }

// Rev is the snapshot version at which the entry last changed (0 when absent): the per-repository ETag.
func (s *Snapshot) Rev(id EntryID) uint64 {
	if e := s.repos[id]; e != nil {
		return e.rev
	}
	return 0
}

func checkReg(r *Registry, got *Registry) {
	if r != got {
		panic("readmodel: handle from a different registry")
	}
}

// Get returns the slot value of an entry. ok is false when the entry or the value is absent.
func Get[T any](s *Snapshot, id EntryID, slot Slot[T]) (v T, ok bool) {
	checkReg(s.reg, slot.reg)
	e := s.repos[id]
	if e == nil {
		return v, false
	}
	x := e.slots[slot.id]
	if x == nil {
		return v, false
	}
	v, ok = x.(T)
	return v, ok
}

// GetGlobal returns a snapshot-wide value. ok is false until a projector or producer has put one.
func GetGlobal[T any](s *Snapshot, g Global[T]) (v T, ok bool) {
	checkReg(s.reg, g.reg)
	x := s.globals[g.id]
	if x == nil {
		return v, false
	}
	v, ok = x.(T)
	return v, ok
}
