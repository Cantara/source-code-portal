//go:build race

package search

// race is whether the race detector is on: the checks that measure time and size skip under it and run once without it (make test).
const race = true
