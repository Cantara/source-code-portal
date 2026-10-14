package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
)

func testSpec(kind, upstream string, run job.RunFunc) job.Spec {
	return job.Spec{Kind: kind, Upstream: upstream, Scope: job.ScopeRepo, Prio: job.PrioScheduled,
		Policy: freshness.Policy{TTL: time.Hour, Jitter: 0.1, IdleBackoffMax: 4 * time.Hour}, Run: run}
}

func defaultRefresh(t *testing.T) RefreshConfig {
	t.Helper()
	l, err := readConfig(loadOptions{skipSecrets: true})
	if err != nil || len(l.violations) > 0 {
		t.Fatalf("defaults must load: %v %v", err, l.violations)
	}
	return l.cfg.Refresh
}

// The defaults list the four upstreams the units name (github, jenkins, snyk, deps-dev) and every key is read by newRefreshCore.
func TestRefreshDefaultsAndRegistration(t *testing.T) {
	cfg := defaultRefresh(t)
	for _, u := range []string{"github", "jenkins", "snyk", "deps-dev"} {
		if _, ok := cfg.Upstreams[u]; !ok {
			t.Errorf("default.yaml lacks upstream %q", u)
		}
	}
	r, err := newRefreshCore(refreshIn{Cfg: cfg, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	run := func(context.Context, job.Key) (job.Outcome, error) { return job.Outcome{}, nil }
	if err := r.Register(testSpec("commits.refresh", "github", run)); err != nil {
		t.Fatal(err)
	}
	// Problems are errors with a message, never the library's panic.
	err = r.Register(
		testSpec("commits.refresh", "github", run),   // twice
		testSpec("sbom.fetch", "maven-central", run), // upstream not listed
		job.Spec{Kind: "Bad Kind"},                   // invalid spec
	)
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"registered twice", `upstream "maven-central", which refresh.upstreams does not list`, "Bad Kind"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	if got := cfg.stop(); got != 19*time.Second {
		t.Errorf("refresh stage stop = %s, want grace 15s + 3s + 1s = 19s (04 section 2.5)", got)
	}
}

// The stage runs a job enqueued through the PLANNER (the Enqueuer of everyone but a Spec's own Run) and stops inside its StopTimeout.
func TestRefreshStageRunsAJobAndStops(t *testing.T) {
	cfg := defaultRefresh(t)
	cfg.Grace = time.Second
	cfg.PlannerTick = time.Second
	r, err := newRefreshCore(refreshIn{Cfg: cfg, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	var ran atomic.Int32
	done := make(chan struct{})
	if err := r.Register(testSpec("commits.refresh", "github", func(context.Context, job.Key) (job.Outcome, error) {
		if ran.Add(1) == 1 {
			close(done)
		}
		return job.Outcome{}, nil
	})); err != nil {
		t.Fatal(err)
	}
	st := r.Stage()
	if st.Name != "refresh" || st.StopTimeout != cfg.stop() || st.Run == nil {
		t.Fatalf("stage = %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan error, 1)
	go func() { ended <- st.Run(ctx) }()
	if err := r.Planner.Enqueue(job.NewKey("commits.refresh", "github", "acme", "web"), job.PrioInteractive, time.Time{}, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the job did not run")
	}
	cancel()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("stage error: %v", err)
		}
	case <-time.After(st.StopTimeout):
		t.Fatal("the stage did not stop within its StopTimeout")
	}
}

// Section rules: backoff.cap below base is refused with the key path; an environment variable changes an existing upstream and
// cannot create one.
func TestRefreshConfigRules(t *testing.T) {
	l, err := readConfig(loadOptions{skipSecrets: true, set: []string{"refresh.backoff.cap=1s", "refresh.backoff.base=2s"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.violationText(), "refresh.backoff.cap: must not be smaller than base") {
		t.Errorf("violations = %q", l.violationText())
	}
	l, err = readConfig(loadOptions{skipSecrets: true, environ: []string{"SCP_REFRESH_UPSTREAMS_DEPS_DEV_CONCURRENCY=3", "SCP_REFRESH_UPSTREAMS_NOPE_CONCURRENCY=2"}})
	if err != nil || len(l.violations) > 0 {
		t.Fatalf("%v %v", err, l.violations)
	}
	if got := l.cfg.Refresh.Upstreams["deps-dev"].Concurrency; got != 3 {
		t.Errorf("deps-dev concurrency = %d, want 3 from the environment", got)
	}
	if _, ok := l.cfg.Refresh.Upstreams["nope"]; ok {
		t.Error("the environment created an upstream")
	}
}

func TestDeriveGivesIndependentStreams(t *testing.T) {
	seen := map[uint64]bool{}
	for n := range uint64(8) {
		seen[derive(42, n)] = true
	}
	again := derive(42, 3)
	if len(seen) != 8 || !seen[again] || derive(42, 0) == derive(43, 0) {
		t.Errorf("derive is not a deterministic, well-spread function: %v", seen)
	}
}
