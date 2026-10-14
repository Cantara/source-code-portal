//go:build race

package render

import "testing"

// timeScale multiplies every wall-clock bound of the adversarial tests: the race detector slows goroutine-free CPU code by
// 5 to 10 times, and the first version of these tests failed under `go test -race` (verification V-13).
const timeScale = 10

// heavy skips a timing test whose bound is meaningless under the race detector; the CI job "adversarial" runs these
// without -race (design section 12.5).
func heavy(t *testing.T) { t.Helper(); t.Skip("adversarial timing test: runs in the non-race job") }
