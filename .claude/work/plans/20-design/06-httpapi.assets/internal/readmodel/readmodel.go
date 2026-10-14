// Package readmodel: STUB of the read side only (10-architecture.md 8.4).
package readmodel

type EntryID string
type Slot[T any] struct{ name string }
type Snapshot struct{ data map[string]map[EntryID]any }

func NewSnapshotForTest(data map[string]map[EntryID]any) *Snapshot { return &Snapshot{data: data} }
func NewSlotForTest[T any](name string) Slot[T]                    { return Slot[T]{name: name} }
func (s *Snapshot) Version() uint64                                { return 1 }
func Get[T any](s *Snapshot, id EntryID, slot Slot[T]) (T, bool) {
	var zero T
	m, ok := s.data[slot.name]
	if !ok {
		return zero, false
	}
	v, ok := m[id]
	if !ok {
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}
