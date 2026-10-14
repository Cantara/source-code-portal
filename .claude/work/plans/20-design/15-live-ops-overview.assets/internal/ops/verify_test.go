package ops_test

// Tests added by the independent verification of design unit 15 (findings V15-xx in 30-verification/design-15-live-ops-overview.md).

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/ops"
	"github.com/cantara/source-code-portal/internal/ops/opsapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

// fullFake implements every optional source so that deriveWarnings sees every input.
type fullFake struct {
	fake
	inst  []ops.InstallationView
	rates []ops.RateView
	tasks []ops.TaskView
	docs  ops.DocsView
	desc  ops.DescriptorsView
	man   ops.ManifestsView
	integ []ops.IntegrationView
	acc   ops.AccessView
	hook  ops.WebhookView
}

func (f *fullFake) Installations() []ops.InstallationView    { return f.inst }
func (f *fullFake) Rate() []ops.RateView                     { return f.rates }
func (f *fullFake) Tasks() []ops.TaskView                    { return f.tasks }
func (f *fullFake) Docs() ops.DocsView                       { return f.docs }
func (f *fullFake) Descriptors() ops.DescriptorsView         { return f.desc }
func (f *fullFake) Manifests() ops.ManifestsView             { return f.man }
func (f *fullFake) IntegrationHealth() []ops.IntegrationView { return f.integ }
func (f *fullFake) Access() ops.AccessView                   { return f.acc }
func (f *fullFake) Webhook() ops.WebhookView                 { return f.hook }

func newFull(t *testing.T, w *world, f *fullFake, k ops.Kind, cfg ops.Config) *ops.Service {
	t.Helper()
	svc, err := ops.New(cfg, ops.DefaultReadiness(), ops.Deps{
		Build: ops.BuildInfo{Version: "1"}, Started: t0.Add(-48 * time.Hour),
		Sources: ops.Sources{Snapshot: f, Store: f, Engine: f, Catalogue: f, Installations: f, Tasks: f, Features: f, Integrations: f, Access: f, Webhook: f},
		Kinds:   []ops.Kind{k}, Retrier: f})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func codesOf(doc opsapi.Status) []string {
	var c []string
	for _, w := range doc.Warnings {
		c = append(c, w.Code)
	}
	return c
}

// V15-18: the design table lists 20 rows of warning codes; the first draft's test exercised four.
func TestWarningsCoverEveryRuleOfTheTable(t *testing.T) {
	// a publisher that rejects one candidate, for snapshot.invalid-publish
	reg := &readmodel.Registry{}
	repo := readmodel.Register[int](reg, "catalog.repo", readmodel.Existence())
	com := readmodel.Register[freshness.Info](reg, "commits.view")
	pub := readmodel.NewPublisher(reg, nil, []readmodel.Validator{func(s *readmodel.Snapshot) error {
		if s.Has("bad") {
			return errors.New("bad entry")
		}
		return nil
	}}, readmodel.Deps{})
	if _, err := pub.Apply(func(b *readmodel.Builder) error {
		_ = readmodel.Put(b, "a", repo, 1)
		return readmodel.Put(b, "a", com, *okInfo(t0.Add(-40 * time.Hour))) // 40 h old, TTL 2 h, interval 12 h: 3 x 12 h = 36 h
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "bad", repo, 1) })
	w := &world{pub: pub, repo: repo, com: com}
	now := t0
	f := &fullFake{
		fake: fake{pub: pub, cat: ops.CatalogueView{StoreOpened: true, Live: true},
			store: ops.StoreView{OK: false, Reason: "disk full", SizeBytes: -1, BackupEnabled: true, BackupInterval: time.Hour, LastBackupAt: now.Add(-5 * time.Hour)},
			eng: ops.EngineView{Heartbeat: now, ParkedTotal: 60, Parked: []ops.ParkedView{{Key: "k/a", Since: now.Add(-time.Hour)}},
				Upstreams: []ops.UpstreamView{
					{Name: "github", Concurrency: 2, Stuck: 2, AllStuckSince: now.Add(-time.Minute)},
					{Name: "jenkins", Concurrency: 2, PausedUntil: now.Add(time.Minute), PauseReason: "unauthorized"},
					{Name: "snyk", Concurrency: 2, PausedUntil: now.Add(time.Minute), PauseReason: "breaker"}}}},
		inst:  []ops.InstallationView{{Source: "a", State: "suspended"}, {Source: "b", State: "removed", TokenExpiresAt: now.Add(-time.Minute)}},
		rates: []ops.RateView{{Source: "a", Resource: "core", Limit: 5000, Remaining: 100, ResetAt: now.Add(time.Hour)}},
		docs:  ops.DocsView{NotResidentDocs: 3, BudgetBytes: 1 << 20},
		desc:  ops.DescriptorsView{Problems: 2, RepositoriesWithProblems: 1},
		man:   ops.ManifestsView{Enabled: true, StaleRepos: 2, DisabledParsers: []string{"x"}},
		integ: []ops.IntegrationView{{Provider: "jenkins", Enabled: true, State: "unknown", TTL: time.Minute}},
		acc:   ops.AccessView{Warnings: []string{"no_proxy_secret"}},
		hook:  ops.WebhookView{Enabled: true, LastAcceptedAt: now.Add(-48 * time.Hour)},
	}
	f.store.LastBackupAt = now.Add(-5 * time.Hour)
	k := w.kind()
	k.Interval = 12 * time.Hour
	svc := newFull(t, w, f, k, ops.DefaultConfig())
	doc := svc.Status.Status(now)
	got := codesOf(doc)
	for _, want := range []string{"store.degraded", "store.backup-stale", "snapshot.invalid-publish", "upstream.stuck", "upstream.unauthorized", "upstream.paused",
		"jobs.parked", "github.installation-suspended", "github.installation-removed", "github.token-expired", "github.rate-low", "jobs.stale",
		"docs.budget-exceeded", "descriptors.problems", "manifests.parser-disabled", "manifests.stale", "integrations.degraded", "access.no_proxy_secret", "webhook.silent"} {
		if !slices.Contains(got, want) {
			t.Errorf("rule %s did not fire; got %v", want, got)
		}
	}
	// parked 60 >= 50 is critical
	for _, x := range doc.Warnings {
		if x.Code == "jobs.parked" && x.Severity != "critical" {
			t.Errorf("60 parked keys must be critical: %+v", x)
		}
		if x.Code == "github.rate-low" && x.Severity != "warning" { // 100/5000 = 2%, below 10%, not below a fifth of it (2%)
			t.Logf("rate-low severity %s", x.Severity)
		}
	}
	// readiness.degraded needs the degraded phase: separate case below
}

// V15-06: commits has TTL 2 h and an idle back-off cap of 12 h. An item that is 8 h old is normal for a quiet repository.
func TestJobsStaleIsMeasuredAgainstTheIdleBackoffInterval(t *testing.T) {
	for _, c := range []struct {
		name     string
		age      time.Duration
		interval time.Duration
		want     bool
	}{
		{"8 h old, TTL only: spurious warning (the first draft)", 8 * time.Hour, 0, true},
		{"8 h old with the 12 h interval: normal", 8 * time.Hour, 12 * time.Hour, false},
		{"35 h old with the 12 h interval: still inside 3 x 12 h", 35 * time.Hour, 12 * time.Hour, false},
		{"37 h old with the 12 h interval: stale", 37 * time.Hour, 12 * time.Hour, true},
	} {
		w := newWorld(t)
		w.put(t, "a", okInfo(t0.Add(-c.age)))
		f := &fullFake{fake: fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}, store: ops.StoreView{OK: true}, eng: ops.EngineView{Heartbeat: t0}}}
		k := w.kind()
		k.Interval = c.interval
		doc := newFull(t, w, f, k, ops.DefaultConfig()).Status.Status(t0)
		if has := slices.Contains(codesOf(doc), "jobs.stale"); has != c.want {
			t.Errorf("%s: jobs.stale=%v want %v", c.name, has, c.want)
		}
	}
}

func TestWarningReadinessDegradedAndNeverBackedUpAndIntegrationGrace(t *testing.T) {
	w := newWorld(t)
	w.put(t, "a", nil)
	cfg := ops.DefaultReadiness()
	cfg.StartupDeadline = time.Minute
	f := &fullFake{
		fake:  fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}, store: ops.StoreView{OK: true, BackupEnabled: true, BackupInterval: time.Hour}, eng: ops.EngineView{Heartbeat: t0}},
		integ: []ops.IntegrationView{{Provider: "jenkins", Enabled: true, State: "unknown", TTL: 5 * time.Minute}},
	}
	svc, err := ops.New(ops.DefaultConfig(), cfg, ops.Deps{Build: ops.BuildInfo{Version: "1"}, Started: t0.Add(-4 * time.Hour),
		Sources: ops.Sources{Snapshot: f, Store: f, Engine: f, Catalogue: f, Integrations: f}, Kinds: []ops.Kind{w.kind()}, Retrier: f})
	if err != nil {
		t.Fatal(err)
	}
	codes := codesOf(svc.Status.Status(t0))
	for _, want := range []string{"readiness.degraded", "store.backup-stale", "integrations.degraded"} {
		if !slices.Contains(codes, want) {
			t.Errorf("%s missing: %v", want, codes)
		}
	}
	// a provider that has simply not completed its first sweep yet must not warn at once after start
	svc2, _ := ops.New(ops.DefaultConfig(), ops.DefaultReadiness(), ops.Deps{Build: ops.BuildInfo{Version: "1"}, Started: t0.Add(-time.Minute),
		Sources: ops.Sources{Snapshot: f, Store: f, Engine: f, Catalogue: f, Integrations: f}, Kinds: []ops.Kind{w.kind()}, Retrier: f})
	if slices.Contains(codesOf(svc2.Status.Status(t0)), "integrations.degraded") {
		t.Error("integrations.degraded raised one minute after start, before the first sweep was due")
	}
}

// V15-05: warning messages, task errors and the store reason are cut to 300 bytes, one line, valid UTF-8.
func TestStatusTextIsBoundedOneLineAndValidUTF8(t *testing.T) {
	long := strings.Repeat("é", 400) + "\nsecond line\x00"
	w := newWorld(t)
	w.put(t, "a", okInfo(t0))
	f := &fullFake{fake: fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}, store: ops.StoreView{OK: false, Reason: long, SizeBytes: -1},
		eng: ops.EngineView{Heartbeat: t0, Globals: []ops.GlobalJobView{{Kind: "catalog.reconcile", Upstream: "github", TTL: time.Minute, LastSuccessAt: t0.Add(-time.Hour), ConsecutiveFailures: 3, LastError: long}}}},
		tasks: []ops.TaskView{{Name: "t", Every: time.Minute, LastError: long}}}
	doc := newFull(t, w, f, w.kind(), ops.DefaultConfig()).Status.Status(t0)
	check := func(what, s string) {
		if len(s) > 300 || !utf8.ValidString(s) || strings.ContainsAny(s, "\n\x00") {
			t.Errorf("%s: %d bytes valid=%v %q", what, len(s), utf8.ValidString(s), s)
		}
	}
	for _, x := range doc.Warnings {
		check("warning "+x.Code, x.Message)
	}
	if doc.Store.Reason == nil {
		t.Fatal("no store reason")
	}
	check("store.reason", *doc.Store.Reason)
	check("task.lastError", *doc.Tasks[0].LastError)
	if !slices.Contains(codesOf(doc), "jobs.stale") {
		t.Fatalf("the failing global job did not produce jobs.stale: %v", codesOf(doc))
	}
}

func TestStatusParkedListIsCappedTotalIsNot(t *testing.T) {
	w := newWorld(t)
	w.put(t, "a", okInfo(t0))
	var parked []ops.ParkedView
	for i := range 5 {
		parked = append(parked, ops.ParkedView{Key: fmt.Sprintf("k/%d", i), Since: t0.Add(time.Duration(i) * time.Minute), Reason: "perm"})
	}
	f := &fullFake{fake: fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}, store: ops.StoreView{OK: true}, eng: ops.EngineView{Heartbeat: t0, Parked: parked, ParkedTotal: 7}}}
	cfg := ops.DefaultConfig()
	cfg.Status.MaxParkedShown = 2
	doc := newFull(t, w, f, w.kind(), cfg).Status.Status(t0)
	if len(doc.Jobs.Parked) != 2 || doc.Jobs.ParkedTotal != 7 || doc.Jobs.Parked[0].Key != "k/0" {
		t.Fatalf("%+v", doc.Jobs)
	}
}

func TestStatusETagIsWeakBodyHash(t *testing.T) {
	w := newWorld(t)
	w.put(t, "a", okInfo(time.Now()))
	f := &fullFake{fake: fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}, store: ops.StoreView{OK: true}, eng: ops.EngineView{Heartbeat: time.Now()}}}
	rec := httptest.NewRecorder()
	newFull(t, w, f, w.kind(), ops.DefaultConfig()).Status.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/status", nil))
	if et := rec.Header().Get("ETag"); !strings.HasPrefix(et, `W/"b`) {
		t.Fatalf("design 07 section 4.4 says weak W/\"b<hash>\": %q", et)
	}
}

// V15-03: net/http/pprof serves delta profiles (heap?seconds=N, goroutine?seconds=N ...) from the index handler and
// holds the request for N seconds; the cap must apply there as well, not only to profile and trace.
func TestPprofIndexDeltaSecondsAreCapped(t *testing.T) {
	hs := ops.PprofHandlers(ops.DefaultConfig().Pprof)
	for _, target := range []string{"/debug/pprof/heap?seconds=3600", "/debug/pprof/goroutine?seconds=-1", "/debug/pprof/allocs?seconds=x"} {
		rec := httptest.NewRecorder()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // an uncapped delta profile would hold the request for the whole N seconds
		hs[ops.RoutePprofIndex].ServeHTTP(rec, httptest.NewRequest("GET", target, nil).WithContext(ctx))
		cancel()
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "#/query/seconds") {
			t.Errorf("%s: %d %s", target, rec.Code, rec.Body)
		}
	}
}

// V15-09: the dispatcher beats every 10 s (unit 04); a heartbeat_max below two beats makes /livez flap.
func TestLivenessConfigFloor(t *testing.T) {
	cfg := ops.DefaultConfig()
	cfg.Liveness.HeartbeatMax = 10 * time.Second
	if errs := cfg.Validate("ops"); len(errs) == 0 || !strings.Contains(errs[0].Error(), "ops.liveness.heartbeat_max") {
		t.Fatalf("%v", errs)
	}
}

func TestReadySinceIsKeptWhenADegradedStartBecomesReady(t *testing.T) {
	w := newWorld(t)
	w.put(t, "r", nil)
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}}
	r := newReadiness(t, w, f, ops.DefaultReadiness(), t0)
	d := r.Evaluate(t0.Add(time.Hour))
	if d.Phase != ops.PhaseDegraded {
		t.Fatal(d.Phase)
	}
	w.put(t, "r", okInfo(t0.Add(time.Hour)))
	u := r.Evaluate(t0.Add(2 * time.Hour))
	if u.Phase != ops.PhaseReady || !u.ReadySince.Equal(d.ReadySince) {
		t.Fatalf("readySince moved from %v to %v", d.ReadySince, u.ReadySince)
	}
}
