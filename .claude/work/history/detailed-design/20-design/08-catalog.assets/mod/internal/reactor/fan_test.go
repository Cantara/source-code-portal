package reactor_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/reactor"
	"github.com/cantara/source-code-portal/internal/scm"
)

type recorder struct {
	mu   sync.Mutex
	keys []job.Key
	err  error
}

func (r *recorder) Enqueue(k job.Key, _ job.Priority, _ time.Time, _ bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = append(r.keys, k)
	return r.err
}

var ev = scm.Event{Kind: scm.EventPush, Delivery: "d-1", Ref: "refs/heads/main", Repo: scm.RepoRef{Source: "acme", Owner: "o", Name: "r"}}

func enqueueKind(kind string) reactor.Func {
	return func(_ context.Context, e scm.Event, enq job.Enqueuer) error {
		return enq.Enqueue(job.NewKey(kind, e.Repo.KeyParts()...), job.PrioWebhook, time.Time{}, false)
	}
}

func TestFanRunsEveryReactorInOrder(t *testing.T) {
	rec := &recorder{}
	f := reactor.New(rec, reactor.Deps{}, enqueueKind("commits.refresh"), enqueueKind("docs.refresh"), enqueueKind("manifests.refresh"))
	if err := f.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	want := []job.Key{"commits.refresh/acme/o/r", "docs.refresh/acme/o/r", "manifests.refresh/acme/o/r"}
	if len(rec.keys) != 3 || rec.keys[0] != want[0] || rec.keys[1] != want[1] || rec.keys[2] != want[2] {
		t.Fatal(rec.keys)
	}
}

func TestRefusalIsPropagatedAndDoesNotStopOthers(t *testing.T) {
	for _, refusal := range []error{job.ErrQueueFull, job.ErrClosed} {
		rec := &recorder{err: refusal}
		f := reactor.NewNamed(rec, reactor.Deps{},
			reactor.Reactor{Name: "commits", React: enqueueKind("commits.refresh")},
			reactor.Reactor{Name: "docs", React: enqueueKind("docs.refresh")})
		err := f.Handle(context.Background(), ev)
		if !job.IsRefusal(err) || !errors.Is(err, refusal) {
			t.Fatalf("webhook needs errors.Is to see the refusal %v: %v", refusal, err)
		}
		if len(rec.keys) != 2 {
			t.Fatalf("both reactors must still run: %v", rec.keys)
		}
		if !strings.Contains(err.Error(), "reactor commits") || !strings.Contains(err.Error(), "reactor docs") {
			t.Fatalf("errors name their reactor: %v", err)
		}
	}
}

func TestPanicIsRecoveredCountedAndJoined(t *testing.T) {
	var obs []reactor.Observation
	var mu sync.Mutex
	rec := &recorder{}
	f := reactor.NewNamed(rec, reactor.Deps{Observe: func(o reactor.Observation) { mu.Lock(); obs = append(obs, o); mu.Unlock() }},
		reactor.Reactor{Name: "boom", React: func(context.Context, scm.Event, job.Enqueuer) error { panic("kaboom") }},
		reactor.Reactor{Name: "other", React: enqueueKind("docs.refresh")},
		reactor.Reactor{Name: "bad", React: func(context.Context, scm.Event, job.Enqueuer) error { return errors.New("nope") }})
	err := f.Handle(context.Background(), ev)
	var pe *reactor.PanicError
	if !errors.As(err, &pe) || pe.Reactor != "boom" {
		t.Fatalf("%v", err)
	}
	if job.IsRefusal(err) {
		t.Fatal("a panic or a plain error is not a refusal: the delivery is still accepted")
	}
	if len(rec.keys) != 1 {
		t.Fatal("the reactor after the panic must run")
	}
	want := []reactor.Outcome{reactor.OutcomePanic, reactor.OutcomeOK, reactor.OutcomeError}
	for i, o := range obs {
		if o.Outcome != want[i] || o.Kind != scm.EventPush {
			t.Errorf("observation %d: %+v", i, o)
		}
	}
	if len(obs) != 3 {
		t.Fatal(obs)
	}
}

func TestReactorsDoNotSeeCancellationButSeeValues(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	cancel()
	var sawErr error
	var sawVal any
	f := reactor.New(&recorder{}, reactor.Deps{}, func(ctx context.Context, _ scm.Event, _ job.Enqueuer) error {
		sawErr, sawVal = ctx.Err(), ctx.Value(key{})
		return nil
	})
	_ = f.Handle(ctx, ev)
	if sawErr != nil || sawVal != "v" {
		t.Fatalf("err=%v val=%v", sawErr, sawVal)
	}
}

func TestConstructorPanics(t *testing.T) {
	for name, f := range map[string]func(){
		"nil enqueuer": func() { reactor.New(nil, reactor.Deps{}) },
		"nil func":     func() { reactor.New(&recorder{}, reactor.Deps{}, nil) },
		"no name":      func() { reactor.NewNamed(&recorder{}, reactor.Deps{}, reactor.Reactor{React: enqueueKind("x")}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			f()
		}()
	}
}

func TestConcurrentHandle(t *testing.T) {
	rec := &recorder{}
	f := reactor.New(rec, reactor.Deps{}, enqueueKind("a.b"), enqueueKind("c.d"))
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = f.Handle(context.Background(), ev) }()
	}
	wg.Wait()
	if len(rec.keys) != 64 {
		t.Fatal(len(rec.keys))
	}
}

func TestDurationUsesTheVirtualClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var got time.Duration
		f := reactor.New(&recorder{}, reactor.Deps{Observe: func(o reactor.Observation) { got = o.Duration }},
			func(context.Context, scm.Event, job.Enqueuer) error { time.Sleep(3 * time.Millisecond); return nil })
		_ = f.Handle(context.Background(), ev)
		if got != 3*time.Millisecond {
			t.Fatal(got)
		}
	})
}

func TestInvalidRepositoryNeverReachesReactors(t *testing.T) {
	rec := &recorder{}
	ran := false
	f := reactor.New(rec, reactor.Deps{}, func(context.Context, scm.Event, job.Enqueuer) error { ran = true; return nil })
	bad := ev
	bad.Repo.Owner = "a/b"
	err := f.Handle(context.Background(), bad)
	if ran || !errors.Is(err, scm.ErrInvalid) || job.IsRefusal(err) {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
	inst := scm.Event{Kind: scm.EventInstallation, Delivery: "d-2"} // installation-level: zero Repo is legal
	if err := f.Handle(context.Background(), inst); err != nil || !ran {
		t.Fatalf("zero repo must pass: ran=%v err=%v", ran, err)
	}
}

func TestNoReactorsIsNotAnError(t *testing.T) {
	if err := reactor.New(&recorder{}, reactor.Deps{}).Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
}

func TestFanCopiesItsReactors(t *testing.T) {
	rec := &recorder{}
	rs := []reactor.Reactor{{Name: "a", React: enqueueKind("a.b")}}
	f := reactor.NewNamed(rec, reactor.Deps{}, rs...)
	rs[0].React = func(context.Context, scm.Event, job.Enqueuer) error { panic("replaced after construction") }
	if err := f.Handle(context.Background(), ev); err != nil || len(rec.keys) != 1 {
		t.Fatalf("err=%v keys=%v", err, rec.keys)
	}
}
