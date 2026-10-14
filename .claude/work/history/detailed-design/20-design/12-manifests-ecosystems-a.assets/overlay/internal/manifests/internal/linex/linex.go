// Package linex (rank 1, stdlib only; new package of unit 12): byte offset to 1-based line lookup used by parsers that
// scan text and report Line positions. Built once per file, queried in O(log n).
package linex

// Index holds the byte offset of the start of every line.
type Index []int

// New indexes data. Line 1 starts at offset 0.
func New(data []byte) Index {
	idx := Index{0}
	for i, c := range data {
		if c == '\n' {
			idx = append(idx, i+1)
		}
	}
	return idx
}

// Line returns the 1-based line containing the byte offset off (clamped to the last line).
func (l Index) Line(off int) int {
	lo, hi := 0, len(l)
	for lo < hi {
		m := (lo + hi) / 2
		if l[m] <= off {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

// Count returns the number of lines.
func (l Index) Count() int { return len(l) }
