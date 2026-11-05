//go:build race

package search

// slowdown scales time bounds in tests: the race detector makes indexing about ten times slower.
const slowdown = 10
