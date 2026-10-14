package commits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// ---- fakes -----------------------------------------------------------------------------------------------------

type fakeLister struct {
	calls   int
	lastVal scm.Validators
	res     map[string]scm.Result[[]scm.Commit]
	err     error
	onCall  func() // runs inside the call, before the result is returned (a push that arrives during the fetch)
}

func (f *fakeLister) LatestCommits(_ context.Context, r scm.RepoRef, _ string, _ int, v scm.Validators) (scm.Result[[]scm.Commit], error) {
	f.calls++
	f.lastVal = v
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return scm.Result[[]scm.Commit]{}, f.err
	}
	return f.res[r.Name], nil
}

// fakeStore mirrors the AppendCommits semantics of the real store: insert-or-ignore, then trim to keep newest.
type fakeStore struct {
	mu    sync.Mutex // the real store is safe for concurrent use
	rows  map[store.RepoKey][]store.CommitRow
	items map[store.ItemKey]store.Item
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[store.RepoKey][]store.CommitRow{}, items: map[store.ItemKey]store.Item{}}
}
func (f *fakeStore) AppendCommits(_ context.Context, k store.RepoKey, cs []store.CommitRow, keep int) error {
	if err := k.Validate(); err != nil { // the real store rejects what its contract rejects (03 section 5.3)
		return err
	}
	if keep <= 0 {
		return &store.InvalidError{Field: "keep", Reason: "must be > 0"}
	}
	for _, c := range cs {
		if err := store.ValidateCommit(c); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.rows[k]
	for _, c := range cs {
		if !slices.ContainsFunc(rows, func(r store.CommitRow) bool { return r.SHA == c.SHA }) {
			rows = append(rows, c)
		}
	}
	slices.SortStableFunc(rows, func(a, b store.CommitRow) int { return b.CommittedAt.Compare(a.CommittedAt) })
	if len(rows) > keep {
		rows = rows[:keep]
	}
	f.rows[k] = rows
	return nil
}
func (f *fakeStore) LoadCommits(_ context.Context, k store.RepoKey, limit int) ([]store.CommitRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[k]
	return r[:min(limit, len(r))], nil
}
func (f *fakeStore) PutItem(_ context.Context, it store.Item) error {
	if err := store.ValidateItem(it); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[it.Key] = it
	return nil
}
func (f *fakeStore) Revalidated(_ context.Context, k store.ItemKey, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[k]
	if !ok || it.FetchedAt.IsZero() || it.State == freshness.StateError { // 03: ErrNoItem (no row or no good value)
		return store.ErrNoItem
	}
	it.FetchedAt, it.CheckedAt = now, now
	it.Attempts, it.Error = 0, ""
	f.items[k] = it
	return nil
}
func (f *fakeStore) RecordFailure(_ context.Context, k store.ItemKey, fl store.Failure) error {
	if err := store.ValidateFailure(k, fl); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[k]
	if !ok {
		it = store.Item{Key: k, State: freshness.StateError} // 03: creates an error row when none exists
	}
	it.Attempts++
	it.Error = fl.Code
	f.items[k] = it
	return nil
}

type allowAll struct{ private bool }

func (a allowAll) CanView(_ context.Context, _ authz.Principal, v scm.Visibility) bool {
	return v == scm.VisPublic || a.private
}

type env struct {
	svc    *Service
	pub    *readmodel.Publisher
	lister *fakeLister
	store  *fakeStore
	d      Deps
}

func newEnv(t *testing.T, private bool, repos ...scm.Repository) *env {
	t.Helper()
	return newEnvCfg(t, DefaultConfig(), private, repos...)
}

func newEnvCfg(t *testing.T, cfg Config, private bool, repos ...scm.Repository) *env {
	t.Helper()
	reg := &readmodel.Registry{}
	d := Deps{Cfg: cfg, Log: slog.New(slog.DiscardHandler),
		Meter: noop.NewMeterProvider().Meter("t"), Tracer: tracenoop.NewTracerProvider().Tracer("t"), Authz: allowAll{private}}
	d.Repos = readmodel.Register[freshness.Value[scm.Repository]](reg, "catalog.repo", readmodel.Existence())
	d.Stats = readmodel.Register[freshness.Value[scm.RepoStats]](reg, "catalog.stats")
	d.Members = readmodel.RegisterGlobal[scm.Membership](reg, "catalog.membership")
	d.Aliases = readmodel.RegisterGlobal[map[scm.Slug]scm.Slug](reg, "catalog.aliases")
	d.View, d.Latest = RegisterSlots(reg)
	e := &env{lister: &fakeLister{res: map[string]scm.Result[[]scm.Commit]{}}, store: newFakeStore()}
	d.Lister, d.Store = e.lister, e.store
	// the projector and validator need only Slots and Config, so the publisher exists before the service
	proj := NewProjection(d.Cfg, d.Slots)
	d.Pub = readmodel.NewPublisher(reg, []readmodel.Projector{proj.Projector()}, []readmodel.Validator{proj.Validator()}, readmodel.Deps{})
	svc, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	e.svc, e.pub, e.d = svc, d.Pub, d
	var slugs []scm.Slug
	for _, r := range repos {
		slugs = append(slugs, r.Slug)
	}
	_, err = e.pub.Apply(func(b *readmodel.Builder) error {
		for _, r := range repos {
			if err := readmodel.Put(b, readmodel.EntryID(r.Slug), d.Repos, freshness.Value[scm.Repository]{V: r}); err != nil {
				return err
			}
		}
		if err := readmodel.PutGlobal(b, d.Aliases, map[scm.Slug]scm.Slug{"old-name": slugs[0]}); err != nil {
			return err
		}
		return readmodel.PutGlobal(b, d.Members, testMembership(t, repos, []scm.GroupID{"all", "g1"},
			map[scm.GroupID][]scm.Slug{"all": slugs, "g1": slugs[:1]}))
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func repo(name string, vis scm.Visibility) scm.Repository {
	return scm.Repository{Slug: scm.Slug(name), DefaultBranch: "main", Visibility: vis, HTMLURL: "https://github.com/o/" + name,
		Ref: scm.RepoRef{Source: "s", ID: scm.RepoID(idOf(name)), Owner: "o", Name: name}}
}

func commitList(n, off int, now time.Time) []scm.Commit {
	var cs []scm.Commit
	for i := 0; i < n; i++ {
		cs = append(cs, scm.Commit{SHA: sha(off + i + 1), Subject: fmt.Sprintf("c%d", off+i), CommittedAt: now.Add(-time.Duration(off+i) * time.Minute), URL: "https://github.com/o/r/commit/" + sha(off+i+1)})
	}
	return cs
}

func key(name string) job.Key { return job.NewKey(JobKind, "s", "o", name) }

// ---- tests -----------------------------------------------------------------------------------------------------

func TestRunChangedUnchangedAndSkip(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	now := time.Now().UTC()
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(5, 0, now), Validators: scm.Validators{SHA: sha(1)}}

	out, err := e.svc.run(context.Background(), key("a"))
	if err != nil || out.Unchanged {
		t.Fatalf("first run: %+v %v", out, err)
	}
	v, _ := readmodel.Get(e.pub.Current(), "a", e.d.View)
	if len(v.Latest) != 5 || v.Info.ContentSHA != sha(1) || v.Info.State != freshness.StateOK {
		t.Fatalf("view %+v", v.Info)
	}
	if len(e.store.rows[store.RepoKey{Source: "s", Owner: "o", Name: "a"}]) != 5 {
		t.Fatal("rows not persisted")
	}

	// same content again: Unchanged, ring not rewritten, validators passed
	out, err = e.svc.run(context.Background(), key("a"))
	if err != nil || !out.Unchanged || e.lister.lastVal.SHA != sha(1) {
		t.Fatalf("second run: %+v %v validators %+v", out, err, e.lister.lastVal)
	}

	// head sweep says head == our head and is fresh: no upstream call at all
	calls := e.lister.calls
	_, err = e.pub.Apply(func(b *readmodel.Builder) error {
		return readmodel.Put(b, "a", e.d.Stats, freshness.Value[scm.RepoStats]{
			V: scm.RepoStats{HeadSHA: sha(1)}, Info: freshness.Info{State: freshness.StateOK, FetchedAt: time.Now(), TTL: 5 * time.Minute}})
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err = e.svc.run(context.Background(), key("a"))
	if err != nil || !out.Unchanged || e.lister.calls != calls {
		t.Fatalf("skip: %+v %v calls %d->%d", out, err, calls, e.lister.calls)
	}
}

func TestRunFailureIsStaleWhileError(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	now := time.Now().UTC()
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(3, 0, now)}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	e.lister.err = fmt.Errorf("boom: %w", scm.ErrUnavailable)
	if _, err := e.svc.run(context.Background(), key("a")); err == nil {
		t.Fatal("want the error returned to the engine")
	}
	v, _ := readmodel.Get(e.pub.Current(), "a", e.d.View)
	if len(v.Latest) != 3 || v.Info.ErrCode != "unavailable" || v.Info.Attempts != 1 {
		t.Fatalf("ring must survive a failure: %d commits, %+v", len(v.Latest), v.Info)
	}
	if got := v.Info.Derive(time.Now()); got != freshness.StatusStale {
		t.Fatalf("status %v want stale", got)
	}
}

func TestRunGoneAndRateLimited(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	if _, err := e.svc.run(context.Background(), key("zzz")); !job.IsGone(err) {
		t.Fatalf("unknown repository must be Gone, got %v", err)
	}
	e.lister.err = rateLimited{}
	_, err := e.svc.run(context.Background(), key("a"))
	if _, ok := job.AsRateLimited(err); !ok {
		t.Fatalf("rate limit must pass through: %v", err)
	}
	if len(e.store.items) != 0 {
		t.Fatal("a rate limit must not be recorded as a data failure")
	}
}

type rateLimited struct{}

func (rateLimited) Error() string      { return "rate limited" }
func (rateLimited) RetryAt() time.Time { return time.Now().Add(time.Minute) }

func TestRestoreIgnoresOrphansAfterForcePush(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	now := time.Now().UTC()
	// history A (3 commits), then a force push replaces the two newest with rewritten ones
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(3, 5, now.Add(-time.Hour))}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: append(commitList(2, 0, now), commitList(3, 5, now.Add(-time.Hour))[2])}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	rk := store.RepoKey{Source: "s", Owner: "o", Name: "a"}
	if n := len(e.store.rows[rk]); n != 5 {
		t.Fatalf("union semantics expected 5 rows, got %d", n) // 3 old + 2 new: the store cannot delete
	}
	it := e.store.items[store.ItemKey{Repo: rk, Kind: ItemKind}]

	// restart: a fresh snapshot is rebuilt from the store
	e2 := newEnv(t, false, repo("a", scm.VisPublic))
	e2.store = e.store
	e2.svc.d.Store = e.store
	_, err := e2.pub.Apply(func(b *readmodel.Builder) error { return e2.svc.Restore(context.Background(), b, "a", rk, it) })
	if err != nil {
		t.Fatal(err)
	}
	v, _ := readmodel.Get(e2.pub.Current(), "a", e2.d.View)
	if len(v.Latest) != 3 || v.Latest[0].SHA != sha(1) {
		t.Fatalf("restored %d commits (want the 3 on the branch), first %s", len(v.Latest), v.Latest[0].SHA[36:])
	}
}

func TestRestoreIncompleteRingYieldsNoView(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	rk := store.RepoKey{Source: "s", Owner: "o", Name: "a"}
	ring, _ := encodeRing("main", sha(1), []scm.Commit{{SHA: sha(1)}, {SHA: sha(2)}})
	it := store.Item{Key: store.ItemKey{Repo: rk, Kind: ItemKind}, State: freshness.StateOK, FetchedAt: time.Now(), Payload: ring}
	rows, _ := encodeRows([]scm.Commit{{SHA: sha(1), CommittedAt: time.Now().UTC()}}) // second row lost in a crash
	_ = e.store.AppendCommits(context.Background(), rk, rows, 30)
	_, err := e.pub.Apply(func(b *readmodel.Builder) error { return e.svc.Restore(context.Background(), b, "a", rk, it) })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := readmodel.Get(e.pub.Current(), "a", e.d.View); ok {
		t.Fatal("an incomplete ring must not be served")
	}
}

type recEnq struct {
	keys []job.Key
	prio []job.Priority
}

func (r *recEnq) Enqueue(k job.Key, p job.Priority, _ time.Time, _ bool) error {
	r.keys, r.prio = append(r.keys, k), append(r.prio, p)
	return nil
}

func TestReact(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	ref := scm.RepoRef{Source: "s", Owner: "O", Name: "A"} // case differs: GitHub names are case-insensitive
	for _, tc := range []struct {
		name string
		ev   scm.Event
		want int
	}{
		{"push default branch", scm.Event{Kind: scm.EventPush, Repo: ref, Ref: "refs/heads/main"}, 1},
		{"push other branch", scm.Event{Kind: scm.EventPush, Repo: ref, Ref: "refs/heads/feature/x"}, 0},
		{"push tag", scm.Event{Kind: scm.EventPush, Repo: ref, Ref: "refs/tags/v1"}, 0},
		{"push deleted", scm.Event{Kind: scm.EventPush, Repo: ref, Ref: "refs/heads/main", Deleted: true}, 0},
		{"unknown repo", scm.Event{Kind: scm.EventPush, Repo: scm.RepoRef{Source: "s", Owner: "o", Name: "zzz"}, Ref: "refs/heads/main"}, 0},
		{"head changed", scm.Event{Kind: scm.EventHeadChanged, Repo: ref}, 1},
		{"release ignored", scm.Event{Kind: scm.EventRelease, Repo: ref}, 0},
		{"ping ignored", scm.Event{Kind: scm.EventPing}, 0},
	} {
		var enq recEnq
		if err := e.svc.React(context.Background(), tc.ev, &enq); err != nil || len(enq.keys) != tc.want {
			t.Errorf("%s: %d enqueues (want %d), err %v", tc.name, len(enq.keys), tc.want, err)
		}
	}
}

func TestProjectorViewClassesAndValidator(t *testing.T) {
	e := newEnv(t, false, repo("pub", scm.VisPublic), repo("priv", scm.VisPrivate))
	now := time.Now().UTC()
	e.lister.res["pub"] = scm.Result[[]scm.Commit]{Value: commitList(2, 10, now)}
	e.lister.res["priv"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, now)} // newer than pub
	for _, n := range []string{"pub", "priv"} {
		if _, err := e.svc.run(context.Background(), key(n)); err != nil {
			t.Fatal(err)
		}
	}
	lat, _ := readmodel.GetGlobal(e.pub.Current(), e.d.Latest)
	if len(lat.Public.All) != 2 || lat.Public.All[0].Slug != "pub" {
		t.Fatalf("public variant leaks: %+v", lat.Public.All)
	}
	if len(lat.All.All) != 4 || lat.All.All[0].Slug != "priv" {
		t.Fatalf("all variant: %+v", lat.All.All)
	}
	if lat.Public.AllInfo.Repos != 1 || lat.All.AllInfo.Repos != 2 {
		t.Fatalf("feed info counts must follow the class: %+v %+v", lat.Public.AllInfo, lat.All.AllInfo)
	}
}

// mounted is the route as cmd/scp serves it: the generated strict handler behind httpapi.WithRequest.
func mounted(e *env) http.Handler {
	mux := http.NewServeMux()
	e.svc.Mount(mux)
	return httpapi.WithRequest(mux)
}

func getList(t *testing.T, e *env, path string) (commitPage, *httptest.ResponseRecorder) {
	t.Helper()
	mux := mounted(e)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	var l commitPage
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
	}
	return l, rec
}

func TestHandlerFastPathEqualsMergePathAndPaging(t *testing.T) {
	var rs []scm.Repository
	for i := 0; i < 6; i++ {
		rs = append(rs, repo(fmt.Sprintf("r%d", i), scm.VisPublic))
	}
	e := newEnv(t, false, rs...)
	now := time.Now().UTC().Truncate(time.Second)
	for i, r := range rs {
		e.lister.res[string(r.Slug)] = scm.Result[[]scm.Commit]{Value: commitList(8, i*3, now)}
		if _, err := e.svc.run(context.Background(), key(string(r.Slug))); err != nil {
			t.Fatal(err)
		}
	}
	// LatestN=50 > 48 commits: the feed is complete, so the first page is served from the projection
	fast, rec := getList(t, e, "/api/v1/commits?limit=10")
	if !strings.HasPrefix(rec.Header().Get("ETag"), `W/"g`) { // Cache-Control is added by httpapi.APIDefaults, not here
		t.Fatalf("headers %v", rec.Header())
	}
	// force the merge path by adding a cursor-less request on a service whose LatestN is tiny
	e.svc.d.Cfg.LatestN = 3
	merged, _ := getList(t, e, "/api/v1/commits?limit=10")
	if len(fast.Items) != 10 || !slices.EqualFunc(fast.Items, merged.Items, func(a, b openapi.Commit) bool { return a.Sha == b.Sha && a.Repo == b.Repo }) {
		t.Fatalf("fast and merge paths differ:\n%v\n%v", shas(fast), shas(merged))
	}
	// page through everything
	var all []openapi.Commit
	path := "/api/v1/commits?limit=7"
	for i := 0; i < 20; i++ {
		l, _ := getList(t, e, path)
		all = append(all, l.Items...)
		if l.Page.NextCursor == nil {
			break
		}
		path = "/api/v1/commits?limit=7&cursor=" + *l.Page.NextCursor
	}
	if len(all) != 48 {
		t.Fatalf("paged %d items, want 48", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].CommittedAt.Before(all[i].CommittedAt) {
			t.Fatalf("not newest-first at %d", i)
		}
	}
	// errors
	for path, want := range map[string]int{
		"/api/v1/commits?repo=r1&group=nope": 404,
		"/api/v1/commits?limit=0":            400,
		"/api/v1/commits?limit=999":          400,
		"/api/v1/commits?cursor=garbage":     400,
		"/api/v1/commits?repo=nope":          404,
		"/api/v1/commits?group=nope":         404,
	} {
		if _, rec := getList(t, e, path); rec.Code != want {
			t.Errorf("%s: %d want %d", path, rec.Code, want)
		}
	}
	// conditional GET
	_, rec = getList(t, e, "/api/v1/commits?limit=7")
	req := httptest.NewRequest("GET", "/api/v1/commits?limit=7", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec2 := httptest.NewRecorder()
	mux := mounted(e)
	mux.ServeHTTP(rec2, req)
	if rec2.Code != 304 {
		t.Fatalf("conditional: %d", rec2.Code)
	}
}

func (s *Service) _unused() {}

func shas(l commitPage) []string {
	var o []string
	for _, i := range l.Items {
		o = append(o, i.Repo+"/"+i.Sha[36:])
	}
	return o
}

func TestHandlerHidesPrivateRepoFromAnonymous(t *testing.T) {
	e := newEnv(t, false, repo("pub", scm.VisPublic), repo("priv", scm.VisPrivate))
	now := time.Now().UTC()
	e.lister.res["pub"] = scm.Result[[]scm.Commit]{Value: commitList(2, 10, now)}
	e.lister.res["priv"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, now)}
	for _, n := range []string{"pub", "priv"} {
		_, _ = e.svc.run(context.Background(), key(n))
	}
	l, _ := getList(t, e, "/api/v1/commits")
	for _, it := range l.Items {
		if it.Repo == "priv" {
			t.Fatal("private repository visible to an anonymous principal")
		}
	}
	if _, rec := getList(t, e, "/api/v1/commits?repo=priv"); rec.Code != 404 {
		t.Fatalf("private repo filter: %d", rec.Code)
	}
}

var _ = errors.New

func idOf(name string) int {
	h := 7
	for _, c := range name {
		h = h*31 + int(c)
	}
	return h
}

func TestHandlerFiltersCombineAndAliases(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic), repo("b", scm.VisPublic))
	now := time.Now().UTC()
	for _, n := range []string{"a", "b"} {
		e.lister.res[n] = scm.Result[[]scm.Commit]{Value: commitList(3, int(n[0]), now)}
		if _, err := e.svc.run(context.Background(), key(n)); err != nil {
			t.Fatal(err)
		}
	}
	// group g1 holds only "a": repo=b with group=g1 is an empty page, not an error; repo=a returns a's commits
	if l, rec := getList(t, e, "/api/v1/commits?repo=b&group=g1"); rec.Code != 200 || len(l.Items) != 0 {
		t.Fatalf("non-member: %d %d", rec.Code, len(l.Items))
	}
	if l, _ := getList(t, e, "/api/v1/commits?repo=a&group=g1"); len(l.Items) != 3 {
		t.Fatalf("member: %d", len(l.Items))
	}
	// a renamed repository's old slug resolves silently to the current one
	if l, rec := getList(t, e, "/api/v1/commits?repo=old-name"); rec.Code != 200 || len(l.Items) != 3 || l.Items[0].Repo != "a" {
		t.Fatalf("alias: %d %+v", rec.Code, l.Items)
	}
}

// commitPage is the body of listCommits (generated as openapi.CommitPage in the real tree).
type commitPage struct {
	Items     []openapi.Commit  `json:"items"`
	Page      openapi.PageInfo  `json:"page"`
	Freshness openapi.Freshness `json:"freshness"`
}

func TestHandlerRejectsBadLimitAndForeignCursor(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic), repo("b", scm.VisPublic))
	now := time.Now().UTC()
	for _, n := range []string{"a", "b"} {
		e.lister.res[n] = scm.Result[[]scm.Commit]{Value: commitList(4, int(n[0]), now)}
		if _, err := e.svc.run(context.Background(), key(n)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"limit=0", "limit=-3", "limit=201", "cursor=!!!"} {
		if _, rec := getList(t, e, "/api/v1/commits?"+q); rec.Code != 400 {
			t.Errorf("%s: %d want 400", q, rec.Code)
		}
	}
	first, _ := getList(t, e, "/api/v1/commits?limit=2")
	if first.Page.NextCursor == nil {
		t.Fatal("no next cursor")
	}
	c := *first.Page.NextCursor
	if _, rec := getList(t, e, "/api/v1/commits?limit=2&cursor="+c); rec.Code != 200 {
		t.Errorf("own cursor: %d", rec.Code)
	}
	// a cursor is bound to the filter it was issued for (06-httpapi T12)
	for _, q := range []string{"repo=a&", "group=g1&"} {
		if _, rec := getList(t, e, "/api/v1/commits?"+q+"limit=2&cursor="+c); rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid-cursor") {
			t.Errorf("%s foreign cursor: %d %s", q, rec.Code, rec.Body.String())
		}
	}
}

// The derived freshness status ages without a new snapshot, so it is part of the ETag (06-httpapi D15).
func TestETagFollowsDerivedStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, false, repo("a", scm.VisPublic))
		e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(3, 0, time.Now())}
		if _, err := e.svc.run(context.Background(), key("a")); err != nil {
			t.Fatal(err)
		}
		l1, r1 := getList(t, e, "/api/v1/commits")
		if l1.Freshness.Status != "fresh" {
			t.Fatalf("status %s", l1.Freshness.Status)
		}
		etag := r1.Header().Get("ETag")
		mux := mounted(e)
		cond := func() int {
			req := httptest.NewRequest("GET", "/api/v1/commits", nil)
			req.Header.Set("If-None-Match", etag)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			return rec.Code
		}
		time.Sleep(2 * time.Hour) // within 1.5 x TTL (3h): unchanged
		if c := cond(); c != 304 {
			t.Fatalf("within the TTL: %d want 304", c)
		}
		time.Sleep(2 * time.Hour) // 4h > 3h: the same snapshot now answers "stale"
		if c := cond(); c != 200 {
			t.Fatalf("after 1.5 TTL: %d want 200 (new status in the body)", c)
		}
		l2, r2 := getList(t, e, "/api/v1/commits")
		if l2.Freshness.Status != "stale" || r2.Header().Get("ETag") == etag || l2.Freshness.StaleSince == nil {
			t.Fatalf("%+v %s", l2.Freshness, r2.Header().Get("ETag"))
		}
	})
}

func TestRestoreOfAFailingRepositoryIsStale(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	now := time.Now().UTC()
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, now)}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	e.lister.err = fmt.Errorf("x: %w", scm.ErrUnavailable)
	_, _ = e.svc.run(context.Background(), key("a"))
	rk := store.RepoKey{Source: "s", Owner: "o", Name: "a"}
	it := e.store.items[store.ItemKey{Repo: rk, Kind: ItemKind}]
	if it.State != freshness.StateOK || it.Attempts != 1 || it.Error != "unavailable" {
		t.Fatalf("stored item: %+v", it) // the store keeps the status of a row that has a value
	}
	e2 := newEnv(t, false, repo("a", scm.VisPublic))
	e2.svc.d.Store = e.store
	if _, err := e2.pub.Apply(func(b *readmodel.Builder) error { return e2.svc.Restore(context.Background(), b, "a", rk, it) }); err != nil {
		t.Fatal(err)
	}
	v, _ := readmodel.Get(e2.pub.Current(), "a", e2.d.View)
	if len(v.Latest) != 2 || v.Info.State != freshness.StateError || v.Info.ErrCode != "unavailable" || v.Info.Derive(time.Now()) != freshness.StatusStale {
		t.Fatalf("restored %d commits, %+v", len(v.Latest), v.Info)
	}
	if have, total := e2.svc.Coverage(e2.pub.Current()); have != 1 || total != 1 {
		t.Fatalf("coverage %d/%d: a failing repository that serves its ring counts", have, total)
	}
}

func TestJobSpecIsValidForTheEngine(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	specs := e.svc.Jobs()
	if len(specs) != 1 {
		t.Fatalf("%d specs", len(specs))
	}
	if err := specs[0].Validate(); err != nil {
		t.Fatal(err)
	}
	if specs[0].Kind != JobKind || specs[0].Scope != job.ScopeRepo || specs[0].Upstream != "github" {
		t.Fatalf("%+v", specs[0])
	}
}

// The precomputed feeds hold LatestN+1 refs, so a first page of exactly LatestN entries knows whether more exist.
func TestFastPathBoundaryAtLatestN(t *testing.T) {
	for _, total := range []int{4, 5, 6} { // LatestN is 5
		cfg := DefaultConfig()
		cfg.LatestN = 5
		e := newEnvCfg(t, cfg, false, repo("a", scm.VisPublic), repo("b", scm.VisPublic))
		now := time.Now().UTC()
		e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList((total+1)/2, 0, now)}
		e.lister.res["b"] = scm.Result[[]scm.Commit]{Value: commitList(total/2, 100, now)}
		for _, n := range []string{"a", "b"} {
			if _, err := e.svc.run(context.Background(), key(n)); err != nil {
				t.Fatal(err)
			}
		}
		for _, limit := range []int{4, 5, 6} {
			path := fmt.Sprintf("/api/v1/commits?limit=%d", limit)
			got, _ := getList(t, e, path) // limit <= 5: precomputed feed, otherwise merge
			e.svc.d.Cfg.LatestN = 1       // force the merge path for the reference answer
			want, _ := getList(t, e, path)
			e.svc.d.Cfg.LatestN = 5
			if !slices.EqualFunc(got.Items, want.Items, func(a, b openapi.Commit) bool { return a.Sha == b.Sha }) ||
				(got.Page.NextCursor == nil) != (want.Page.NextCursor == nil) {
				t.Errorf("total %d limit %d: fast %v more=%v, merge %v more=%v", total, limit, shas(got), got.Page.NextCursor != nil, shas(want), want.Page.NextCursor != nil)
			}
			if wantMore := total > limit; (got.Page.NextCursor != nil) != wantMore {
				t.Errorf("total %d limit %d: nextCursor present=%v want %v", total, limit, got.Page.NextCursor != nil, wantMore)
			}
		}
	}
}

// Jobs of different repositories publish while readers page through the log (run with -race): every page must be
// internally consistent (sorted, no duplicate, all items resolvable) whatever snapshot it was built from.
func TestConcurrentJobsAndReaders(t *testing.T) {
	var rs []scm.Repository
	for i := 0; i < 8; i++ {
		rs = append(rs, repo(fmt.Sprintf("r%d", i), scm.VisPublic))
	}
	e := newEnv(t, false, rs...)
	var mu sync.Mutex
	now := time.Now().UTC()
	for i, r := range rs {
		e.lister.res[string(r.Slug)] = scm.Result[[]scm.Commit]{Value: commitList(10, i*2, now)}
	}
	lister := &lockedLister{inner: e.lister, mu: &mu}
	e.svc.d.Lister = lister
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mux := mounted(e)
			for {
				select {
				case <-stop:
					return
				default:
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/commits?limit=7", nil))
				if rec.Code != 200 && rec.Code != 503 {
					t.Errorf("status %d", rec.Code)
					return
				}
				if rec.Code == 200 {
					var p commitPage
					if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
						t.Error(err)
						return
					}
					for i := 1; i < len(p.Items); i++ {
						a, b := p.Items[i-1], p.Items[i]
						if a.CommittedAt.Before(b.CommittedAt) || (a.CommittedAt.Equal(b.CommittedAt) && (a.Repo > b.Repo || (a.Repo == b.Repo && a.Sha <= b.Sha))) {
							t.Errorf("page out of order at %d", i)
							return
						}
					}
				}
			}
		}()
	}
	var jobs sync.WaitGroup
	for round := 0; round < 5; round++ {
		for _, r := range rs {
			jobs.Add(1)
			go func() {
				defer jobs.Done()
				_, _ = e.svc.run(context.Background(), key(string(r.Slug)))
			}()
		}
		jobs.Wait()
	}
	close(stop)
	wg.Wait()
	if have, total := e.svc.Coverage(e.pub.Current()); have != 8 || total != 8 {
		t.Fatalf("coverage %d/%d", have, total)
	}
}

type lockedLister struct {
	inner *fakeLister
	mu    *sync.Mutex
}

func (l *lockedLister) LatestCommits(ctx context.Context, r scm.RepoRef, ref string, n int, v scm.Validators) (scm.Result[[]scm.Commit], error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.LatestCommits(ctx, r, ref, n, v)
}

// The validator gates publication: a feed that names a commit the repository's ring does not hold is rejected and the
// previous snapshot stays current.
func TestValidatorRejectsDanglingRef(t *testing.T) {
	e := newEnv(t, false, repo("a", scm.VisPublic))
	e.lister.res["a"] = scm.Result[[]scm.Commit]{Value: commitList(2, 0, time.Now().UTC())}
	if _, err := e.svc.run(context.Background(), key("a")); err != nil {
		t.Fatal(err)
	}
	// a publisher with the validator but without the projector, so a hand-made global survives to validation
	reg := &readmodel.Registry{}
	var s Slots
	s.Repos = readmodel.Register[freshness.Value[scm.Repository]](reg, "catalog.repo", readmodel.Existence())
	s.View, s.Latest = RegisterSlots(reg)
	pub := readmodel.NewPublisher(reg, nil, []readmodel.Validator{NewProjection(DefaultConfig(), s).Validator()}, readmodel.Deps{})
	apply := func(sha string) error {
		_, err := pub.Apply(func(b *readmodel.Builder) error {
			if err := readmodel.Put(b, "a", s.Repos, freshness.Value[scm.Repository]{V: repo("a", scm.VisPublic)}); err != nil {
				return err
			}
			if err := readmodel.Put(b, "a", s.View, View{Latest: commitList(2, 0, time.Now().UTC())}); err != nil {
				return err
			}
			l := Latest{All: []CommitRef{{Slug: "a", SHA: sha}}}
			return readmodel.PutGlobal(b, s.Latest, scm.Classed[Latest]{Public: l, All: l})
		})
		return err
	}
	if err := apply(sha(1)); err != nil {
		t.Fatalf("valid ref rejected: %v", err)
	}
	err := apply(sha(99))
	if !errors.Is(err, readmodel.ErrInvalidSnapshot) || !job.IsPermanent(err) {
		t.Fatalf("dangling ref accepted or not permanent: %v", err)
	}
	if pub.Current().Version() != 1 {
		t.Fatalf("a rejected candidate must not be published: version %d", pub.Current().Version())
	}
}
