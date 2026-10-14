package main

import (
	"context"
	"io"
	"net"
	"strings"

	"github.com/cantara/source-code-portal/internal/github"
)

// Env is everything the process hands to run: no function below main reads os.Args, the environment, the standard streams
// or opens a listener by itself, so every command is testable with a fake Env (Mat Ryer's run pattern).
type Env struct {
	Args           []string // os.Args[1:]
	Environ        []string // os.Environ(): KEY=VALUE pairs, the form config.Load takes
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Listen opens a TCP listener. Tests pass a wrapper that records the bound address of ":0" listeners.
	Listen func(addr string) (net.Listener, error)
	// Hostname is the service.instance.id of the telemetry resource ("" when the host has none).
	Hostname string
	// Reload delivers one value per SIGHUP (main.go: signal.Notify, a one-slot channel, coalescing). serve answers each with Wired.Reload; a nil
	// channel means "no reload" (tests, and every command but serve). SIGHUP never stops the process and never re-reads the configuration
	// (a configuration change is a restart); it rotates what 05 and 10 asked for: GitHub App keys and the pages set.
	Reload <-chan struct{}
	// Seed seeds the engine, planner and scheduler jitter; 0 draws a seed from crypto/rand (tests pass a fixed one).
	Seed uint64

	// Seams for the commands that talk to GitHub. main.go sets the real ones (built from the validated Config with the netguard
	// client factory); tests set fakes. A nil seam makes the command exit 1 with "not available in this build".
	Doctor      func(ctx context.Context, cfg Config, o github.DoctorOptions) ([]github.Finding, error)
	AppManifest func(ctx context.Context, o github.ManifestOptions) (github.ManifestResult, error)

	// Wire builds the features (startup steps 2-7). main.go sets wire.go's product implementation; tests of the lifecycle set a stub.
	Wire WireFunc
}

// Getenv looks a variable up in Environ (used for flag defaults such as SCP_CONFIG).
func (e Env) Getenv(key string) string {
	for _, kv := range e.Environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// Exit codes (design 16 section 4.1). One table for every subcommand.
const (
	exitOK    = 0 // success; for lint/doctor/check: nothing found at error level (and nothing at warn level under --strict)
	exitFail  = 1 // the thing asked for failed: an I/O or runtime error, doctor FAIL, a corrupt store
	exitUsage = 2 // the INPUT was refused: a bad command line, or a configuration or descriptor with violations (02 section 4.3.8)
	exitWarn  = 3 // completed with caveats: warnings only under --strict

	exitInvalid = exitUsage // the same value read as "violations in the file": lint, descriptor lint and the refusal of serve
)
