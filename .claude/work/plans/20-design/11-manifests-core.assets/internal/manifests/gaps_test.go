package manifests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

func TestJobsSpecs(t *testing.T) {
	e, _ := apiEnv(t)
	specs := e.svc.Jobs()
	if len(specs) != 2 {
		t.Fatalf("%d specs", len(specs))
	}
	r, g := specs[0], specs[1]
	cfg := DefaultConfig()
	if r.Kind != KindRefresh || r.Upstream != "github" || r.Scope != job.ScopeRepo || r.Prio != job.PrioScheduled || r.Timeout != cfg.Refresh.JobTimeout ||
		r.Policy.TTL != cfg.Refresh.TTL || r.Policy.IdleBackoffMax != cfg.Refresh.IdleBackoffMax || r.Policy.Jitter != cfg.Refresh.Jitter || r.Run == nil {
		t.Fatalf("%+v", r)
	}
	if g.Kind != KindGraph || g.Upstream != "local" || g.Scope != job.ScopeGlobal || g.Policy.TTL != cfg.Graph.TTL || g.Run == nil || graphKey().Kind() != KindGraph {
		t.Fatalf("%+v", g)
	}
	if out, err := r.Run(context.Background(), rkey("core")); err != nil || !out.Unchanged { // the tree has not changed since apiEnv refreshed it
		t.Fatalf("spec Run is wired to runRefresh: %+v %v", out, err)
	}
	if out, err := g.Run(context.Background(), graphKey()); err != nil || !out.Unchanged {
		t.Fatalf("spec Run is wired to runGraph: %+v %v", out, err)
	}
}

func TestBindTwicePanics(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core"}) // newEnv already bound once
	defer func() {
		if recover() == nil {
			t.Fatal("second Bind must panic")
		}
	}()
	e.svc.Bind(e.enq)
}

func TestGuardUsesTheAllVariant(t *testing.T) {
	e, _ := apiEnv(t)
	ctx := context.Background()
	secret := "secret-lib"
	sha := gitSHA(secret)
	e.git.blobs[sha] = secret
	e.git.trees["secret"] = scm.NewTree(fakeOID("tree-"+sha), []scm.TreeEntry{{Path: "package.json", SHA: sha, Size: int64(len(secret))}}, false)
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error {
		ref := scm.RepoRef{Source: "github", Owner: "acme", Name: "secret"}
		if err := readmodel.Put(b, "secret", e.slots.Repos, freshness.Value[scm.Repository]{V: scm.Repository{Ref: ref, Slug: "secret", DefaultBranch: "main", Visibility: scm.VisPrivate}}); err != nil {
			return err
		}
		return readmodel.Put(b, "secret", e.slots.Stats, freshness.Value[scm.RepoStats]{V: scm.RepoStats{HeadSHA: "c"}})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.runRefresh(ctx, rkey("secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.runGraph(ctx, graphKey()); err != nil {
		t.Fatal(err)
	}
	g := e.svc.Guard()
	if g.Allow(ComponentID{NPM, "", "secret-lib"}) || !g.Allow(ComponentID{NPM, "", "left-pad"}) || !errors.Is(g.Check(ComponentID{NPM, "", "secret-lib"}), ErrPrivateCoordinate) {
		t.Fatal("a coordinate produced by a private repository must not leave the organisation")
	}
	if g.Allow(ComponentID{NPM, "", "core"}) { // produced by a PUBLIC repository, but an organisation artifact all the same
		t.Fatal("core is produced by an organisation unit")
	}
}

func TestRegistryRules(t *testing.T) {
	r := NewRegistry()
	ok := stubParser{id: "npm/a", eco: NPM, ver: 1, base: "a.json"}
	if err := r.Register(ok); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]Parser{
		"duplicate id":            ok,
		"id without ecosystem":    stubParser{id: "a", eco: NPM, ver: 1, base: "x"},
		"id of another ecosystem": stubParser{id: "maven/x", eco: NPM, ver: 1, base: "x"},
		"upper case id":           stubParser{id: "npm/A", eco: NPM, ver: 1, base: "x"},
		"version below 1":         stubParser{id: "npm/b", eco: NPM, ver: 0, base: "x"},
		"ecosystem without spec":  stubParser{id: "zzz/x", eco: "zzz", ver: 1, base: "x"},
	} {
		if err := r.Register(p); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := r.RegisterEcosystem(EcosystemSpec{ID: "zzz"}); err == nil {
		t.Error("incomplete ecosystem spec accepted")
	}
	if err := r.RegisterAssembler(joinAsm{eco: NPM}); err != nil {
		t.Fatal(err)
	}
	if r.RegisterAssembler(joinAsm{eco: NPM}) == nil || r.RegisterAssembler(joinAsm{eco: "zzz"}) == nil {
		t.Error("duplicate assembler or assembler without spec accepted")
	}
	if _, found := r.Parser("npm/a"); !found || !r.Interesting("pkg/a.json", nil) || r.Interesting("node_modules/a.json", map[string]bool{"node_modules": true}) {
		t.Fatal("lookup and path pre-filter")
	}
	r.Disable(NPM)
	if _, found := r.Parser("npm/a"); found || len(r.Match("a.json")) != 0 {
		t.Fatal("Disable must remove the ecosystem's parsers")
	}
	r.Freeze()
	h1 := r.SetHash()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("registration after Freeze must panic")
			}
		}()
		_ = r.Register(stubParser{id: "npm/c", eco: NPM, ver: 1, base: "c.json"})
	}()
	// the set hash covers parser versions, assembler versions and the model version
	mk := func(ver int, asm Assembler) string {
		x := NewRegistry()
		_ = x.Register(stubParser{id: "npm/a", eco: NPM, ver: ver, base: "a.json"})
		if asm != nil {
			_ = x.RegisterAssembler(asm)
		}
		x.Freeze()
		return x.SetHash()
	}
	if mk(1, nil) == mk(2, nil) || mk(1, nil) == mk(1, joinAsm{eco: NPM}) || mk(1, joinAsm{eco: NPM}) == mk(1, joinAsm{eco: NPM, ver: 2}) || mk(1, nil) != mk(1, nil) || h1 == "" {
		t.Fatal("SetHash must change with every parser or assembler version and be stable otherwise")
	}
}

type joinAsm struct {
	eco EcosystemID
	ver int
}

func (j joinAsm) Ecosystem() EcosystemID                                      { return j.eco }
func (j joinAsm) Assemble(context.Context, FileSet, []Parsed) ([]Unit, error) { return nil, nil }
func (j joinAsm) AssemblerVersion() int {
	if j.ver > 0 {
		return j.ver
	}
	return 1
}

type panicAsm struct{ err error }

func (panicAsm) Ecosystem() EcosystemID { return NPM }
func (a panicAsm) Assemble(context.Context, FileSet, []Parsed) ([]Unit, error) {
	if a.err != nil {
		return nil, a.err
	}
	panic("boom")
}

func TestRunAssemblerContainsPanicAndError(t *testing.T) {
	parsed := []Parsed{{File: FileRef{Path: "a/package.json"}, Provides: []Artifact{{ID: ComponentID{NPM, "", "a"}}}}}
	for name, asm := range map[string]Assembler{"panic": panicAsm{}, "error": panicAsm{err: errors.New("bad")}} {
		reg := NewRegistry()
		if err := reg.RegisterAssembler(asm); err != nil {
			t.Fatal(err)
		}
		reg.Freeze()
		units, diags := runAssembler(context.Background(), reg, NPM, parsed, PathSet{"a/package.json"})
		if len(units) != 1 || units[0].Dir != "a" || len(diags) != 1 || diags[0].Code != DiagAssembleFailed || diags[0].Severity != SevError {
			t.Errorf("%s: units=%d diags=%+v", name, len(units), diags)
		}
	}
}

func TestRebasePaths(t *testing.T) {
	mk := func(ns, name, ref string) Dependency {
		d := Dependency{Target: ComponentID{NPM, ns, name}}
		d.SetSource(SourcePath, ref)
		return d
	}
	p := Parsed{File: FileRef{Path: "services/api/package.json"}, Dependencies: []Dependency{
		mk("", "shared", "../shared"),                  // becomes services/shared
		mk("", "outside", "../../../elsewhere"),        // escapes: kept, location cleared, diagnostic
		mk(LocalNamespace, "../lib", "../lib"),         // _local, fine
		mk(LocalNamespace, "../../../x", "../../../x"), // _local that escapes: dropped
		{Target: ComponentID{NPM, "", "plain"}},        // registry dependency: untouched
	}}
	rebasePaths(&p)
	if len(p.Dependencies) != 4 || p.Dependencies[0].SourceRef() != "services/shared" || p.Dependencies[1].SourceRef() != "" || p.Dependencies[1].Source != SourcePath ||
		p.Dependencies[2].SourceRef() != "services/lib" {
		t.Fatalf("%+v", p.Dependencies)
	}
	if len(p.Diagnostics) != 2 || p.Diagnostics[0].Code != DiagPathEscape {
		t.Fatalf("%+v", p.Diagnostics)
	}
}

func parseOneEnv(t *testing.T) *env {
	e := newEnv(t, map[string]string{"core": "core"})
	e.svc.cfg.Limits.ParseTimeout = 30 * time.Millisecond
	e.svc.wd.Grace = 10 * time.Millisecond
	e.svc.wd.Trip = 2
	return e
}

func TestParseOneOutcomes(t *testing.T) {
	e := parseOneEnv(t)
	ctx := context.Background()
	cand := Candidate{Path: "package.json", SHA: gitSHA("x"), Size: 1}
	run := func(name string, fn func(File) (Parsed, error)) (Parsed, *parseErr, bool) {
		p := stubParser{id: "npm/" + name, eco: NPM, ver: 1, base: "package.json", fn: fn}
		cand.SHA = gitSHA(name)
		out, perr := e.svc.parseOne(ctx, p, cand, []byte("x"))
		_, cached, _ := e.st.GetCache(ctx, NSParse, ParseKey(cand.SHA, p.ID(), 1))
		return out, perr, cached
	}
	// a limit is a cached, deterministic failure
	if _, perr, cached := run("limit", func(File) (Parsed, error) { return Parsed{}, &limits.LimitError{Limit: limits.LimitNodes, Max: 5} }); perr == nil || perr.Code != "limit.nodes" || !cached {
		t.Errorf("limit: %+v cached=%v", perr, cached)
	}
	// a timeout depends on machine load: reported but never cached
	if _, perr, cached := run("timeout", func(File) (Parsed, error) { return Parsed{}, &limits.LimitError{Limit: limits.LimitTimeout, Max: 1} }); perr == nil || perr.Code != "limit.timeout" || cached {
		t.Errorf("timeout: %+v cached=%v", perr, cached)
	}
	if _, perr, cached := run("panic", func(File) (Parsed, error) { panic("kaboom") }); perr == nil || perr.Code != DiagParsePanic || !cached {
		t.Errorf("panic: %+v cached=%v", perr, cached)
	}
	if _, perr, cached := run("plain", func(File) (Parsed, error) { return Parsed{}, errors.New("line 3:\x00 bad‮ token") }); perr == nil || perr.Code != DiagParseFailed || strings.ContainsAny(perr.Msg, "\x00‮") || !cached {
		t.Errorf("plain: %+v cached=%v", perr, cached)
	}
	// the sanitiser repairs what a parser returned: control characters, invalid identities, out-of-range lines
	bad := func(File) (Parsed, error) {
		return Parsed{Dependencies: []Dependency{
			{Target: ComponentID{NPM, "", "ok‮"}, Requirement: "^1\x00", Line: 99},
			{Target: ComponentID{NPM, "", ""}},
		}}, nil
	}
	out, perr, _ := run("sanitize", bad)
	if perr != nil || len(out.Dependencies) != 1 || out.Dependencies[0].Target.Name != "ok" || out.Dependencies[0].Requirement != "^1" || out.Dependencies[0].Line != 0 {
		t.Errorf("sanitize: %+v %+v", out.Dependencies, perr)
	}
	// a parser that ignores its context is abandoned, not cached; after Trip abandonments its circuit opens
	release := make(chan struct{})
	hang := func(File) (Parsed, error) { <-release; return Parsed{}, nil }
	for i := 0; i < 2; i++ {
		if _, perr, cached := run("hang", hang); perr == nil || perr.Code != DiagParseAbandoned || cached {
			t.Fatalf("abandon %d: %+v cached=%v", i, perr, cached)
		}
	}
	if _, perr, cached := run("hang", hang); perr == nil || perr.Code != DiagCircuitOpen || cached {
		t.Fatalf("circuit: %+v cached=%v", perr, cached)
	}
	if !e.svc.wd.Open("npm/hang") || e.svc.wd.Budget.InUse() == 0 {
		t.Fatal("the abandoned parsers must still hold their memory weight")
	}
	drainCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	if err := e.svc.Drain(drainCtx); err == nil {
		t.Fatal("Drain must report goroutines that are still running")
	}
	cancel()
	close(release)
	if err := e.svc.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if e.svc.wd.Budget.InUse() != 0 {
		t.Fatalf("weight not released: %d", e.svc.wd.Budget.InUse())
	}
}

func bigDeps(n int) string {
	var b strings.Builder
	b.WriteString("big:")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "dependency-with-a-longish-name-%05d", i)
	}
	return b.String()
}

func TestOversizedFactsAreShrunkThenRejected(t *testing.T) {
	e := newEnv(t, map[string]string{"big": bigDeps(3000)})
	e.svc.cfg.Limits.MaxFactsBytes = 128 << 10
	if _, err := e.svc.runRefresh(context.Background(), rkey("big")); err != nil {
		t.Fatal(err)
	}
	f := e.facts(t, "big")
	n := len(f.V.Units[0].Dependencies)
	if n == 0 || n >= 3000 || !hasDiag(f.V.Diagnostics, DiagDepsCap) {
		t.Fatalf("deps=%d diags=%+v", n, f.V.Diagnostics)
	}
	e2 := newEnv(t, map[string]string{"big": bigDeps(3000)})
	e2.svc.cfg.Limits.MaxFactsBytes = 1 << 10 // impossible even after six halvings
	_, err := e2.svc.runRefresh(context.Background(), rkey("big"))
	if !job.IsPermanent(err) || !errors.Is(err, ErrFactsTooLarge) {
		t.Fatalf("err=%v", err)
	}
	if _, ok := readmodel.Get(e2.pub.Current(), "big", e2.slots.Facts); ok {
		t.Fatal("nothing may be published when the payload cannot be stored")
	}
}

func hasDiag(ds []Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

// An unchanged repository must not drift to "stale" (design 06 D15): the job republishes the slot with a new FetchedAt at most every
// refresh.republish_after, long before 1.5 x idle_backoff_max.
func TestUnchangedRepublishesBeforeStale(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, map[string]string{"core": "core"})
		ctx := context.Background()
		if _, err := e.svc.runRefresh(ctx, rkey("core")); err != nil {
			t.Fatal(err)
		}
		tree := e.facts(t, "core").V.TreeSHA
		sv, _ := readmodel.Get(e.pub.Current(), "core", e.slots.Stats)
		sv.V.HeadTreeOID = tree
		if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "core", e.slots.Stats, sv) }); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ { // five hours apart: the facts would be stale after six without the republish
			time.Sleep(5 * time.Hour)
			out, err := e.svc.runRefresh(ctx, rkey("core"))
			if err != nil || !out.Unchanged {
				t.Fatalf("round %d: %+v %v", i, out, err)
			}
			if st := e.facts(t, "core").Info.Derive(time.Now()); st != freshness.StatusFresh {
				t.Fatalf("round %d: derived status %s", i, st)
			}
		}
	})
}

func TestLicenceBucketsAreCapped(t *testing.T) {
	m := map[string]int{}
	for i := 0; i < 80; i++ {
		m[fmt.Sprintf("LIC-%02d", i)] = i + 1
	}
	out := capLicences(m)
	if len(out) != 50 || out["other"] == 0 || out["LIC-79"] != 80 {
		t.Fatalf("%d buckets, other=%d", len(out), out["other"])
	}
	sum := 0
	for _, n := range m {
		sum += n
	}
	got := 0
	for _, n := range out {
		got += n
	}
	if got != sum {
		t.Fatalf("occurrences lost: %d != %d", got, sum)
	}
	if small := map[string]int{"MIT": 3}; len(capLicences(small)) != 1 {
		t.Fatal("small maps pass through")
	}
}

func TestGroupMarkdownHasVersionSpread(t *testing.T) {
	e := newEnv(t, map[string]string{"a": "a:shared", "b": "b:shared"})
	ctx := context.Background()
	for _, n := range []string{"a", "b"} {
		if _, err := e.svc.runRefresh(ctx, rkey(n)); err != nil {
			t.Fatal(err)
		}
	}
	// give the two repositories different requirements of the shared component
	for n, req := range map[string]string{"a": "^1.0.0", "b": "^2.0.0"} {
		f := e.facts(t, n)
		u := f.V.Units[0]
		u.Dependencies = append([]Dependency(nil), u.Dependencies...)
		u.Dependencies[0].Requirement = req
		f.V.Units = []Unit{u}
		if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, readmodel.EntryID(n), e.slots.Facts, f) }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.runGraph(ctx, graphKey()); err != nil {
		t.Fatal(err)
	}
	g, _ := readmodel.GetGlobal(e.pub.Current(), e.slots.Graph)
	var sb strings.Builder
	repos := []MDRepo{{"a", e.facts(t, "a")}, {"b", e.facts(t, "b")}}
	if err := RenderDependenciesMD(&sb, "g", repos, g.All, 10, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "## Version spread") || !strings.Contains(sb.String(), "`npm:shared` | 2 | 2") {
		t.Fatalf("%s", sb.String())
	}
}
