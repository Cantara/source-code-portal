package snapshot

import (
	"iter"
	"maps"
)

// Facet is a feature's data on a repository entry, by name. A feature declares its facets as constants, for example
// `const commitsFacet snapshot.Facet[[]Commit] = "commits"`; the name also labels the facet's freshness in the metrics.
type Facet[T any] string

type facet struct {
	v     any
	fresh Freshness
}

// Get returns the facet of e with its freshness; the zero values when the feature has not contributed yet.
func (f Facet[T]) Get(e *RepoEntry) (T, Freshness) {
	got := e.facets[string(f)]
	v, _ := got.v.(T)
	return v, got.fresh
}

// Set records the facet on e. Call it on the copy Builder.UpdateRepo hands out: published entries are never changed.
func (f Facet[T]) Set(e *RepoEntry, v T, fresh Freshness) {
	m := maps.Clone(e.facets)
	if m == nil {
		m = map[string]facet{}
	}
	m[string(f)] = facet{v: v, fresh: fresh}
	e.facets = m
}

// Facets yields the name and freshness of every facet of e.
func (e *RepoEntry) Facets() iter.Seq2[string, Freshness] {
	return func(yield func(string, Freshness) bool) {
		for name, f := range e.facets {
			if !yield(name, f.fresh) {
				return
			}
		}
	}
}

// Index is what a feature derives from every repository of a snapshot, by name, declared as a constant like a Facet. Its Indexer,
// passed to NewPublisher, builds it at every publish; the public view builds it again over its own repositories.
type Index[T any] string

// Get returns the index built for s; the zero value before the first publish.
func (i Index[T]) Get(s *Snapshot) T {
	v, _ := s.indexes[string(i)].(T)
	return v
}

// Build returns the indexer that computes the index with fn.
func (i Index[T]) Build(fn func(*Snapshot) T) Indexer {
	return Indexer{name: string(i), build: func(s *Snapshot) any { return fn(s) }}
}

// Indexer builds one Index.
type Indexer struct {
	name  string
	build func(*Snapshot) any
}
