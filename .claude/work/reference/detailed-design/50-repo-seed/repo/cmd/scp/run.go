package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Exit codes (design 16 section 4.1): one table for every command.
const (
	exitOK    = 0 // success
	exitFail  = 1 // the thing asked for failed: an I/O or runtime error
	exitUsage = 2 // the input was refused: a bad command line, or a configuration with violations
	exitWarn  = 3 // completed with caveats (warnings only under --strict)
)

// run dispatches args and returns the process exit code. It never calls os.Exit.
func run(_ context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(stdout)
		return exitOK
	case "-v", "--version", "version":
		return cmdVersion(args[1:], stdout, stderr)
	}
	_, _ = fmt.Fprintf(stderr, "scp: unknown command %q\n\n", args[0])
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, "usage: scp <command>\n\ncommands:\n  version   print version and build information (--json)\n  help      this text\n")
}

func cmdVersion(args []string, stdout, stderr io.Writer) int {
	asJSON := false
	for _, a := range args {
		if a != "--json" && a != "-json" {
			_, _ = fmt.Fprintf(stderr, "scp version: unexpected argument %q\n", a)
			return exitUsage
		}
		asJSON = true
	}
	bi := readBuildInfo()
	if asJSON {
		enc := json.NewEncoder(stdout)
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
	_, _ = fmt.Fprintf(stdout, "scp %s build %s%s %s %s/%s spa=%s\n", bi.Version, bi.Build, mod, bi.Go, bi.OS, bi.Arch, bi.SPA)
	return exitOK
}
