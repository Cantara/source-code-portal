package commits

import (
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Order. Every feed is ordered newest first by (CommittedAt desc, Slug asc, SHA desc). Within one repository this is
// exactly scm.CompareCommits (newest first, equal timestamps by SHA descending: the one commit order of the portal,
// 01-kernel SC-08). The Slug is part of the cross-repository key because the same SHA may exist in two repositories
// (forks, mirrors; GR-07), so (CommittedAt, SHA) alone is not total.

// compareWithin orders two commits of the same repository. Every commit that reaches a ring has a date (normalise drops
// the others), so this is scm.CompareCommits.
func compareWithin(a, b scm.Commit) int { return scm.CompareCommits(a, b) }

// Compare orders commits across repositories; a negative result means a is listed before b.
func Compare(aSlug scm.Slug, a scm.Commit, bSlug scm.Slug, b scm.Commit) int {
	if c := b.CommittedAt.Compare(a.CommittedAt); c != 0 {
		return c
	}
	if c := strings.Compare(string(aSlug), string(bSlug)); c != 0 {
		return c
	}
	return strings.Compare(b.SHA, a.SHA) // descending, as scm.CompareCommits
}

// Position is a keyset cursor: the key of the last entry already delivered.
type Position struct {
	At   time.Time
	Slug scm.Slug
	SHA  string
}

// Source is one repository's ring, ordered by compareWithin (as produced by normalise). It is read-only.
type Source struct {
	Slug    scm.Slug
	Commits []scm.Commit
}

// Entry is one merged element; C points into the immutable ring of its source.
type Entry struct {
	Slug scm.Slug
	C    *scm.Commit
}

type cursor struct {
	src *Source
	i   int
}

func (c cursor) head() *scm.Commit { return &c.src.Commits[c.i] }

func lessCur(a, b cursor) bool {
	return Compare(a.src.Slug, *a.head(), b.src.Slug, *b.head()) < 0
}

// firstAfter returns the index of the first commit of s that is listed strictly after pos (binary search).
func firstAfter(s *Source, pos Position) int {
	lo, hi := 0, len(s.Commits)
	pc := scm.Commit{CommittedAt: pos.At, SHA: pos.SHA}
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if Compare(s.Slug, s.Commits[m], pos.Slug, pc) <= 0 {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

// Merge returns up to limit entries of the union of srcs, newest first, starting strictly after pos (nil: from the
// top), plus whether more entries exist after the last returned one. Cost: O(k log n) to position k sources by binary
// search over rings of n entries, O(k) to build the heap, O(limit log k) to emit; nothing proportional to the total
// number of retained commits. dst is reused when it has capacity.
func Merge(srcs []Source, pos *Position, limit int, dst []Entry) (out []Entry, more bool) {
	out = dst[:0]
	if limit <= 0 {
		return out, false
	}
	h := make([]cursor, 0, len(srcs))
	for i := range srcs {
		s := &srcs[i]
		start := 0
		if pos != nil {
			start = firstAfter(s, *pos)
		}
		if start < len(s.Commits) {
			h = append(h, cursor{s, start})
		}
	}
	for i := len(h)/2 - 1; i >= 0; i-- {
		siftDown(h, i)
	}
	for len(out) < limit && len(h) > 0 {
		top := h[0]
		out = append(out, Entry{Slug: top.src.Slug, C: top.head()})
		if top.i+1 < len(top.src.Commits) {
			h[0].i++
		} else {
			h[0] = h[len(h)-1]
			h = h[:len(h)-1]
		}
		if len(h) > 0 {
			siftDown(h, 0)
		}
	}
	return out, len(h) > 0
}

func siftDown(h []cursor, i int) {
	n := len(h)
	for {
		l := 2*i + 1
		if l >= n {
			return
		}
		m := l
		if r := l + 1; r < n && lessCur(h[r], h[l]) {
			m = r
		}
		if !lessCur(h[m], h[i]) {
			return
		}
		h[i], h[m] = h[m], h[i]
		i = m
	}
}

// PositionOf is the cursor value that follows entry e.
func PositionOf(e Entry) Position { return Position{At: e.C.CommittedAt, Slug: e.Slug, SHA: e.C.SHA} }
