//go:build unix

package main

import (
	"context"
	"errors"
	"fmt"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// maxCommandOutput is the stdout cap of the token_command runner (05 section 2.5: 16 KiB).
const maxCommandOutput = 16 << 10

// errCommandOutput is returned when the program printed more than maxCommandOutput bytes.
var errCommandOutput = errors.New("command output exceeds 16 KiB")

// capWriter keeps the first maxCommandOutput bytes and discards the rest while still draining the pipe, so a chatty program is
// never blocked on a full pipe and the portal never holds more than the cap.
type capWriter struct {
	buf  []byte
	over bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	room := maxCommandOutput - len(w.buf)
	if len(p) > room {
		w.buf = append(w.buf, p[:max(room, 0)]...)
		w.over = true
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// commandRunner is auth.CommandRunner of unit 05 (assignable to it: the same func type). It lives in cmd/scp because only cmd/scp and
// internal/gitsource may import os/exec (G6, GR-36); the product wire passes newCommandRunner(env.Environ) as github.Deps.RunCommand
// and the skeleton exercises it in tests only (V16-08).
type commandRunner = func(ctx context.Context, argv []string, timeout time.Duration) ([]byte, error)

// newCommandRunner returns the runner of the token_command strategy (05 section 2.5): no shell, an absolute program path, a timeout
// (default 10 s), stdout capped at 16 KiB, stderr discarded (it can carry a credential, so it is never logged), the whole process group
// killed when the context ends, and an environment without any SCP_* variable (the portal's own configuration and secrets are not
// handed to a helper program).
func newCommandRunner(environ []string) commandRunner {
	var childEnv []string
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "SCP_") {
			childEnv = append(childEnv, kv)
		}
	}
	return func(ctx context.Context, argv []string, timeout time.Duration) ([]byte, error) {
		if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
			return nil, errors.New("command: argv[0] must be an absolute program path")
		}
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := osexec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: argv is operator configuration, absolute path, and no shell is involved
		cmd.Env = childEnv
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) } // the program and everything it spawned
		cmd.WaitDelay = 2 * time.Second
		out := &capWriter{}
		cmd.Stdout = out
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("command %s: %w", filepath.Base(argv[0]), ctx.Err())
			}
			return nil, fmt.Errorf("command %s: %w", filepath.Base(argv[0]), err)
		}
		if out.over {
			return nil, errCommandOutput
		}
		return out.buf, nil
	}
}
