package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// command is one entry of the subcommand table. words is the command path ("config", "lint").
type command struct {
	words   []string
	summary string
	run     func(ctx context.Context, env Env, args []string) int
}

func (c command) name() string { return strings.Join(c.words, " ") }

// commands returns the table. A function, not a package variable (G5); run order is the order shown by `scp help`.
func commands() []command {
	return []command{
		{[]string{"serve"}, "run the portal (public and admin listeners, sync engine, store)", cmdServe},
		{[]string{"config", "lint"}, "validate a configuration file offline; exit 2 on violations", cmdConfigLint},
		{[]string{"config", "schema"}, "print the JSON Schema of the configuration (--descriptor: of the repository descriptor)", cmdConfigSchema},
		{[]string{"config", "reference"}, "print the Markdown reference of every configuration key", cmdConfigReference},
		{[]string{"config", "env"}, "print the SCP_* environment variable of every configuration key", cmdConfigEnv},
		{[]string{"config", "show"}, "print the effective configuration (secrets redacted; --layers: where each key came from)", cmdConfigShow},
		{[]string{"descriptor", "lint"}, "validate scp.yaml repository descriptors", cmdDescriptorLint},
		{[]string{"doctor"}, "check credentials, permissions, webhook, rate state and store", cmdDoctor},
		{[]string{"github", "app", "manifest"}, "register a GitHub App with the minimal permission set (manifest flow)", cmdGitHubAppManifest},
		{[]string{"store", "check"}, "check the integrity of the SQLite file (offline)", cmdStoreCheck},
		{[]string{"store", "restore"}, "replace the SQLite file with a verified backup (offline)", cmdStoreRestore},
		{[]string{"healthcheck"}, "probe a loopback URL (container HEALTHCHECK without curl); exit 0 on 2xx", cmdHealthcheck},
		{[]string{"version"}, "print version and build information", cmdVersion},
	}
}

// run dispatches env.Args and returns the process exit code. It never calls os.Exit.
func run(ctx context.Context, env Env) int {
	args := env.Args
	if len(args) == 0 {
		usage(env.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		if len(args) > 1 {
			if c, rest, ok := lookup(args[1:]); ok && len(rest) == 0 {
				_, _ = fmt.Fprintf(env.Stdout, "scp %s: %s\n\nRun `scp %s -h` for its flags.\n", c.name(), c.summary, c.name())
				return exitOK
			}
		}
		usage(env.Stdout)
		return exitOK
	case "-v", "--version":
		args = []string{"version"}
	}
	c, rest, ok := lookup(args)
	if !ok {
		_, _ = fmt.Fprintf(env.Stderr, "scp: unknown command %q\n\n", strings.Join(args[:min(len(args), 3)], " "))
		usage(env.Stderr)
		return exitUsage
	}
	return c.run(ctx, env, rest)
}

// lookup finds the longest command whose words are a prefix of args and returns the remaining arguments.
func lookup(args []string) (command, []string, bool) {
	best, n := command{}, 0
	for _, c := range commands() {
		if len(c.words) > len(args) || len(c.words) <= n {
			continue
		}
		match := true
		for i, w := range c.words {
			if args[i] != w {
				match = false
				break
			}
		}
		if match {
			best, n = c, len(c.words)
		}
	}
	return best, args[n:], n > 0
}

func usage(w io.Writer) {
	cs := commands()
	_, _ = fmt.Fprintln(w, "usage: scp <command> [flags]\n\ncommands:")
	for _, c := range cs {
		_, _ = fmt.Fprintf(w, "  %-22s %s\n", c.name(), c.summary)
	}
	_, _ = fmt.Fprintln(w, "\nexit codes: 0 ok, 1 failure, 2 input refused (usage or invalid file), 3 completed with caveats (warnings under --strict)")
}

// newFlagSet builds a FlagSet that prints to env.Stderr and returns errors instead of exiting.
func newFlagSet(env Env, name string) *flag.FlagSet {
	fs := flag.NewFlagSet("scp "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

// parseFlags returns (code, true) when the caller must return code: -h exits 0, a bad flag exits 2.
func parseFlags(fs *flag.FlagSet, args []string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK, true
		}
		return exitUsage, true
	}
	return 0, false
}

// configPathFlag registers --config with the SCP_CONFIG default.
func configPathFlag(fs *flag.FlagSet, env Env) *string {
	return fs.String("config", env.Getenv("SCP_CONFIG"), "configuration file (default $SCP_CONFIG; empty = embedded defaults only)")
}
