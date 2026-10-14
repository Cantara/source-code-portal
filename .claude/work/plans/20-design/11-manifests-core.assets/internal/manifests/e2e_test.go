package manifests

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
	tnoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

func gitSHA(data string) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00%s", len(data), data)
	return hex.EncodeToString(h.Sum(nil))
}

type fakeGit struct {
	mu         sync.Mutex
	treeCalls  int
	blobCalls  int
	trees      map[string]scm.Tree // repo name -> tree
	blobs      map[string]string   // sha -> content
	treeErr    error
	byRef      map[string]scm.Tree // tree id -> tree, answers non-recursive reads (the directory walk addresses directories by tree id)
	calls      []scm.TreeOptions   // every ListTree option set, in order
	blobLiesTo map[string]string   // sha -> wrong content (integrity test)
	batchCalls int
	batchSkip  map[string]string
}

func (f *fakeGit) ListTree(ctx context.Context, r scm.RepoRef, ref string, o scm.TreeOptions) (scm.Result[scm.Tree], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.treeCalls++
	if f.treeErr != nil {
		return scm.Result[scm.Tree]{}, f.treeErr
	}
	f.calls = append(f.calls, o)
	if !o.Recursive && f.byRef != nil {
		t, ok := f.byRef[ref]
		if !ok {
			return scm.Result[scm.Tree]{}, scm.ErrNotFound
		}
		return scm.Result[scm.Tree]{Value: t}, nil
	}
	return scm.Result[scm.Tree]{Value: f.trees[r.Name]}, nil
}

func (f *fakeGit) ReadBlob(ctx context.Context, r scm.RepoRef, sha string, max int64) (scm.Blob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobCalls++
	if w, ok := f.blobLiesTo[sha]; ok {
		return scm.Blob{SHA: sha, Data: []byte(w)}, nil
	}
	c, ok := f.blobs[sha]
	if !ok {
		return scm.Blob{}, scm.ErrNotFound
	}
	return scm.Blob{SHA: sha, Data: []byte(c)}, nil
}

type fakeStore struct {
	mu     sync.Mutex
	items  map[store.ItemKey]store.Item
	cache  map[string][]byte
	reval  int
	failed []store.Failure
}

func newFakeStore() *fakeStore {
	return &fakeStore{items: map[store.ItemKey]store.Item{}, cache: map[string][]byte{}}
}
func (s *fakeStore) PutItem(_ context.Context, it store.Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[it.Key] = it
	return nil
}
func (s *fakeStore) Revalidated(context.Context, store.ItemKey, time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reval++
	return nil
}
func (s *fakeStore) RecordFailure(_ context.Context, _ store.ItemKey, f store.Failure) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = append(s.failed, f)
	return nil
}
func (s *fakeStore) GetCache(_ context.Context, ns, k string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.cache[ns+"|"+k]
	return b, ok, nil
}
func (s *fakeStore) PutCache(_ context.Context, ns, k string, d []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[ns+"|"+k] = d
	return nil
}

type fakeEnq struct {
	mu   sync.Mutex
	keys []job.Key
	prio []job.Priority
}

func (e *fakeEnq) Enqueue(k job.Key, p job.Priority, _ time.Time, _ bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keys = append(e.keys, k)
	e.prio = append(e.prio, p)
	return nil
}
func (e *fakeEnq) count(kind string) (n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, k := range e.keys {
		if k.Kind() == kind {
			n++
		}
	}
	return
}

type env struct {
	svc   *Service
	git   *fakeGit
	st    *fakeStore
	enq   *fakeEnq
	pub   *readmodel.Publisher
	slots Slots
}

// pkgParser parses a toy "name[: dep1,dep2]" package.json replacement so the pipeline can be exercised without the ecosystem packages.
func pkgParser() Parser {
	return stubParser{id: "npm/package-json", eco: NPM, ver: 1, base: "package.json", fn: func(f File) (Parsed, error) {
		var name, deps string
		for i, c := range f.Data {
			if c == ':' {
				name, deps = string(f.Data[:i]), string(f.Data[i+1:])
				break
			}
		}
		if name == "" {
			name = string(f.Data)
		}
		if name == "boom" {
			return Parsed{}, errors.New("syntax error at 1:1")
		}
		p := Parsed{Provides: []Artifact{{ID: ComponentID{NPM, "", name}, Version: "1.0.0", Kind: "library"}}}
		start := 0
		for i := 0; i <= len(deps); i++ {
			if i == len(deps) || deps[i] == ',' {
				if d := deps[start:i]; d != "" {
					p.Dependencies = append(p.Dependencies, Dependency{Target: ComponentID{NPM, "", d}, Requirement: "^1.0.0", Scope: ScopeRuntime, Source: SourceRegistry})
				}
				start = i + 1
			}
		}
		return p, nil
	}}
}

func newEnv(t *testing.T, repos map[string]string) *env { // repo name -> package.json content
	t.Helper()
	return newEnvReg(t, repos, newTestRegistry(t, pkgParser()))
}

func newEnvReg(t *testing.T, repos map[string]string, reg *Registry) *env {
	t.Helper()
	rr := &readmodel.Registry{}
	repoSlot := readmodel.Register[freshness.Value[scm.Repository]](rr, "catalog.repo", readmodel.Existence())
	memSlot := readmodel.RegisterGlobal[scm.Membership](rr, "catalog.membership")
	statSlot := readmodel.Register[freshness.Value[scm.RepoStats]](rr, "catalog.stats")
	factSlot, graphSlot := RegisterSlots(rr)
	pub := readmodel.NewPublisher(rr, nil, nil, readmodel.Deps{})
	git := &fakeGit{trees: map[string]scm.Tree{}, blobs: map[string]string{}}
	_, err := pub.Apply(func(b *readmodel.Builder) error {
		for name, content := range repos {
			sha := gitSHA(content)
			git.blobs[sha] = content
			git.trees[name] = scm.NewTree(fakeOID("tree-"+sha), []scm.TreeEntry{{Path: "package.json", SHA: sha, Size: int64(len(content))}, {Path: "README.md", SHA: "r1", Size: 3}}, false)
			ref := scm.RepoRef{Source: "github", Owner: "acme", Name: name}
			id := readmodel.EntryID(name)
			if err := readmodel.Put(b, id, repoSlot, freshness.Value[scm.Repository]{V: scm.Repository{Ref: ref, Slug: scm.Slug(name), DefaultBranch: "main", Visibility: scm.VisPublic, HTMLURL: "https://github.com/acme/" + name}}); err != nil {
				return err
			}
			if err := readmodel.Put(b, id, statSlot, freshness.Value[scm.RepoStats]{V: scm.RepoStats{HeadSHA: fakeOID("c-" + name)}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st := newFakeStore()
	svc, err := New(DefaultConfig(), Deps{Registry: reg, Trees: git, Blobs: git, Store: st, Publisher: pub,
		Slots: Slots{Repos: repoSlot, Stats: statSlot, Facts: factSlot, Graph: graphSlot, Membership: memSlot},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)), Meter: noop.NewMeterProvider().Meter("t"), Tracer: tnoop.NewTracerProvider().Tracer("t")})
	if err != nil {
		t.Fatal(err)
	}
	enq := &fakeEnq{}
	svc.Bind(enq)
	return &env{svc, git, st, enq, pub, svc.d.Slots}
}

func (e *env) facts(t *testing.T, name string) freshness.Value[RepoFacts] {
	t.Helper()
	v, ok := readmodel.Get(e.pub.Current(), readmodel.EntryID(name), e.slots.Facts)
	if !ok {
		t.Fatalf("no facts for %s", name)
	}
	return v
}
func rkey(name string) job.Key { return job.NewKey(KindRefresh, "github", "acme", name) }

func TestRefreshEndToEnd(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core", "app": "app:core,left-pad"})
	ctx := context.Background()
	if _, err := e.svc.runRefresh(ctx, rkey("core")); err != nil {
		t.Fatal(err)
	}
	if e.git.treeCalls != 1 || e.git.blobCalls != 1 {
		t.Fatalf("tree=%d blob=%d", e.git.treeCalls, e.git.blobCalls)
	}
	f := e.facts(t, "core")
	if f.Info.State != "ok" || len(f.V.Units) != 1 || f.V.Units[0].Provides[0].ID.Name != "core" || f.V.TreeSHA == "" {
		t.Fatalf("%+v", f)
	}
	if e.enq.count(KindGraph) != 1 {
		t.Fatal("graph job not enqueued")
	}
	if _, ok := e.st.items[store.ItemKey{Repo: store.RepoKey{Source: "github", Owner: "acme", Name: "core"}, Kind: ItemKind}]; !ok {
		t.Fatal("facts not persisted")
	}
	// same HEAD tree: no API call at all
	sv, _ := readmodel.Get(e.pub.Current(), "core", e.slots.Stats)
	sv.V.HeadTreeOID = f.V.TreeSHA
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "core", e.slots.Stats, sv) }); err != nil {
		t.Fatal(err)
	}
	t0, b0 := e.git.treeCalls, e.git.blobCalls
	out, err := e.svc.runRefresh(ctx, rkey("core"))
	if err != nil || !out.Unchanged || e.git.treeCalls != t0 || e.git.blobCalls != b0 {
		t.Fatalf("out=%+v err=%v calls=%d/%d", out, err, e.git.treeCalls-t0, e.git.blobCalls-b0)
	}
	if e.st.reval != 1 {
		t.Fatalf("revalidated=%d", e.st.reval)
	}
	// tree changed (README) but manifest blob identical: tree read, zero blob reads, parse cache hit
	sv.V.HeadTreeOID = fakeOID("tree-2")
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "core", e.slots.Stats, sv) }); err != nil {
		t.Fatal(err)
	}
	sha := gitSHA("core")
	e.git.trees["core"] = scm.NewTree(fakeOID("tree-2"), []scm.TreeEntry{{Path: "package.json", SHA: sha, Size: 4}, {Path: "README.md", SHA: "r2", Size: 3}}, false)
	b0 = e.git.blobCalls
	if _, err := e.svc.runRefresh(ctx, rkey("core")); err != nil {
		t.Fatal(err)
	}
	if e.git.blobCalls != b0 || e.facts(t, "core").V.TreeSHA != fakeOID("tree-2") {
		t.Fatalf("blob calls %d, tree %s", e.git.blobCalls-b0, e.facts(t, "core").V.TreeSHA)
	}
	// graph
	if _, err := e.svc.runRefresh(ctx, rkey("app")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.runGraph(ctx, graphKey()); err != nil {
		t.Fatal(err)
	}
	g, _ := readmodel.GetGlobal(e.pub.Current(), e.slots.Graph)
	if g.All == nil || g.All.Stats.Units != 2 || g.All.Stats.InternalEdges != 1 {
		t.Fatalf("%+v", g.All.Stats)
	}
	if out, _ := e.svc.runGraph(ctx, graphKey()); !out.Unchanged {
		t.Fatal("second graph run should be a no-op")
	}
	if err := e.svc.Drain(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshFailureModes(t *testing.T) {
	e := newEnv(t, map[string]string{"bad": "boom", "lie": "real"})
	ctx := context.Background()
	// deterministic parse failure becomes a diagnostic, the job succeeds
	if _, err := e.svc.runRefresh(ctx, rkey("bad")); err != nil {
		t.Fatal(err)
	}
	f := e.facts(t, "bad")
	if len(f.V.Units) != 0 || len(f.V.Diagnostics) == 0 || f.V.Diagnostics[0].Code != DiagParseFailed {
		t.Fatalf("%+v", f.V)
	}
	// failures are cached: second refresh at a new tree makes no blob call and no parser call
	e.git.trees["bad"] = scm.NewTree("t2", []scm.TreeEntry{{Path: "package.json", SHA: gitSHA("boom"), Size: 4}}, false)
	b0 := e.git.blobCalls
	if _, err := e.svc.runRefresh(ctx, rkey("bad")); err != nil || e.git.blobCalls != b0 {
		t.Fatalf("err=%v blobs=%d", err, e.git.blobCalls-b0)
	}
	// integrity: content not matching the tree sha is rejected with a diagnostic, never parsed
	e.git.blobLiesTo = map[string]string{gitSHA("real"): "evil"}
	if _, err := e.svc.runRefresh(ctx, rkey("lie")); err != nil {
		t.Fatal(err)
	}
	f = e.facts(t, "lie")
	if len(f.V.Units) != 0 || f.V.Diagnostics[0].Code != DiagSHAMismatch {
		t.Fatalf("%+v", f.V.Diagnostics)
	}
	// gone
	_, err := e.svc.runRefresh(ctx, rkey("nope"))
	if !job.IsGone(err) {
		t.Fatalf("err=%v", err)
	}
	// upstream failure keeps the previous facts (stale-while-error) and records the failure
	e.git.treeErr = scm.ErrUnavailable
	e.git.trees["bad"] = scm.NewTree("t3", nil, false)
	if _, err := e.svc.runRefresh(ctx, rkey("bad")); !errors.Is(err, scm.ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
	f = e.facts(t, "bad")
	if f.Info.Attempts != 1 || f.Info.ErrCode != "unavailable" || f.V.TreeSHA != "t2" || len(e.st.failed) != 1 {
		t.Fatalf("%+v failed=%d", f.Info, len(e.st.failed))
	}
	// empty repository is a state, not a failure
	e.git.treeErr = scm.ErrNotFound
	if _, err := e.svc.runRefresh(ctx, rkey("lie")); err != nil {
		t.Fatal(err)
	}
	if st := e.facts(t, "lie").Info.State; st != "no_content" {
		t.Fatalf("state %s", st)
	}
}

func TestTruncatedTreeWalk(t *testing.T) {
	e := newEnv(t, map[string]string{"mono": "root"})
	e.git.trees["mono"] = scm.NewTree("big", nil, true)
	e.git.blobs[gitSHA("svc")] = "svc"
	e.git.byRef = map[string]scm.Tree{ // keyed by tree id: the walk reads every directory by the id its parent entry carries
		"big":  scm.NewTree("big", []scm.TreeEntry{{Path: "package.json", SHA: gitSHA("root"), Size: 4}, {Path: "services", SHA: "tsvc", Type: scm.EntryTree}, {Path: "node_modules", SHA: "tnm", Type: scm.EntryTree}}, false),
		"tsvc": scm.NewTree("tsvc", []scm.TreeEntry{{Path: "api", SHA: "tapi", Type: scm.EntryTree}}, false),
		"tapi": scm.NewTree("tapi", []scm.TreeEntry{{Path: "package.json", SHA: gitSHA("svc"), Size: 3}}, false),
	}
	if _, err := e.svc.runRefresh(context.Background(), rkey("mono")); err != nil {
		t.Fatal(err)
	}
	f := e.facts(t, "mono")
	if !f.V.Truncated || len(f.V.Units) != 2 {
		t.Fatalf("%+v", f.V)
	}
	if e.git.treeCalls != 4 { // 1 recursive + root + services + services/api; node_modules pruned
		t.Fatalf("tree calls %d", e.git.treeCalls)
	}
}

func TestReactor(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core"})
	ctx := context.Background()
	ref := scm.RepoRef{Source: "github", Owner: "acme", Name: "core"}
	if _, err := e.svc.runRefresh(ctx, rkey("core")); err != nil {
		t.Fatal(err)
	}
	push := func(mod func(*scm.Event)) int {
		ev := scm.Event{Kind: scm.EventPush, Repo: ref, Ref: "refs/heads/main", PathsComplete: true, ChangedPaths: []string{"src/a.go"}, HeadTreeOID: "t9"}
		mod(&ev)
		before := e.enq.count(KindRefresh)
		if err := e.svc.React(ctx, ev, e.enq); err != nil {
			t.Fatal(err)
		}
		return e.enq.count(KindRefresh) - before
	}
	cases := []struct {
		name string
		mod  func(*scm.Event)
		want int
	}{
		{"unrelated paths", func(*scm.Event) {}, 0},
		{"manifest path", func(ev *scm.Event) { ev.ChangedPaths = []string{"pkg/package.json"} }, 1},
		{"non-default branch", func(ev *scm.Event) { ev.Ref = "refs/heads/dev"; ev.ChangedPaths = []string{"package.json"} }, 0},
		{"incomplete path list", func(ev *scm.Event) { ev.PathsComplete = false }, 1},
		{"forced", func(ev *scm.Event) { ev.Forced = true }, 1},
		{"created", func(ev *scm.Event) { ev.Created = true }, 1},
		{"deleted", func(ev *scm.Event) { ev.Deleted = true }, 0},
		{"unknown repo", func(ev *scm.Event) { ev.Repo.Name = "ghost"; ev.ChangedPaths = []string{"package.json"} }, 0},
	}
	for _, c := range cases {
		if got := push(c.mod); got != c.want {
			t.Errorf("%s: enqueued %d want %d", c.name, got, c.want)
		}
	}
	// head_changed: the clean push above recorded t9 as manifest-equivalent, so it is dropped; a new oid is enqueued
	hc := func(oid string) int {
		before := e.enq.count(KindRefresh)
		_ = e.svc.React(ctx, scm.Event{Kind: scm.EventHeadChanged, Repo: ref, HeadTreeOID: oid}, e.enq)
		return e.enq.count(KindRefresh) - before
	}
	if hc("t9") != 0 || hc("t10") != 1 {
		t.Fatal("head_changed rules")
	}
	if n := func() int {
		before := e.enq.count(KindRefresh)
		_ = e.svc.React(ctx, scm.Event{Kind: scm.EventRepository, Repo: ref, Action: "archived"}, e.enq)
		return e.enq.count(KindRefresh) - before
	}(); n != 0 {
		t.Fatal("archived repo must be ignored")
	}
}

func TestConcurrentRefreshGraphAndReads(t *testing.T) {
	repos := map[string]string{}
	for i := 0; i < 12; i++ {
		repos[fmt.Sprintf("r%02d", i)] = fmt.Sprintf("r%02d:r%02d,common", i, (i+1)%12)
	}
	e := newEnv(t, repos)
	ctx := context.Background()
	var wg sync.WaitGroup
	for n := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.runRefresh(ctx, rkey(n)); err != nil {
				t.Error(err)
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.svc.runGraph(ctx, graphKey())
			snap := e.pub.Current()
			for _, id := range snap.Repos() {
				_, _ = readmodel.Get(snap, id, e.slots.Facts)
			}
		}()
	}
	wg.Wait()
	if _, err := e.svc.runGraph(ctx, graphKey()); err != nil {
		t.Fatal(err)
	}
	g, _ := readmodel.GetGlobal(e.pub.Current(), e.slots.Graph)
	if g.All.Stats.Units != 12 || g.All.Stats.Cycles != 1 {
		t.Fatalf("%+v", g.All.Stats)
	}
}

func (f *fakeGit) ReadBlobs(ctx context.Context, r scm.RepoRef, shas []string, max int64) (map[string]scm.Blob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchCalls++
	out := map[string]scm.Blob{}
	for _, s := range shas {
		if c, ok := f.blobs[s]; ok && f.batchSkip[s] == "" {
			out[s] = scm.Blob{SHA: s, Data: []byte(c)}
		}
	}
	return out, nil
}

func TestBatchedFetchWithSingleFallback(t *testing.T) {
	e := newEnv(t, map[string]string{"mono": "root"})
	var entries []scm.TreeEntry
	for i := 0; i < 30; i++ {
		c := fmt.Sprintf("p%02d", i)
		e.git.blobs[gitSHA(c)] = c
		entries = append(entries, scm.TreeEntry{Path: fmt.Sprintf("m%02d/package.json", i), SHA: gitSHA(c), Size: 3})
	}
	e.git.batchSkip = map[string]string{gitSHA("p07"): "x"} // the batch omits one blob: it must be read singly
	e.git.trees["mono"] = scm.NewTree("t", entries, false)
	e.svc.d.BlobBatch = e.git
	if _, err := e.svc.runRefresh(context.Background(), rkey("mono")); err != nil {
		t.Fatal(err)
	}
	if e.git.batchCalls != 2 || e.git.blobCalls != 1 || len(e.facts(t, "mono").V.Units) != 30 {
		t.Fatalf("batch=%d single=%d units=%d", e.git.batchCalls, e.git.blobCalls, len(e.facts(t, "mono").V.Units))
	}
}

// fakeOID is a well-formed (40 hex) git object id derived from s, so fixtures satisfy the contract's Sha pattern.
func fakeOID(s string) string { return gitSHA("oid:" + s) }
