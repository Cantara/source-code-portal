//go:build race

package docs

// slowdown scales time bounds in tests: the race detector makes rendering about ten times slower.
const slowdown = 10
