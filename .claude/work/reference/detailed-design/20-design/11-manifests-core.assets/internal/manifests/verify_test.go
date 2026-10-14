package manifests

// Regression tests added by the independent verification of unit 11 (30-verification/design-11-manifests-core.md; the VF-nn ids are those of that report).
// Each test failed on the first revision of the reference implementation.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/manifests/manifestsapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// VF-01: a webhook-triggered refresh must not be short-circuited by the catalogue's stale HeadTreeOID (the sweep has not run yet).
func TestPushRefreshBypassesStaleHeadTreeOID(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core"})
	ctx := context.Background()
	if _, err := e.svc.runRefresh(ctx, rkey("core")); err != nil {
		t.Fatal(err)
	}
	old := e.facts(t, "core").V.TreeSHA
	sv, _ := readmodel.Get(e.pub.Current(), "core", e.slots.Stats)
	sv.V.HeadTreeOID = old // the last sweep saw the old tree
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "core", e.slots.Stats, sv) }); err != nil {
		t.Fatal(err)
	}
	sha := gitSHA("core:left-pad")
	e.git.blobs[sha] = "core:left-pad"
	e.git.trees["core"] = scm.NewTree(fakeOID("tree-new"), []scm.TreeEntry{{Path: "package.json", SHA: sha, Size: 13}}, false)
	ref := scm.RepoRef{Source: "github", Owner: "acme", Name: "core"}
	ev := scm.Event{Kind: scm.EventPush, Repo: ref, Ref: "refs/heads/main", PathsComplete: true, ChangedPaths: []string{"package.json"}}
	if err := e.svc.React(ctx, ev, e.enq); err != nil {
		t.Fatal(err)
	}
	if out, err := e.svc.runRefresh(ctx, rkey("core")); err != nil || out.Unchanged {
		t.Fatalf("the push-triggered refresh was skipped: %+v %v", out, err)
	}
	if got := e.facts(t, "core").V.TreeSHA; got != fakeOID("tree-new") {
		t.Fatalf("facts still describe %s", got)
	}
	// the mark is consumed: the next ordinary run is cheap again
	sv.V.HeadTreeOID = fakeOID("tree-new")
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "core", e.slots.Stats, sv) }); err != nil {
		t.Fatal(err)
	}
	t0 := e.git.treeCalls
	if out, _ := e.svc.runRefresh(ctx, rkey("core")); !out.Unchanged || e.git.treeCalls != t0 {
		t.Fatal("the dirty mark must be consumed by one run")
	}
}

// VF-02: getRepoManifests carries graph-derived counts, so its validator must change when only the graph changes.
func TestETagCoversGraphDerivedCounts(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core", "app": "app:core"})
	ctx := context.Background()
	for _, n := range []string{"core", "app"} {
		if _, err := e.svc.runRefresh(ctx, rkey(n)); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	e.svc.Mount(mux)
	first := get(t, mux, "/api/v1/repos/app/manifests") // no graph published yet: internal count 0
	if m := decode[manifestsapi.RepoManifests](t, first); m.Counts.InternalDependencies != 0 {
		t.Fatalf("%+v", m.Counts)
	}
	if _, err := e.svc.runGraph(ctx, graphKey()); err != nil {
		t.Fatal(err)
	}
	again := get(t, mux, "/api/v1/repos/app/manifests", "If-None-Match", first.Header().Get("ETag"))
	if again.Code != 200 {
		t.Fatalf("a body whose internal count changed from 0 to 1 was answered %d with the old validator", again.Code)
	}
	if m := decode[manifestsapi.RepoManifests](t, again); m.Counts.InternalDependencies != 1 {
		t.Fatalf("%+v", m.Counts)
	}
}

// VF-03: the SBOM is a function of the tree AND of the parser set (components, scp:parserSet), so a parser bump must change its validator.
func TestSBOMETagCoversParserSet(t *testing.T) {
	e, mux := apiEnv(t)
	first := get(t, mux, "/api/v1/repos/app/sbom.cdx.json")
	if first.Code != 200 || first.Header().Get("ETag") == "" {
		t.Fatalf("%d", first.Code)
	}
	f := e.facts(t, "app")
	f.V.ParserSet = "other-parser-set"
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "app", e.slots.Facts, f) }); err != nil {
		t.Fatal(err)
	}
	if again := get(t, mux, "/api/v1/repos/app/sbom.cdx.json", "If-None-Match", first.Header().Get("ETag")); again.Code != 200 {
		t.Fatalf("same tree, different parser set, answered %d", again.Code)
	}
}

// VF-04: without an Authorizer the service fails closed (public view), it never serves a private repository.
func TestNilAuthzFailsClosed(t *testing.T) {
	e, _ := apiEnv(t)
	e.svc.d.Authz = nil
	ctx := context.Background()
	secret := "secret:core"
	sha := gitSHA(secret)
	e.git.blobs[sha] = secret
	e.git.trees["secret"] = scm.NewTree(fakeOID("tree-"+sha), []scm.TreeEntry{{Path: "package.json", SHA: sha, Size: int64(len(secret))}}, false)
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error {
		ref := scm.RepoRef{Source: "github", Owner: "acme", Name: "secret"}
		if err := readmodel.Put(b, "secret", e.slots.Repos, freshness.Value[scm.Repository]{V: scm.Repository{Ref: ref, Slug: "secret", DefaultBranch: "main", Visibility: scm.VisPrivate}}); err != nil {
			return err
		}
		return readmodel.Put(b, "secret", e.slots.Stats, freshness.Value[scm.RepoStats]{V: scm.RepoStats{HeadSHA: fakeOID("c")}})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.runRefresh(ctx, rkey("secret")); err != nil {
		t.Fatal(err)
	}
	svc2, err := New(DefaultConfig(), Deps{Registry: e.svc.d.Registry, Trees: e.git, Blobs: e.git, Store: e.st, Publisher: e.pub, Slots: e.slots,
		Log: e.svc.d.Log, Meter: e.svc.d.Meter, Tracer: e.svc.d.Tracer}) // Authz deliberately nil
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc2.Mount(mux)
	if rec := get(t, mux, "/api/v1/repos/secret/manifests"); rec.Code != 404 {
		t.Fatalf("private repository served without an Authorizer: %d", rec.Code)
	}
}

// VF-05: the engine's job spec priority is the steady-state one (PrioScheduled); the planner raises cold keys to backfill itself (unit 04).
func TestRefreshSpecIsScheduledPriority(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core"})
	if p := e.svc.Jobs()[0].Prio; p != job.PrioScheduled {
		t.Fatalf("manifests.refresh priority %v", p)
	}
}

// VF-06: the facts payload is stored through store.Item.Payload, which the store caps at 16 MiB.
func TestConfigFactsBytesFitTheStore(t *testing.T) {
	c := DefaultConfig()
	c.Limits.MaxFactsBytes = 17 << 20
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "limits.max_facts_bytes") {
		t.Fatalf("a payload bound above the store limit was accepted: %v", err)
	}
}

// VF-07: platform/config.Describe rejects a key without jsonschema_description and a non-snake_case or list-index env promise.
func TestConfigTagsAreDocumented(t *testing.T) {
	var walk func(path string, rt reflect.Type)
	walk = func(path string, rt reflect.Type) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if name == "" || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(name) {
				t.Errorf("%s.%s: yaml name %q", path, f.Name, name)
			}
			if strings.TrimSpace(f.Tag.Get("jsonschema_description")) == "" {
				t.Errorf("%s.%s: no jsonschema_description", path, name)
			}
			if f.Type.Kind() == reflect.Struct && f.Type != reflect.TypeOf(time.Time{}) {
				walk(path+"."+name, f.Type)
			}
		}
	}
	walk("manifests", reflect.TypeOf(Config{}))
}

// VF-08: a row that only records a failed first attempt has no payload; WarmItem says so with a sentinel the caller skips silently.
func TestWarmItemWithoutPayload(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core"})
	_, err := e.svc.WarmItem(store.Item{Key: store.ItemKey{Repo: store.RepoKey{Source: "github", Owner: "acme", Name: "core"}, Kind: ItemKind}, State: freshness.StateError, Attempts: 1, Error: "unavailable"})
	if !errors.Is(err, ErrNoFacts) {
		t.Fatalf("err=%v", err)
	}
}

// VF-09: stale-while-error survives a restart: a row with a failed attempt after a good value warms as an erroring (stale) value.
func TestWarmItemKeepsFailureState(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core"})
	ctx := context.Background()
	if _, err := e.svc.runRefresh(ctx, rkey("core")); err != nil {
		t.Fatal(err)
	}
	it := e.st.items[store.ItemKey{Repo: store.RepoKey{Source: "github", Owner: "acme", Name: "core"}, Kind: ItemKind}]
	it.Attempts, it.Error = 2, "unavailable" // what store.RecordFailure leaves on a row that has a payload (status stays "ok")
	v, err := e.svc.WarmItem(it)
	if err != nil {
		t.Fatal(err)
	}
	if v.Info.State != freshness.StateError || v.Info.ErrCode != "unavailable" || !v.Info.HasValue() {
		t.Fatalf("%+v", v.Info)
	}
}

// VF-10: the creation of the tracked branch refreshes the repository; a tag does not.
func TestReactCreateOfTrackedBranch(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core"})
	ref := scm.RepoRef{Source: "github", Owner: "acme", Name: "core"}
	n := func(refName string) int {
		before := e.enq.count(KindRefresh)
		if err := e.svc.React(context.Background(), scm.Event{Kind: scm.EventCreate, Repo: ref, Ref: refName, Created: true}, e.enq); err != nil {
			t.Fatal(err)
		}
		return e.enq.count(KindRefresh) - before
	}
	if n("refs/heads/main") != 1 || n("refs/heads/dev") != 0 || n("refs/tags/v1") != 0 {
		t.Fatal("create rules")
	}
}

// VF-11: the truncated-tree walk reads every directory by its tree id (one immutable call per directory, one consistent snapshot).
func TestTruncatedWalkUsesTreeIDs(t *testing.T) {
	e := newEnv(t, map[string]string{"mono": "root"})
	e.git.trees["mono"] = scm.NewTree("rootsha", nil, true)
	e.git.blobs[gitSHA("svc")] = "svc"
	e.git.byRef = map[string]scm.Tree{
		"rootsha": scm.NewTree("rootsha", []scm.TreeEntry{{Path: "package.json", SHA: gitSHA("root"), Size: 4}, {Path: "services", SHA: "svcdir", Type: scm.EntryTree}}, false),
		"svcdir":  scm.NewTree("svcdir", []scm.TreeEntry{{Path: "package.json", SHA: gitSHA("svc"), Size: 3}}, false),
	}
	if _, err := e.svc.runRefresh(context.Background(), rkey("mono")); err != nil {
		t.Fatal(err)
	}
	if got := len(e.facts(t, "mono").V.Units); got != 2 {
		t.Fatalf("units %d", got)
	}
	for _, c := range e.git.calls {
		if !c.Recursive && c.Path != "" {
			t.Fatalf("a directory was addressed by path, not by tree id: %+v", c)
		}
	}
}

// VF-12: the contract requires a purl on every dependency; an ecosystem whose formatter says "no purl" still gets the custom-type form in the API.
func TestAPIPurlIsAlwaysPresent(t *testing.T) {
	reg := NewRegistry()
	spec, _ := reg.Spec(Terraform)
	spec.FormatPURL = func(ComponentID, string, string, string) (string, bool) { return "", false }
	if err := reg.RegisterEcosystem(spec); err != nil {
		t.Fatal(err)
	}
	p := stubParser{id: "terraform/hcl", eco: Terraform, ver: 1, base: "package.json", fn: func(File) (Parsed, error) {
		return Parsed{Provides: []Artifact{{ID: ComponentID{Terraform, "acme", "net"}, Version: "1.0.0", Kind: "library"}},
			Dependencies: []Dependency{{Target: ComponentID{Terraform, "hashicorp", "aws"}, Requirement: "~> 5.0", Scope: ScopeRuntime, Source: SourceRegistry}}}, nil
	}}
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	reg.Freeze()
	e := newEnvReg(t, map[string]string{"infra": "x"}, reg)
	if _, err := e.svc.runRefresh(context.Background(), rkey("infra")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.runGraph(context.Background(), graphKey()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	e.svc.Mount(mux)
	d := decode[manifestsapi.DependencyList](t, get(t, mux, "/api/v1/repos/infra/dependencies"))
	if len(d.Items) != 1 || d.Items[0].Purl == "" || !regexp.MustCompile(`^pkg:[a-z][a-z0-9.+-]*/[^\s]+$`).MatchString(d.Items[0].Purl) {
		t.Fatalf("%+v", d.Items)
	}
	m := decode[manifestsapi.RepoManifests](t, get(t, mux, "/api/v1/repos/infra/manifests"))
	if pu := m.Units[0].Provides[0].Purl; !regexp.MustCompile(`^pkg:[a-z][a-z0-9.+-]*/[^\s]+$`).MatchString(pu) {
		t.Fatalf("artifact purl %q", pu)
	}
	if rec := get(t, mux, "/api/v1/components?purl="+d.Items[0].Purl); rec.Code != 200 {
		t.Fatalf("the purl the API prints must be accepted by getComponent: %d %s", rec.Code, rec.Body)
	}
}

// VF-13: unit ids satisfy the contract pattern ^[^#\s]{1,100}#[^\s]{1,300}$ for any legal directory name.
func TestUnitIDsSatisfyTheContractPattern(t *testing.T) {
	pat := regexp.MustCompile(`^[^#\s]{1,100}#[^\s]{1,300}$`)
	long := strings.Repeat("d", 100) + "/" + strings.Repeat("e", 100) + "/" + strings.Repeat("f", 100) + "/" + strings.Repeat("g", 100)
	for _, dir := range []string{".", "my project/api", "tab\tdir", "nbsp dir", "100%/x", long} {
		u := Unit{Ecosystem: NPM, Dir: dir, Files: []FileRef{{Path: dir + "/package.json"}}}
		us, _ := finalize([]Unit{u}, map[string]Role{dir + "/package.json": RolePrimary}, 500, DefaultConfig().Limits)
		if id := unitID("repo", us[0].ID); !pat.MatchString(id) {
			t.Errorf("dir %q -> id %q violates the pattern", dir, id)
		}
	}
	a, _ := finalize([]Unit{{Ecosystem: NPM, Dir: long + "/a"}, {Ecosystem: NPM, Dir: long + "/b"}}, nil, 500, DefaultConfig().Limits)
	if a[0].ID == a[1].ID {
		t.Fatal("two long directories collapsed to one id")
	}
}

// VF-14: a registry never lets a disabled ecosystem back in, whichever order cmd/scp calls Register and Disable.
func TestDisableThenRegister(t *testing.T) {
	r := NewRegistry()
	r.Disable(NPM)
	if err := r.Register(stubParser{id: "npm/a", eco: NPM, ver: 1, base: "a.json"}); err != nil {
		t.Fatal(err)
	}
	r.Freeze()
	if _, ok := r.Parser("npm/a"); ok || len(r.Match("a.json")) != 0 {
		t.Fatal("a parser of a disabled ecosystem is active")
	}
}

// VF-15: units that overflow the payload together (none is the largest by far) still publish, with limit.deps, instead of failing permanently.
func TestManyMidSizedUnitsAreShrunkToFit(t *testing.T) {
	e := newEnv(t, map[string]string{"mono": "x"})
	content, one := bigDeps(200), strings.Replace(bigDeps(400), "big:", "huge:", 1)
	sha, shaOne := gitSHA(content), gitSHA(one)
	e.git.blobs[sha], e.git.blobs[shaOne] = content, one
	entries := []scm.TreeEntry{{Path: "huge/package.json", SHA: shaOne, Size: int64(len(one))}}
	for i := 0; i < 79; i++ {
		entries = append(entries, scm.TreeEntry{Path: fmt.Sprintf("svc%02d/package.json", i), SHA: sha, Size: int64(len(content))})
	}
	e.git.trees["mono"] = scm.NewTree(fakeOID("mono"), entries, false)
	e.svc.cfg.Acquisition.MaxFiles = 200
	e.svc.cfg.Limits.MaxFactsBytes = 300 << 10
	if _, err := e.svc.runRefresh(context.Background(), rkey("mono")); err != nil {
		t.Fatalf("a repository that can be stored after trimming failed: %v", err)
	}
	f := e.facts(t, "mono")
	if len(f.V.Units) != 80 || !hasDiag(f.V.Diagnostics, DiagDepsCap) {
		t.Fatalf("units=%d diags=%v", len(f.V.Units), f.V.Diagnostics)
	}
}

// VF-16: a locked-component set larger than output.sbom_max_locked is cut deterministically and says so; the validator stays usable.
func TestSBOMLockedBudget(t *testing.T) {
	e, mux := apiEnv(t)
	e.svc.cfg.Output.SBOMMaxLocked = 1
	f := e.facts(t, "app")
	u := f.V.Units[0]
	u.LockedCount = 5
	f.V.Units = []Unit{u}
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "app", e.slots.Facts, f) }); err != nil {
		t.Fatal(err)
	}
	rec := get(t, mux, "/api/v1/repos/app/sbom.cdx.json")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"scp:lockedOmitted"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("an SBOM cut by the budget is deterministic and keeps its validator")
	}
}

// VF-17: the artifact kind enum of the contract has `other`.
func TestArtifactKindOther(t *testing.T) {
	if artifactKind("metapackage") != "other" || artifactKind("plugin") != "plugin" || artifactKind("") != "library" {
		t.Fatal("artifactKind")
	}
}
