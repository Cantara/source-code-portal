//go:build race

package main

// slowdown scales time bounds in tests: the race detector makes serving about ten times slower.
const slowdown = 10
