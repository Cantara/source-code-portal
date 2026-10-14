package bridge_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/cantara/source-code-portal/cmd/scp/internal/bridge"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/ops"
	"github.com/cantara/source-code-portal/internal/reactor"
	"github.com/cantara/source-code-portal/internal/refresh"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

type fakePlans struct {
	mu    sync.Mutex
	calls [][]store.PlanRow
	fail  error
}

func (f *fakePlans) SavePlans(_ context.Context, rows []store.PlanRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.calls = append(f.calls, rows)
	return nil
}

func (f *fakePlans) snapshot() [][]store.PlanRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]store.PlanRow(nil), f.calls...)
}

func (f *fakePlans) total() (n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		n += len(c)
	}
	return n
}

func TestPlanSinkBatchesAndFlushesOnTimerAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := &fakePlans{}
		sink := bridge.NewPlanSink(st, 5*time.Second, nil)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- sink.Run(ctx) }()

		now := time.Now()
		sink.SaveState(job.NewKey("commits.refresh", "acme", "acme-org", "api"), refresh.PlanState{NextDue: now.Add(time.Hour)})
		sink.SaveState(job.NewKey("commits.refresh", "acme", "acme-org", "api"), refresh.PlanState{NextDue: now.Add(2 * time.Hour), UnchangedStreak: 3}) // newer wins
		sink.SaveState(job.NewKey("docs.refresh", "acme", "acme-org", "api"), refresh.PlanState{NextDue: now.Add(time.Minute)})
		sink.SaveState(job.NewKey("catalog.reconcile", "_"), refresh.PlanState{}) // global: no item row
		if sink.Pending() != 2 || sink.Dropped() != 1 {
			t.Fatalf("pending=%d dropped=%d", sink.Pending(), sink.Dropped())
		}

		time.Sleep(4 * time.Second)
		synctest.Wait()
		if len(st.snapshot()) != 0 {
			t.Fatal("flushed before the interval")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		calls := st.snapshot()
		if len(calls) != 1 || len(calls[0]) != 2 {
			t.Fatalf("calls=%v", calls)
		}
		for _, r := range calls[0] {
			if r.Key.Kind == "commits.refresh" && (r.UnchangedStreak != 3 || r.Key.Repo != (store.RepoKey{Source: "acme", Owner: "acme-org", Name: "api"})) {
				t.Fatalf("row %+v", r)
			}
		}

		// state written after the last tick is only persisted by Close, after Run returned
		sink.SaveState(job.NewKey("commits.refresh", "acme", "acme-org", "web"), refresh.PlanState{})
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if st.total() != 2 {
			t.Fatal("Run must not flush on exit")
		}
		if err := sink.Close(t.Context()); err != nil || st.total() != 3 {
			t.Fatalf("close: %v total=%d", err, st.total())
		}
	})
}

// blockingPlans holds the first SavePlans call until released and then fails it, the second call succeeds.
type blockingPlans struct {
	mu      sync.Mutex
	n       int
	entered chan struct{}
	release chan struct{}
	saved   []store.PlanRow
}

func (b *blockingPlans) SavePlans(_ context.Context, rows []store.PlanRow) error {
	b.mu.Lock()
	b.n++
	n := b.n
	b.mu.Unlock()
	if n == 1 {
		close(b.entered)
		<-b.release
		return errors.New("disk full")
	}
	b.mu.Lock()
	b.saved = append(b.saved, rows...)
	b.mu.Unlock()
	return nil
}

func TestPlanSinkSerializesConcurrentFlushes(t *testing.T) {
	st := &blockingPlans{entered: make(chan struct{}), release: make(chan struct{})}
	sink := bridge.NewPlanSink(st, time.Hour, nil)
	k := job.NewKey("commits.refresh", "acme", "o", "r")
	old := time.Unix(1000, 0)
	sink.SaveState(k, refresh.PlanState{NextDue: old})
	errA := make(chan error, 1)
	go func() { errA <- sink.Flush(t.Context()) }() // A: takes the old state and blocks inside the store
	<-st.entered
	sink.SaveState(k, refresh.PlanState{NextDue: old.Add(time.Hour)}) // newer state arrives while A is in flight
	errB := make(chan error, 1)
	go func() { errB <- sink.Flush(t.Context()) }() // B must wait for A, not race it
	time.Sleep(20 * time.Millisecond)
	close(st.release) // A fails and puts nothing back over the newer state
	if err := <-errA; err == nil {
		t.Fatal("flush A must fail")
	}
	if err := <-errB; err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.saved) != 1 || !st.saved[0].NextDue.Equal(old.Add(time.Hour)) {
		t.Fatalf("the newest state must win, saved=%+v", st.saved)
	}
	if sink.Pending() != 0 {
		t.Fatalf("pending=%d", sink.Pending())
	}
}

func TestPlanSinkKeepsLatestStateWhenTheStoreFails(t *testing.T) {
	st := &fakePlans{fail: errors.New("disk full")}
	var seen []string
	sink := bridge.NewPlanSink(st, time.Hour, func(op string, _ error) { seen = append(seen, op) })
	k := job.NewKey("commits.refresh", "acme", "o", "r")
	sink.SaveState(k, refresh.PlanState{UnchangedStreak: 1})
	if err := sink.Flush(t.Context()); err == nil || sink.Pending() != 1 || len(seen) != 1 || seen[0] != "SavePlans" {
		t.Fatalf("err=%v pending=%d seen=%v", err, sink.Pending(), seen)
	}
	st.fail = nil
	sink.SaveState(k, refresh.PlanState{UnchangedStreak: 2}) // newer state arrives between failure and retry
	if err := sink.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := st.snapshot()[0][0].UnchangedStreak; got != 2 {
		t.Fatalf("persisted %d, want the newest state 2", got)
	}
}

func TestPlanSinkRaceFreeUnderConcurrentSaveAndFlush(t *testing.T) {
	st := &fakePlans{}
	sink := bridge.NewPlanSink(st, time.Hour, nil)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 200 {
				sink.SaveState(job.NewKey("commits.refresh", "s", "o", string(rune('a'+i))), refresh.PlanState{UnchangedStreak: j})
				if j%50 == 0 {
					_ = sink.Flush(t.Context())
				}
			}
		})
	}
	wg.Wait()
	_ = sink.Flush(t.Context())
}

type admitter struct{ got []github.ReserveClass }

func (a *admitter) Admit(c github.ReserveClass) time.Duration {
	a.got = append(a.got, c)
	return time.Duration(c+1) * time.Second
}

// The mapping is the one 05 section 2.3 prints; any other upstream is not gated, and an unknown priority is interactive.
func TestRateGateMapsPrioritiesAndOnlyGatesGitHub(t *testing.T) {
	a := &admitter{}
	g := bridge.RateGate{C: a}
	for p, want := range map[job.Priority]github.ReserveClass{
		job.PrioBackfill: github.ClassBackfill, job.PrioScheduled: github.ClassScheduled,
		job.PrioWebhook: github.ClassWebhook, job.PrioInteractive: github.ClassInteractive, job.Priority(200): github.ClassInteractive,
	} {
		a.got = nil
		if d := g.Admit("github", p); len(a.got) != 1 || a.got[0] != want || d != time.Duration(want+1)*time.Second {
			t.Errorf("priority %v: asked %v, waited %v, want class %v", p, a.got, d, want)
		}
	}
	a.got = nil
	for _, up := range []string{"jenkins", "snyk", "maven-central", "deps-dev", "local", ""} {
		if d := g.Admit(up, job.PrioBackfill); d != 0 {
			t.Errorf("%s was gated: %v", up, d)
		}
	}
	if len(a.got) != 0 {
		t.Errorf("the adapter was asked for a non-github upstream: %v", a.got)
	}
}

type fakePlanner struct{ got map[string][]job.Key }

func (f *fakePlanner) SetSubjects(kind string, keys []job.Key) {
	if f.got == nil {
		f.got = map[string][]job.Key{}
	}
	f.got[kind] = keys
}

type fakeTick struct{ n int }

func (f *fakeTick) Trigger(name string) bool { f.n++; return name == bridge.PlannerTickTask }

func TestTrackerBuildsKeysPerKindInOrderAndTicks(t *testing.T) {
	p, tick := &fakePlanner{}, &fakeTick{}
	tr := bridge.NewTracker([]string{"commits.refresh", "docs.refresh"}, p, tick)
	tr.SetRepos([]scm.RepoRef{{Source: "acme", Owner: "o", Name: "b"}, {Source: "acme", Owner: "o", Name: "a"}})
	if len(p.got) != 2 || tick.n != 1 {
		t.Fatalf("%v %d", p.got, tick.n)
	}
	if got := p.got["docs.refresh"]; got[0] != "docs.refresh/acme/o/b" || got[1] != "docs.refresh/acme/o/a" {
		t.Fatalf("order not preserved: %v", got)
	}
}

type recEnq struct {
	k     job.Key
	p     job.Priority
	force bool
	err   error
}

func (r *recEnq) Enqueue(k job.Key, p job.Priority, _ time.Time, force bool) error {
	r.k, r.p, r.force = k, p, force
	return r.err
}

func TestRetrierForcesAtInteractivePriority(t *testing.T) {
	e := &recEnq{}
	r := bridge.NewRetrier(e)
	if err := r.Retry("commits.refresh/acme/o/r"); err != nil || e.p != job.PrioInteractive || !e.force || e.k != "commits.refresh/acme/o/r" {
		t.Fatalf("%v %+v", err, e)
	}
	for _, bad := range []string{"", "nokind", "/x"} {
		if err := r.Retry(bad); !errors.Is(err, ops.ErrUnknownKey) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// ops cannot import job or refresh, so the bridge is where the engine's sentinels become the three values the admin handler maps
// to 404 and 503 (15 section 4.4 item 6). Anything else stays an error (500), carrying the kind only.
func TestRetrierTranslatesTheEnginesSentinels(t *testing.T) {
	for _, tc := range []struct {
		in   error
		want error
	}{
		{refresh.ErrUnknownKind, ops.ErrUnknownKey},
		{fmt.Errorf("%w: %q", refresh.ErrUnknownKind, "x"), ops.ErrUnknownKey},
		{job.ErrQueueFull, ops.ErrQueueFull},
		{job.ErrClosed, ops.ErrClosed},
	} {
		if err := bridge.NewRetrier(&recEnq{err: tc.in}).Retry("commits.refresh/acme/o/r"); !errors.Is(err, tc.want) {
			t.Errorf("%v -> %v, want %v", tc.in, err, tc.want)
		}
	}
	boom := errors.New("boom")
	err := bridge.NewRetrier(&recEnq{err: boom}).Retry("commits.refresh/acme/secret-repo/r")
	if !errors.Is(err, boom) || strings.Contains(err.Error(), "secret-repo") {
		t.Errorf("an unknown error must pass through without the repository name: %v", err)
	}
}

func TestReactorMetricsAreBoundedAndLabelled(t *testing.T) {
	rd := sdkmetric.NewManualReader()
	obs, err := bridge.NewReactorMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(rd)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	obs(reactor.Observation{Reactor: "catalog", Kind: scm.EventPush, Outcome: reactor.OutcomeOK, Duration: 200 * time.Microsecond})
	obs(reactor.Observation{Reactor: "catalog", Kind: scm.EventPush, Outcome: reactor.OutcomeRefused, Duration: time.Millisecond})
	var rm metricdata.ResourceMetrics
	if err := rd.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				names[m.Name] = len(d.DataPoints)
			case metricdata.Histogram[float64]:
				names[m.Name] = len(d.DataPoints)
			}
		}
	}
	if names[bridge.ReactorRunsMetric] != 2 || names[bridge.ReactorSecondsMetric] != 1 {
		t.Fatalf("series: %v (want 2 outcome series, 1 histogram series per reactor)", names)
	}
}

type tasks struct{ m map[string]store.TaskState }

func (t *tasks) SaveTask(_ context.Context, n string, s store.TaskState) error {
	if t.m == nil {
		t.m = map[string]store.TaskState{}
	}
	t.m[n] = s
	return nil
}
func (t *tasks) LoadTasks(context.Context) (map[string]store.TaskState, error) { return t.m, nil }

func TestWatermarksRoundTrip(t *testing.T) {
	w := bridge.NewWatermarks(&tasks{})
	if got, err := w.Load(t.Context(), "webhook.redelivery/acme"); err != nil || !got.IsZero() {
		t.Fatalf("%v %v", got, err)
	}
	at := time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC)
	if err := w.Save(t.Context(), "webhook.redelivery/acme", at); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.Load(t.Context(), "webhook.redelivery/acme"); !got.Equal(at) {
		t.Fatalf("%v", got)
	}
}

type sinkFn func(context.Context, scm.Event) error

func (f sinkFn) Handle(ctx context.Context, ev scm.Event) error { return f(ctx, ev) }

func TestLateSinkForwardsAfterSetAndRefusesBefore(t *testing.T) {
	var s bridge.LateSink
	if err := s.Handle(t.Context(), scm.Event{}); !errors.Is(err, bridge.ErrSinkNotWired) {
		t.Fatalf("before Set: %v", err)
	}
	want := errors.New("from the fan")
	var got scm.Event
	s.Set(sinkFn(func(_ context.Context, ev scm.Event) error { got = ev; return want }))
	if err := s.Handle(t.Context(), scm.Event{Kind: scm.EventPush}); !errors.Is(err, want) || got.Kind != scm.EventPush {
		t.Fatalf("after Set: %v %v", err, got)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a second Set must panic")
			}
		}()
		s.Set(sinkFn(func(context.Context, scm.Event) error { return nil }))
	}()
}

// TestPlanSinkTranslatesJobKindToItemKind pins CD-JB-01: the plan rows must carry the item kind of the owning feature, or SavePlans
// matches no item row and nothing is persisted (unit 03 plan-kind rule, V-02).
func TestPlanSinkTranslatesJobKindToItemKind(t *testing.T) {
	st := &fakePlans{}
	sink := bridge.NewPlanSink(st, time.Hour, nil).WithItemKinds(map[string]string{"commits.refresh": "commits.ring", "docs.refresh": "docs.repo", "manifests.refresh": "manifests.facts"})
	now := time.Unix(5000, 0)
	for _, kind := range []string{"commits.refresh", "docs.refresh", "manifests.refresh", "catalog.stats"} {
		sink.SaveState(job.NewKey(kind, "acme", "o", "r"), refresh.PlanState{NextDue: now})
	}
	if err := sink.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range st.snapshot() {
		for _, r := range c {
			got[r.Key.Kind] = true
		}
	}
	for _, want := range []string{"commits.ring", "docs.repo", "manifests.facts", "catalog.stats"} {
		if !got[want] {
			t.Fatalf("missing item kind %q in %v", want, got)
		}
	}
	if got["commits.refresh"] || got["docs.refresh"] || got["manifests.refresh"] {
		t.Fatalf("a job kind leaked into a plan row: %v", got)
	}
	if sink.JobKindOf("commits.ring") != "commits.refresh" || sink.JobKindOf("catalog.stats") != "catalog.stats" {
		t.Fatal("inverse mapping")
	}
}
