// Command scp is the composition root and the command line of Source Code Portal (architecture 4.5, design 16).
//
// This is the seed skeleton: only `version` and `help` exist. The commands of design 16 section 3.3 (serve, config, descriptor,
// doctor, github app manifest, store, healthcheck) arrive with tasks B-2 and B-4 to B-7; copy their compiled starting point from
// .claude/work/plans/20-design/16-cmd-build-release.assets/cmd/scp (see .claude/work/plans/50-repo-seed/design-map.md) when each prerequisite package exists.
//
// main contains no logic: it builds the signal context and returns run's exit code. cmd/scp is the only package that may read the
// environment and arguments, print, install signal handlers and exit (rule G9); .golangci.yml carries the matching exemptions.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
