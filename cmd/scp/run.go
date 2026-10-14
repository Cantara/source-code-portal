package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/config"
)

// Exit codes, one table for every command.
const (
	exitOK    = 0 // success
	exitFail  = 1 // a runtime failure: unreadable file, bind error, unclean shutdown
	exitUsage = 2 // refused input: a bad command line or an invalid configuration
)

// failure marks an error as a runtime failure (exit 1); every other error is refused input (exit 2).
type failure struct{ err error }

func (f failure) Error() string { return f.err.Error() }
func (f failure) Unwrap() error { return f.err }

// run executes the command line and returns the process exit code. It never calls os.Exit.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRootCmd(stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, "scp:", err)
	var f failure
	if errors.As(err, &f) {
		return exitFail
	}
	return exitUsage
}

// options are the persistent flags shared by every command.
type options struct {
	configPath string
}

func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	var opts options
	root := &cobra.Command{
		Use:           "scp",
		Short:         "Source Code Portal: an organisational dashboard for GitHub",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVarP(&opts.configPath, "config", "c", "", "YAML configuration file (default: built-in defaults and SCP_* environment)")
	root.PersistentFlags().String("log-level", "", "log level: debug, info, warn, error (default info)")
	root.AddCommand(newServeCmd(&opts), newConfigCmd(&opts), newVersionCmd())
	return root
}

// loadConfig loads the configuration for cmd and checks the groups; an unreadable file is a runtime failure. Group warnings go to
// stderr.
func loadConfig(cmd *cobra.Command, opts *options) (config.Config, map[string]any, error) {
	cfg, settings, err := config.Load(opts.configPath, cmd.Flags())
	var inv *config.Invalid
	if err != nil {
		if !errors.As(err, &inv) {
			err = failure{err}
		}
		return cfg, settings, err
	}
	_, findings := catalog.Compile(cfg.Catalog.Groups)
	var problems []string
	for _, f := range findings {
		if f.Error {
			problems = append(problems, f.Message)
		} else {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "scp: warning:", f.Message)
		}
	}
	if len(problems) > 0 {
		return cfg, settings, &config.Invalid{Problems: problems}
	}
	return cfg, settings, nil
}

func newLogger(c config.Log, w io.Writer) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(c.Level)) // validated by config.Load
	opts := &slog.HandlerOptions{Level: level}
	if c.Format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func newConfigCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect the configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Load and validate the configuration like serve does, then print the effective settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, settings, err := loadConfig(cmd, opts)
			if err != nil {
				return err
			}
			enc := yaml.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent(2)
			if err := enc.Encode(settings); err != nil {
				return failure{err}
			}
			return failure{enc.Close()}.orNil()
		},
	})
	return cmd
}

// orNil returns nil when f wraps no error.
func (f failure) orNil() error {
	if f.err == nil {
		return nil
	}
	return f
}

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version and build information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bi := readBuildInfo()
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return failure{enc.Encode(bi)}.orNil()
			}
			mod := ""
			if bi.Modified {
				mod = " (modified)"
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "scp %s build %s%s %s %s/%s\n", bi.Version, bi.Build, mod, bi.Go, bi.OS, bi.Arch)
			return failure{err}.orNil()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}
