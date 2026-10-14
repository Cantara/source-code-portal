package main

import (
	"context"
	"encoding/json"
	"fmt"
)

func cmdVersion(_ context.Context, env Env, args []string) int {
	fs := newFlagSet(env, "version")
	asJSON := fs.Bool("json", false, "print JSON")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp version: takes no arguments")
		return exitUsage
	}
	bi := readBuildInfo()
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(bi); err != nil {
			return exitFail
		}
		return exitOK
	}
	mod := ""
	if bi.Modified {
		mod = " (modified)"
	}
	_, _ = fmt.Fprintf(env.Stdout, "scp %s build %s%s %s %s/%s spa=%s\n", bi.Version, bi.Build, mod, bi.Go, bi.OS, bi.Arch, bi.SPA)
	return exitOK
}
