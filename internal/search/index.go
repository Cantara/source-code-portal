// Package search is the portal's search (design/search.md): an inverted index in memory of what the portal holds, one segment per
// repository built from its facets through Options.Read and reused while the repository's content digest is unchanged. A view's
// index is its own repositories' segments and its groups, so a public query never reads a private repository.
package search

import (
	"encoding/binary"
	"hash/fnv"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Options are what the index reads of the other features, as functions (cmd/scp/search.go).
type Options struct {
	// Read returns what the index holds of the repository e: deterministic for equal content (the segment is reused while
	// Snapshot.Digest and the listing's fields are unchanged).
	Read func(e *snapshot.RepoEntry) Repository
	// Work names the kind of work (hand, ai, automation) of the commit document item of e in the view s, for the kind filter; nil
	// leaves the filter matching nothing.
	Work func(s *snapshot.Snapshot, e *snapshot.RepoEntry, item int) string
}

// Index is the search index of one view of a snapshot.
type Index struct {
	s      *snapshot.Snapshot
	o      Options
	segs   []*segment // by repository key; the groups' segment last
	groups *segment
	sums   [kinds][fields]uint64
	count  [kinds]uint64
	w      weighting
	took   time.Duration
}

// Stats is what an index holds, for the operator's status.
type Stats struct {
	Segments int     `json:"segments"`
	Terms    int     `json:"terms"`
	Bytes    int     `json:"bytes"`
	BuildMs  float64 `json:"buildMs"` // the last build's time, its reused segments included
}

const index snapshot.Index[*Index] = "search"

// Of returns the search index of the view s; nil before the first publish.
func Of(s *snapshot.Snapshot) *Index { return index.Get(s) }

// Stats returns what ix holds.
func (ix *Index) Stats() Stats {
	st := Stats{BuildMs: float64(ix.took.Microseconds()) / 1000}
	for _, sg := range ix.segs {
		st.Segments++
		st.Terms += sg.numTerms()
		st.Bytes += sg.size()
	}
	return st
}

// cache holds the last builds' segments by repository, shared by the views of a publish.
type cache struct {
	mu   sync.Mutex
	gen  uint64
	segs map[snapshot.RepoKey]*segment
}

// Indexer is the search index's indexer, for snapshot.NewPublisher.
func Indexer(o Options) snapshot.Indexer {
	c := &cache{segs: map[snapshot.RepoKey]*segment{}}
	return index.Build(func(s *snapshot.Snapshot) *Index { return c.build(s, o) })
}

// build assembles the index of the view s: a segment per repository, reused when its key is unchanged, built otherwise (in parallel),
// and the view's groups. A segment neither this build nor the one before used is dropped: the full view and its public view build
// one after the other, so each keeps the other's.
func (c *cache) build(s *snapshot.Snapshot, o Options) *Index {
	start := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	ix := &Index{s: s, o: o, w: weights()}
	keys := make([]snapshot.RepoKey, 0, len(s.Repos))
	for k := range s.Repos {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	ix.segs = make([]*segment, len(keys))
	var todo []int
	for i, k := range keys {
		key := keyOf(s, k)
		if sg := c.segs[k]; sg != nil && sg.key == key {
			sg.used = c.gen
			ix.segs[i] = sg
			continue
		}
		todo = append(todo, i)
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for range min(runtime.GOMAXPROCS(0), len(todo)) {
		wg.Go(func() {
			for i := range next {
				k := keys[i]
				sg := build(k, o.Read(s.Repos[k]))
				sg.key = keyOf(s, k)
				ix.segs[i] = sg
			}
		})
	}
	for _, i := range todo {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, i := range todo {
		sg := ix.segs[i]
		sg.used = c.gen
		c.segs[keys[i]] = sg
	}
	for k, sg := range c.segs {
		if sg.used+1 < c.gen {
			delete(c.segs, k)
		}
	}
	defer func() { ix.took = time.Since(start) }()
	ix.groups = groupSegment(s)
	ix.segs = append(ix.segs, ix.groups)
	for _, sg := range ix.segs {
		for k := range kinds {
			ix.count[k] += uint64(sg.count[k])
			for f := range fields {
				ix.sums[k][f] += sg.sums[k][f]
			}
		}
	}
	return ix
}

// keyOf is what the segment of k is built from: its content digest and the listing's fields Read reads.
func keyOf(s *snapshot.Snapshot, k snapshot.RepoKey) uint64 {
	r := s.Repos[k].Repo
	h := fnv.New64a()
	for _, f := range append([]string{r.Owner, r.Name, r.Description, r.License, r.Visibility, r.HTMLURL, r.DefaultBranch}, r.Topics...) {
		_, _ = h.Write([]byte(f))
		_, _ = h.Write([]byte{0})
	}
	if r.Archived {
		_, _ = h.Write([]byte{1})
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], s.Digest(k))
	_, _ = h.Write(b[:])
	return h.Sum64()
}

// groupSegment indexes the view's groups: their names, ids and descriptions; a group's members are the view's.
func groupSegment(s *snapshot.Snapshot) *segment {
	return build("", Repository{Documents: groupDocuments(s)})
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
