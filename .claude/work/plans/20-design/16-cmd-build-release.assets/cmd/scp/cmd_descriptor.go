package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cantara/source-code-portal/internal/docs"
)

// cmdDescriptorLint validates scp.yaml files; any problem refuses the file, exit 2 (architecture 15: "scp descriptor lint fails on any problem").
func cmdDescriptorLint(_ context.Context, env Env, args []string) int {
	fs := newFlagSet(env, "descriptor lint")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp descriptor lint: no file")
		return exitUsage
	}
	bad, unreadable := false, false
	for _, f := range fs.Args() {
		data, err := os.ReadFile(f)
		if err != nil {
			_, _ = fmt.Fprintf(env.Stdout, "%s: error: %v\n", f, err)
			unreadable = true
			continue
		}
		ps, err := docs.LintDescriptor(data)
		if err != nil {
			_, _ = fmt.Fprintf(env.Stdout, "%s: error: %v\n", f, err)
			unreadable = true
			continue
		}
		for _, p := range ps {
			_, _ = fmt.Fprintf(env.Stdout, "%s: %s: %s: %s\n", f, p.Pointer, p.Severity(), p.Message)
			bad = true
		}
	}
	switch {
	case bad:
		return exitInvalid
	case unreadable:
		return exitFail
	}
	_, _ = fmt.Fprintf(env.Stdout, "%d file(s) ok\n", fs.NArg())
	return exitOK
}
