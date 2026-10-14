package job_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
)

func TestKey(t *testing.T) {
	k := job.NewKey("commits.refresh", "acme", "acme-labs", "auth-service")
	if k != "commits.refresh/acme/acme-labs/auth-service" || k.Kind() != "commits.refresh" || k.Tail() != "acme/acme-labs/auth-service" {
		t.Fatal(k)
	}
	if got := k.Parts(); len(got) != 3 || got[2] != "auth-service" {
		t.Fatal(got)
	}
	g := job.GlobalKey("catalog.reconcile")
	if g != "catalog.reconcile/_" || !g.IsGlobal() || k.IsGlobal() {
		t.Fatal(g)
	}
	back, err := job.ParseKey(string(k))
	if err != nil || back != k {
		t.Fatal(err)
	}
}

func TestKeyRejects(t *testing.T) {
	for _, s := range []string{"", "nokind", "commits.refresh", "commits.refresh/", "Commits/a", "commits..x/a", ".x/a", "x./a", "commits/a//b", "commits/a/b\x00", "commits/a/" + strings.Repeat("x", 201), "1x/a", "a b/c", strings.Repeat("a", 600)} {
		if _, err := job.ParseKey(s); err == nil {
			t.Errorf("ParseKey(%q) accepted", s)
		}
	}
	for _, f := range []func(){
		func() { job.NewKey("commits", "a/b") },
		func() { job.NewKey("commits") },
		func() { job.NewKey("Bad", "a") },
		func() { job.NewKey("commits", "") },
		func() { job.NewKey("commits", "a\nb") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("NewKey must panic on an invalid key")
				}
			}()
			f()
		}()
	}
}

func TestValidKind(t *testing.T) {
	for k, want := range map[string]bool{"commits.refresh": true, "catalog.reconcile": true, "integrations.jenkins": true, "github.installations": true, "a": true,
		"manifests.graph": true, "x.y-z_1": true, "x-y": false, "x_y": false, "X": false, "": false, "a.": false, ".a": false, "a..b": false, "a.1": false} {
		if got := job.ValidKind(k); got != want {
			t.Errorf("ValidKind(%q)=%v want %v", k, got, want)
		}
	}
}

func goodSpec() job.Spec {
	return job.Spec{Kind: "commits.refresh", Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled,
		Policy: freshness.Policy{TTL: time.Hour, Jitter: 0.1}, Run: func(context.Context, job.Key) (job.Outcome, error) { return job.Outcome{}, nil }}
}

func TestSpecValidate(t *testing.T) {
	if err := goodSpec().Validate(); err != nil {
		t.Fatal(err)
	}
	mut := map[string]func(*job.Spec){
		"kind": func(s *job.Spec) { s.Kind = "Bad" }, "upstream": func(s *job.Spec) { s.Upstream = "" }, "scope": func(s *job.Spec) { s.Scope = 0 },
		"prio": func(s *job.Spec) { s.Prio = 0 }, "ttl": func(s *job.Spec) { s.Policy.TTL = 0 }, "timeout": func(s *job.Spec) { s.Timeout = -1 }, "run": func(s *job.Spec) { s.Run = nil },
	}
	for name, f := range mut {
		s := goodSpec()
		f(&s)
		if s.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

type rl struct{ at time.Time }

func (rl) Error() string        { return "rate limited" }
func (e rl) RetryAt() time.Time { return e.at }

type perm struct{ v bool }

func (perm) Error() string     { return "perm" }
func (e perm) Permanent() bool { return e.v }

func TestClassify(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		err  error
		want job.Class
	}{
		{"nil", nil, job.ClassNone},
		{"plain is transient", errors.New("boom"), job.ClassTransient},
		{"wrapped rate limit", fmt.Errorf("x: %w", rl{at}), job.ClassRateLimited},
		{"permanent", perm{true}, job.ClassPermanent},
		{"permanent=false is transient", perm{false}, job.ClassTransient},
		{"marked permanent", job.MarkPermanent(errors.New("bad content")), job.ClassPermanent},
		{"marked gone", job.MarkGone(errors.New("repo removed")), job.ClassGone},
		{"gone beats permanent", job.MarkGone(job.MarkPermanent(errors.New("x"))), job.ClassGone},
		{"permanent beats rate limit", errors.Join(rl{at}, perm{true}), job.ClassPermanent},
		{"canceled", fmt.Errorf("x: %w", context.Canceled), job.ClassCanceled},
		{"deadline is transient (a timeout is a failure)", context.DeadlineExceeded, job.ClassTransient},
		{"rate limit beats canceled", errors.Join(rl{at}, context.Canceled), job.ClassRateLimited},
	}
	for _, c := range cases {
		if got := job.Classify(c.err); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got, ok := job.AsRateLimited(errors.Join(perm{true}, fmt.Errorf("w: %w", rl{at}))); !ok || !got.Equal(at) {
		t.Fatal("AsRateLimited must find the signal whatever the class")
	}
	if job.MarkGone(nil) != nil || job.MarkPermanent(nil) != nil {
		t.Fatal("nil stays nil")
	}
	if !errors.Is(job.MarkGone(job.ErrClosed), job.ErrClosed) {
		t.Fatal("Unwrap")
	}
}

func TestRefusal(t *testing.T) {
	if !job.IsRefusal(fmt.Errorf("x: %w", job.ErrQueueFull)) || !job.IsRefusal(errors.Join(errors.New("a"), job.ErrClosed)) || job.IsRefusal(errors.New("job: unknown kind")) || job.IsRefusal(nil) {
		t.Fatal("IsRefusal")
	}
}

func TestEnqueueFunc(t *testing.T) {
	var got job.Key
	var e job.Enqueuer = job.EnqueueFunc(func(k job.Key, _ job.Priority, _ time.Time, _ bool) error { got = k; return nil })
	_ = e.Enqueue("a/b", job.PrioWebhook, time.Time{}, false)
	if got != "a/b" {
		t.Fatal(got)
	}
	if job.PrioInteractive >= job.PrioWebhook || job.PrioWebhook >= job.PrioScheduled || job.PrioScheduled >= job.PrioBackfill {
		t.Fatal("lower runs first")
	}
}

func FuzzParseKey(f *testing.F) {
	for _, s := range []string{"commits.refresh/acme/o/r", "catalog.reconcile/_", "a/b", "x/", "/y", "a//b", "A/b", "a/b\x00", "a.b-c_d/e/f"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		k, err := job.ParseKey(s)
		if err != nil {
			return
		}
		if string(k) != s || !job.ValidKind(k.Kind()) || len(k.Parts()) == 0 {
			t.Fatalf("accepted %q with kind %q parts %q", s, k.Kind(), k.Parts())
		}
		for _, p := range k.Parts() {
			if p == "" || strings.ContainsAny(p, "/\x00\n") {
				t.Fatalf("bad part %q in %q", p, s)
			}
		}
		if again := job.NewKey(k.Kind(), k.Parts()...); again != k { // a parsed key rebuilds to itself
			t.Fatalf("rebuild %q != %q", again, k)
		}
	})
}
