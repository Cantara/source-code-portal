// Command fakegithub is the development and end-to-end fake GitHub server (architecture 4.5, design 05 section 9).
//
// It is a stub in the seed: the real server is built on internal/github/githubtest when unit 05 lands (task B-7). It may import only
// internal/github/githubtest and internal/scm (closed-world row cmd/fakegithub).
package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "fakegithub: not implemented yet (design 05, task B-7)")
	os.Exit(1)
}
