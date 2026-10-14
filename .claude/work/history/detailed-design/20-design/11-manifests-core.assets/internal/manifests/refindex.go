package manifests

import (
	"sync/atomic"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// refIndex resolves a (source, owner, name) job key to the portal slug and the repository value. It is rebuilt from the snapshot
// when the repository list changed (the slice header of Snapshot.Repos is shared while membership is unchanged) or on a miss.
type refIndex struct{ cur atomic.Pointer[refIndexState] }

type refIndexState struct {
	first *readmodel.EntryID
	n     int
	byKey map[store.RepoKey]scm.Slug
}

func (x *refIndex) lookup(snap *readmodel.Snapshot, slot readmodel.Slot[freshness.Value[scm.Repository]], k store.RepoKey) (scm.Slug, bool) {
	ids := snap.Repos()
	st := x.cur.Load()
	same := st != nil && st.n == len(ids) && (len(ids) == 0 || st.first == &ids[0])
	if !same {
		st = x.rebuild(snap, slot, ids)
	}
	if s, ok := st.byKey[k]; ok {
		return s, true
	}
	if same { // miss on a current index: one forced rebuild per call is enough to catch a rename
		st = x.rebuild(snap, slot, ids)
		s, ok := st.byKey[k]
		return s, ok
	}
	return "", false
}

func (x *refIndex) rebuild(snap *readmodel.Snapshot, slot readmodel.Slot[freshness.Value[scm.Repository]], ids []readmodel.EntryID) *refIndexState {
	st := &refIndexState{n: len(ids), byKey: make(map[store.RepoKey]scm.Slug, len(ids))}
	if len(ids) > 0 {
		st.first = &ids[0]
	}
	for _, id := range ids {
		if v, ok := readmodel.Get(snap, id, slot); ok {
			st.byKey[store.KeyOf(v.V.Ref)] = v.V.Slug
		}
	}
	x.cur.Store(st)
	return st
}
