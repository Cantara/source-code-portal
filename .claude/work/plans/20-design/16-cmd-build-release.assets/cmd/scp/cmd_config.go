package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/docs"
	"github.com/cantara/source-code-portal/internal/platform/config"
)

type lintRow struct {
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Path     string `json:"path,omitempty"`
	Severity string `json:"severity"` // error | warning
	Message  string `json:"message"`
	Kind     string `json:"-"` // kindIO for a file that could not be read: exit 1, not 2
}

// lintExit maps findings to the exit code: violations -> 2 (the input is refused, as serve would refuse it); an unreadable file ->
// 1; warnings only -> 3 under --strict, else 0.
func lintExit(rows []lintRow, strict bool) int {
	invalid, unreadable, warn := false, false, false
	for _, r := range rows {
		switch {
		case r.Severity == "error" && r.Kind == kindIO:
			unreadable = true
		case r.Severity == "error":
			invalid = true
		default:
			warn = true
		}
	}
	switch {
	case invalid:
		return exitInvalid
	case unreadable:
		return exitFail
	case warn && strict:
		return exitWarn
	}
	return exitOK
}

const kindIO = "io"

func cmdConfigLint(_ context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "config lint")
	path := configPathFlag(fl, env)
	strict := fl.Bool("strict", false, "exit 3 when there are only warnings")
	strictEnv := fl.Bool("strict-env", false, "also read the SCP_* environment: a variable that matches no key is an error, not a warning")
	asJSON := fl.Bool("json", false, "print findings as a JSON array")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	files := fl.Args()
	if len(files) == 0 && *path != "" {
		files = []string{*path}
	}
	if len(files) == 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp config lint: no file (pass FILE... or --config)")
		return exitUsage
	}
	rows := []lintRow{}
	for _, f := range files {
		// Lint reads the file and the embedded defaults only, without secret files; the environment layer joins only under
		// --strict-env. "Does this file mean what I think" must not depend on the machine it is checked on.
		o := loadOptions{file: f, skipSecrets: true}
		if *strictEnv {
			o.environ = env.Environ
		}
		l, err := readConfig(o)
		if err != nil {
			rows = append(rows, lintRow{File: f, Severity: "error", Message: err.Error(), Kind: kindIO})
			continue
		}
		for _, v := range l.violations {
			rows = append(rows, lintRow{File: f, Line: v.Line, Path: v.Path, Severity: "error", Message: v.Message})
		}
		for _, w := range l.warnings {
			sev := "warning"
			if *strictEnv && strings.HasPrefix(w, "environment variable ") {
				sev = "error"
			}
			rows = append(rows, lintRow{File: f, Severity: sev, Message: w})
		}
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
	} else {
		for _, r := range rows {
			loc := r.File
			if r.Line > 0 {
				loc = fmt.Sprintf("%s:%d", r.File, r.Line)
			}
			if r.Path != "" {
				loc += ": " + r.Path
			}
			_, _ = fmt.Fprintf(env.Stdout, "%s: %s: %s\n", loc, r.Severity, r.Message)
		}
		if len(rows) == 0 {
			_, _ = fmt.Fprintf(env.Stdout, "%d file(s) ok\n", len(files))
		}
	}
	return lintExit(rows, *strict)
}

func cmdConfigSchema(_ context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "config schema")
	descriptor := fl.Bool("descriptor", false, "print the repository descriptor schema (scp.yaml v1) instead")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	if fl.NArg() != 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp config schema: takes no arguments")
		return exitUsage
	}
	if *descriptor {
		_, _ = env.Stdout.Write(docs.DescriptorSchema())
		return exitOK
	}
	var cfg Config
	b, err := config.Schema(&cfg, defaultYAML)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp config schema: %v\n", err)
		return exitFail
	}
	_, _ = env.Stdout.Write(b)
	_, _ = io.WriteString(env.Stdout, "\n")
	return exitOK
}

// cmdConfigReference prints the Markdown table of every key (docs/reference/config.md is generated from it; make gen-check).
func cmdConfigReference(_ context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "config reference")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	var cfg Config
	s, err := config.Reference(&cfg, defaultYAML)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp config reference: %v\n", err)
		return exitFail
	}
	_, _ = io.WriteString(env.Stdout, s)
	return exitOK
}

// cmdConfigEnv prints the SCP_* variable of every key, one per line, sorted (docs/reference/env.txt).
func cmdConfigEnv(_ context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "config env")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	var cfg Config
	root, err := config.Describe(&cfg)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp config env: %v\n", err)
		return exitFail
	}
	names := config.EnvNames(root)
	slices.Sort(names)
	_, _ = io.WriteString(env.Stdout, strings.Join(names, "\n")+"\n")
	return exitOK
}

// cmdConfigShow prints the effective configuration (secrets are redacted by secret.Secret itself). --layers lists, for every key
// that is not the embedded default, the layer that set it and where it came from; the default of everything else is `config reference`.
func cmdConfigShow(_ context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "config show")
	path := configPathFlag(fl, env)
	layers := fl.Bool("layers", false, "list the layer (file, env, flag) of every key that differs from the default instead of the document")
	var sets setFlags
	fl.Var(&sets, "set", "override one key: path=value (repeatable; secrets are refused)")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	var cfg Config
	res, err := config.Load(&cfg, config.Options{Defaults: defaultYAML, File: *path, ReadFile: os.ReadFile, Environ: env.Environ, Set: sets, SkipSecrets: true})
	if ce, ok := errors.AsType[*config.Error](err); ok {
		_, _ = fmt.Fprintf(env.Stderr, "scp config show: %d configuration problem(s); run `scp config lint`\n", len(ce.Violations))
		return exitInvalid
	} else if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp config show: %v\n", err)
		return exitFail
	}
	if *layers {
		for _, a := range res.Applied {
			if a.Layer == config.LayerDefault {
				continue
			}
			_, _ = fmt.Fprintf(env.Stdout, "%-48s %-5s %s\n", a.Path, a.Layer, a.From)
		}
		return exitOK
	}
	enc := yaml.NewEncoder(env.Stdout)
	enc.SetIndent(2)
	if err := enc.Encode(&cfg); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp config show: %v\n", err)
		return exitFail
	}
	if err := enc.Close(); err != nil {
		return exitFail
	}
	return exitOK
}
