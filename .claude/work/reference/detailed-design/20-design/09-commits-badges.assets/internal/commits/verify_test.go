package commits

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// V-01: the job key of a reaction is built from KeyParts (folded), so it coalesces with the planner's key and the
// store key (store.KeyOf) addresses the same repository; a mixed-case catalogue entry is the realistic input
// (GitHub keeps the display case: Acme/Billing-Service).
func TestReactKeyIsFoldedAndRunWritesTheFoldedStoreKey(t *testing.T) {
	r := repo("Billing-Service", scm.VisPublic)
	r.Slug = "billing-service"
	r.Ref.Owner, r.Ref.Name = "Acme", "Billing-Service"
	e := newEnv(t, false, r)
	var enq recEnq
	ev := scm.Event{Kind: scm.EventPush, Repo: scm.RepoRef{Source: "s", ID: r.Ref.ID, Owner: "ACME", Name: "BILLING-SERVICE"}, Ref: "refs/heads/main"}
	if err := e.svc.React(context.Background(), ev, &enq); err != nil || len(enq.keys) != 1 {
		t.Fatalf("react: %v %v", err, enq.keys)
	}
	want := job.NewKey(JobKind, r.Ref.KeyParts()...)
	if enq.keys[0] != want || string(want) != "commits.refresh/s/acme/billing-service" {
		t.Fatalf("key %q, want the folded %q", enq.keys[0], want)
	}
	e.lister.res["Billing-Service"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, time.Now())}
	if _, err := e.svc.run(context.Background(), enq.keys[0]); err != nil {
		t.Fatal(err) // the fake store validates the key like the real one: an unfolded RepoKey would fail here
	}
	if len(e.store.rows[store.RepoKey{Source: "s", Owner: "acme", Name: "billing-service"}]) != 2 {
		t.Fatal("rows must be stored under the folded key")
	}
}

// V-02: within one repository the ring order is scm.CompareCommits; the cross-repository order refines it by slug.
func TestRingOrderIsTheKernelOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range 200 {
		var cs []scm.Commit
		for i := 0; i < 20; i++ {
			cs = append(cs, scm.Commit{SHA: sha(rng.IntN(40) + 1), CommittedAt: base.Add(time.Duration(rng.IntN(3)) * time.Second)})
		}
		a := slices.Clone(cs)
		slices.SortStableFunc(a, scm.CompareCommits)
		b := slices.Clone(cs)
		slices.SortStableFunc(b, compareWithin)
		if !slices.Equal(a, b) {
			t.Fatal("compareWithin must be scm.CompareCommits")
		}
		for i := range a {
			if Compare("x", a[i], "x", b[i]) != 0 {
				t.Fatal("Compare within one slug must agree with the kernel order")
			}
		}
	}
}

// V-03: the cleaner is the kernel's: tag-block characters (ASCII smuggling), soft hyphen and the invisible operators
// are removed, the cap is in runes with an ellipsis.
func TestCleanLineUsesKernelCleaner(t *testing.T) {
	smuggled := "fix\U000E0049\U000E0067\U000E006E\U000E006F\U000E0072\U000E0065 bug­⁢⁦x"
	if got := cleanLine(smuggled, 200); got != "fix bugx" {
		t.Fatalf("got %q", got)
	}
	if got := cleanLine(strings.Repeat("ab", 150), 200); len([]rune(got)) != 200 || !strings.HasSuffix(got, "…") {
		t.Fatalf("cap: %d runes", len([]rune(got)))
	}
	if got := cleanLine(strings.Repeat("a", 200), 200); got != strings.Repeat("a", 200) {
		t.Fatal("exactly the cap is not cut")
	}
	if got := cleanLine("subject\r\nbody", 200); got != "subject" {
		t.Fatalf("first line only: %q", got)
	}
}

// V-04: a confirmation that finds no good item in the store (an earlier write failed) writes the ring it serves.
func TestRevalidationHealsAMissingStoreItem(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(3, 0, time.Now()), Validators: scm.Validators{SHA: sha(1)}}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	rk := store.RepoKey{Source: "s", Owner: "o", Name: "a"}
	ik := store.ItemKey{Repo: rk, Kind: ItemKind}
	delete(e.store.items, ik) // the store lost the item (pruned, or the first write failed)
	e.store.rows[rk] = nil
	out, err := e.svc.run(context.Background(), key("a"))
	if err != nil || !out.Unchanged {
		t.Fatalf("%+v %v", out, err)
	}
	if _, ok := e.store.items[ik]; !ok || len(e.store.rows[rk]) != 3 {
		t.Fatalf("the served ring must be written again: item %v rows %d", ok, len(e.store.rows[rk]))
	}
}

// V-05: RecordFailure's error row of a repository that never had a ring is not an anomaly at restore.
func TestRestoreSkipsAnErrorRowWithoutValueQuietly(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	rec := &recMeter{counts: map[string]int{}}
	d := e.d
	d.Meter = rec
	svc, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	rk := store.RepoKey{Source: "s", Owner: "o", Name: "a"}
	it := store.Item{Key: store.ItemKey{Repo: rk, Kind: ItemKind}, State: freshness.StateError, Attempts: 2, Error: "unavailable"}
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return e.svc.Restore(context.Background(), b, "a", rk, it) }); err != nil {
		t.Fatal(err)
	}
	if _, ok := readmodel.Get(e.pub.Current(), "a", e.d.View); ok {
		t.Fatal("no view for a repository that never had a ring")
	}
	if rec.counts[MetricRestoreTotal] != 0 {
		t.Fatalf("restore_total counted %d: a repository that never had a ring is not an anomaly", rec.counts[MetricRestoreTotal])
	}
}

// recMeter counts Add calls per counter name; everything else is the no-op meter.
type recMeter struct {
	metricnoop.Meter
	counts map[string]int
}

func (m *recMeter) Int64Counter(name string, _ ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return recCounter{m: m, name: name}, nil
}

type recCounter struct {
	metricnoop.Int64Counter
	m    *recMeter
	name string
}

func (c recCounter) Add(_ context.Context, n int64, _ ...metric.AddOption) {
	c.m.counts[c.name] += int(n)
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	d := e.d
	d.Log, d.Meter, d.Lister = nil, nil, nil
	_, err := New(d)
	if err == nil || !strings.Contains(err.Error(), "Deps.Lister") || !strings.Contains(err.Error(), "Deps.Meter") || !strings.Contains(err.Error(), "Deps.Log") {
		t.Fatalf("got %v", err)
	}
}

// V-06: a repository filter in the display case of a link is resolved like the folded slug.
func TestRepoFilterIsCaseInsensitive(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, time.Now())}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	l, rec := getList(t, e, "/api/v1/commits?repo=A")
	if rec.Code != 200 || len(l.Items) != 2 {
		t.Fatalf("%d %d", rec.Code, len(l.Items))
	}
}

// V-07: the persisted row holds only the five documented keys and no e-mail address (GR-35).
func TestRowRecordHasNoEmailAndOnlyDocumentedKeys(t *testing.T) {
	rows, err := encodeRows([]scm.Commit{{SHA: sha(1), Subject: "s", AuthorLogin: "ada", AuthorName: "Ada L", URL: "https://github.com/o/r/commit/" + sha(1), CommittedAt: time.Now().UTC()}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(rows[0].Payload, &m); err != nil {
		t.Fatal(err)
	}
	for k := range m {
		if !slices.Contains([]string{"v", "s", "l", "n", "u"}, k) {
			t.Fatalf("unexpected key %q in the row payload", k)
		}
	}
	if strings.Contains(string(rows[0].Payload), "@") {
		t.Fatal("payload must hold no e-mail address")
	}
}

// V-08: the response of the generated handler for the error paths is an RFC 9457 problem with the fixed codes.
func TestGeneratedHandlerProblems(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, time.Now())}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	h := mounted(e)
	for path, want := range map[string]int{
		"/api/v1/commits?repo=nope":      404,
		"/api/v1/commits?group=nope":     404,
		"/api/v1/commits?limit=abc":      400, // binding error: no validator in this chain
		"/api/v1/commits?cursor=!!":      400,
		"/api/v1/commits?limit=2&repo=a": 200,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d want %d (%s)", path, rec.Code, want, rec.Body.String())
		}
	}
	// without httpapi.WithRequest the operation fails closed (500), never a panic
	mux := http.NewServeMux()
	e.svc.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/commits", nil))
	if rec.Code != 500 {
		t.Errorf("without WithRequest: %d", rec.Code)
	}
}

// V-11: a push webhook must not be swallowed by the head-sweep skip: the sweep value predates the push, so a run that
// follows a push to the default branch fetches even though the sweep still reports the old head (GR-21, G-03). A push that
// arrives while the fetch is in flight keeps the follow-up run honest too.
func TestPushBypassesTheHeadSweepSkip(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	now := time.Now().UTC()
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(3, 1, now), Validators: scm.Validators{SHA: sha(2)}}
	ctx := context.Background()
	if _, err := e.svc.run(ctx, key("a")); err != nil {
		t.Fatal(err)
	}
	setHead := func(h string) {
		t.Helper()
		if _, err := e.pub.Apply(func(b *readmodel.Builder) error {
			return readmodel.Put(b, "a", e.d.Stats, freshness.Value[scm.RepoStats]{V: scm.RepoStats{HeadSHA: h},
				Info: freshness.Info{State: freshness.StateOK, FetchedAt: time.Now(), TTL: 5 * time.Minute}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	setHead(sha(2)) // the sweep ran before the push and reports the head our ring was built from
	calls := e.lister.calls
	if _, err := e.svc.run(ctx, key("a")); err != nil || e.lister.calls != calls {
		t.Fatalf("without a push the sweep value is trusted: calls %d -> %d, %v", calls, e.lister.calls, err)
	}

	push := scm.Event{Kind: scm.EventPush, Repo: scm.RepoRef{Source: "s", Owner: "o", Name: "a"}, Ref: "refs/heads/main"}
	var enq recEnq
	if err := e.svc.React(ctx, push, &enq); err != nil || len(enq.keys) != 1 {
		t.Fatal(err)
	}
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(4, 0, now), Validators: scm.Validators{SHA: sha(1)}}
	if _, err := e.svc.run(ctx, enq.keys[0]); err != nil || e.lister.calls != calls+1 {
		t.Fatalf("after a push the run must fetch although the sweep is stale: calls %d, %v", e.lister.calls, err)
	}
	if v, _ := readmodel.Get(e.pub.Current(), "a", e.d.View); len(v.Latest) != 4 || v.Info.ContentSHA != sha(1) {
		t.Fatalf("ring after the push: %d commits, head %s", len(v.Latest), v.Info.ContentSHA)
	}
	// consumed: the next run (sweep still old) skips again
	if _, err := e.svc.run(ctx, enq.keys[0]); err != nil {
		t.Fatal(err)
	}

	// a push that arrives during the fetch is not consumed by that run: the follow-up run fetches again
	setHead(sha(1))
	if err := e.svc.React(ctx, push, &enq); err != nil {
		t.Fatal(err)
	}
	e.lister.onCall = func() { _ = e.svc.React(ctx, push, &enq) } // the second push lands while the lister is answering
	before := e.lister.calls
	if _, err := e.svc.run(ctx, enq.keys[0]); err != nil || e.lister.calls != before+1 {
		t.Fatalf("run with a marker must fetch: %d -> %d, %v", before, e.lister.calls, err)
	}
	e.lister.onCall = nil
	if _, err := e.svc.run(ctx, enq.keys[0]); err != nil || e.lister.calls != before+2 {
		t.Fatalf("the push that arrived during the fetch must cause one more fetch: %d, %v", e.lister.calls, err)
	}
	if _, err := e.svc.run(ctx, enq.keys[0]); err != nil || e.lister.calls != before+2 {
		t.Fatalf("and then the sweep value is trusted again: %d, %v", e.lister.calls, err)
	}
}

// V-12: the read side overview needs (15, R-09-1): n newest, a copy, the item's Info, false before the first view.
func TestRecentForOverview(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic), repo("b", scm.VisPublic))
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(7, 0, time.Now())}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	snap := e.pub.Current()
	cs, info, ok := e.svc.Recent(snap, "a", 5)
	if !ok || len(cs) != 5 || !info.HasValue() || cs[0].SHA != sha(1) {
		t.Fatalf("%d %v %+v", len(cs), ok, info)
	}
	cs[0].Subject = "mutated"
	if again, _, _ := e.svc.Recent(snap, "a", 5); again[0].Subject == "mutated" {
		t.Fatal("the ring must not be aliased")
	}
	if cs, _, ok := e.svc.Recent(snap, "a", 100); !ok || len(cs) != 7 {
		t.Fatalf("n beyond the ring: %d", len(cs))
	}
	if cs, _, ok := e.svc.Recent(snap, "a", -1); !ok || len(cs) != 0 {
		t.Fatalf("negative n: %d", len(cs))
	}
	if _, _, ok := e.svc.Recent(snap, "b", 5); ok {
		t.Fatal("no view yet: ok must be false")
	}
	if _, _, ok := e.svc.Recent(snap, "zzz", 5); ok {
		t.Fatal("unknown repository")
	}
}

// V-13: the planner restores the persisted NextDue of an item whose ring Restore skipped, so nothing would refetch it
// until then; the explicit pass after the warm start does.
func TestEnqueueUnrestoredCoversSkippedRings(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic), repo("b", scm.VisPublic))
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, time.Now())}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	var enq recEnq
	n, err := e.svc.EnqueueUnrestored(e.pub.Current(), &enq)
	if err != nil || n != 1 || len(enq.keys) != 1 || enq.keys[0] != key("b") || enq.prio[0] != job.PrioBackfill {
		t.Fatalf("n=%d err=%v keys=%v prio=%v", n, err, enq.keys, enq.prio)
	}
	full := job.EnqueueFunc(func(job.Key, job.Priority, time.Time, bool) error { return job.ErrQueueFull })
	if n, err := e.svc.EnqueueUnrestored(e.pub.Current(), full); n != 0 || !job.IsRefusal(err) {
		t.Fatalf("a refusal must stop the walk and be returned: %d %v", n, err)
	}
}
