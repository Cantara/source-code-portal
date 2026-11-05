package main

import (
	"os"
	"time"
)

// slack widens the time budgets on a CI runner (two shared cores, CI set): the design's budgets are a developer machine's, and
// three times them on a runner still tells a regression from the runner's noise.
func slack() time.Duration {
	if os.Getenv("CI") != "" {
		return 3
	}
	return 1
}
