//go:build unix

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCommandRunnerRunsWithoutAShell(t *testing.T) {
	run := newCommandRunner(nil)
	out, err := run(t.Context(), []string{"/bin/echo", "a;b $HOME `x`"}, time.Second)
	if err != nil || string(out) != "a;b $HOME `x`\n" {
		t.Fatalf("out=%q err=%v: the arguments must reach the program verbatim", out, err)
	}
}

func TestCommandRunnerRefusesARelativeProgram(t *testing.T) {
	run := newCommandRunner(nil)
	for _, argv := range [][]string{nil, {}, {"echo", "x"}, {"./echo"}} {
		if _, err := run(t.Context(), argv, time.Second); err == nil {
			t.Errorf("argv %q accepted", argv)
		}
	}
}

func TestCommandRunnerCapsStdout(t *testing.T) {
	run := newCommandRunner(nil)
	_, err := run(t.Context(), []string{"/bin/sh", "-c", "head -c 20000 /dev/zero"}, 5*time.Second)
	if !errors.Is(err, errCommandOutput) {
		t.Fatalf("err=%v, want errCommandOutput", err)
	}
	out, err := run(t.Context(), []string{"/bin/sh", "-c", "head -c 16384 /dev/zero | tr '\\0' x"}, 5*time.Second)
	if err != nil || len(out) != maxCommandOutput {
		t.Fatalf("exactly the cap must pass: len=%d err=%v", len(out), err)
	}
}

func TestCommandRunnerKillsTheWholeProcessGroupOnTimeout(t *testing.T) {
	run := newCommandRunner(nil)
	t0 := time.Now()
	_, err := run(t.Context(), []string{"/bin/sh", "-c", "sleep 30 & wait"}, 300*time.Millisecond)
	took := time.Since(t0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want a deadline error", err)
	}
	// The grandchild (sleep) holds the stdout pipe: if only the shell were killed, Wait would return after WaitDelay (2 s).
	if took > 1500*time.Millisecond {
		t.Fatalf("took %v: the process group was not killed", took)
	}
}

func TestCommandRunnerHidesThePortalEnvironment(t *testing.T) {
	run := newCommandRunner([]string{"SCP_GITHUB_TOKEN=secret", "KEEP=1"})
	out, err := run(t.Context(), []string{"/usr/bin/env"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "SCP_") || !strings.Contains(string(out), "KEEP=1") {
		t.Fatalf("environment = %q", out)
	}
}

func TestCommandRunnerErrorNeverCarriesTheOutput(t *testing.T) {
	run := newCommandRunner(nil)
	_, err := run(t.Context(), []string{"/bin/sh", "-c", "echo token-abc; echo token-def >&2; exit 3"}, 5*time.Second)
	if err == nil || strings.Contains(err.Error(), "token-") {
		t.Fatalf("err=%v", err)
	}
}
