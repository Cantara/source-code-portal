package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/sqlite"
)

// storePathFlag registers --db with the SCP_STORE_PATH default; the product reads the same key from the config file (store.path).
// --file is an alias: 03 section 12 (the operator runbook) spells the verbs `scp store check --file F` (V16-07).
func storePathFlag(fs *flag.FlagSet, env Env) *string {
	p := new(string)
	def := env.Getenv("SCP_STORE_PATH")
	fs.StringVar(p, "db", def, "SQLite file (default $SCP_STORE_PATH)")
	fs.StringVar(p, "file", def, "alias of --db")
	return p
}

func cmdStoreCheck(ctx context.Context, env Env, args []string) int {
	fs := newFlagSet(env, "store check")
	db := storePathFlag(fs, env)
	quick := fs.Bool("quick", false, "PRAGMA quick_check instead of integrity_check")
	full := fs.Bool("full", false, "PRAGMA integrity_check (the default; accepted because 03 section 12 spells it)")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if *quick && *full {
		_, _ = fmt.Fprintln(env.Stderr, "scp store check: --quick and --full exclude each other")
		return exitUsage
	}
	if *db == "" {
		_, _ = fmt.Fprintln(env.Stderr, "scp store check: --db is required (or SCP_STORE_PATH)")
		return exitUsage
	}
	if _, err := os.Stat(*db); err != nil { // CheckFile reports a missing file as "unable to open database file (14)": say what is wrong
		_, _ = fmt.Fprintf(env.Stderr, "scp store check: %v\n", err)
		return exitFail
	}
	if err := sqlite.CheckFile(ctx, *db, !*quick); err != nil {
		if errors.Is(err, store.ErrCorrupt) {
			_, _ = fmt.Fprintf(env.Stdout, "CORRUPT %s: %v\n", *db, err)
		} else {
			_, _ = fmt.Fprintf(env.Stderr, "scp store check: %v\n", err)
		}
		return exitFail
	}
	_, _ = fmt.Fprintf(env.Stdout, "ok %s\n", *db)
	return exitOK
}

func cmdStoreRestore(ctx context.Context, env Env, args []string) int {
	fs := newFlagSet(env, "store restore")
	db := storePathFlag(fs, env)
	from := fs.String("from", "", "backup file to restore (required)")
	keep := fs.Int("keep", 3, "quarantined previous files to keep")
	yes := fs.Bool("yes", false, "confirm: the current file is moved aside and replaced")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if *db == "" || *from == "" || *keep < 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp store restore: --db (or SCP_STORE_PATH) and --from are required; --keep must be >= 0")
		return exitUsage
	}
	if !*yes {
		_, _ = fmt.Fprintln(env.Stderr, "scp store restore: refusing without --yes (stop the portal first; the current file is quarantined, not deleted)")
		return exitUsage
	}
	if _, err := os.Stat(*from); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp store restore: %v\n", err)
		return exitFail
	}
	if err := sqlite.Restore(ctx, *db, *from, *keep); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp store restore: %v\n", err)
		return exitFail
	}
	_, _ = fmt.Fprintf(env.Stdout, "restored %s from %s\n", *db, *from)
	return exitOK
}
