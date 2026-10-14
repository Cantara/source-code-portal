package integrations_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/integrations"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// ---- config ----

func jcfg() integrations.Config {
	c := integrations.DefaultConfig()
	c.Jenkins.Instances = []integrations.JenkinsInstance{{Name: "main", BaseURL: "https://jenkins.example.org/ci"}}
	c.Jenkins.ApplyInstanceDefaults()
	return c
}

func TestConfigValidate(t *testing.T) {
	mut := func(f func(*integrations.Config)) integrations.Config { c := jcfg(); f(&c); return c }
	snykOK := func() integrations.Config {
		c := integrations.DefaultConfig()
		c.Snyk.Enabled, c.Snyk.Token, c.Snyk.OrgID = true, secret.New("t"), "4a18d42f-0706-4ad0-b127-24078731fbed"
		return c
	}
	tests := []struct {
		name string
		cfg  integrations.Config
		want string
	}{
		{"all off", integrations.DefaultConfig(), ""},
		{"jenkins ok", jcfg(), ""},
		{"http without allow_http", mut(func(c *integrations.Config) { c.Jenkins.Instances[0].BaseURL = "http://j.local" }), "scheme must be https"},
		{"http with allow_http", mut(func(c *integrations.Config) {
			c.Jenkins.Instances[0].BaseURL = "http://j.local:8080"
			c.Jenkins.Instances[0].AllowHTTP = true
		}), ""},
		{"userinfo in url", mut(func(c *integrations.Config) { c.Jenkins.Instances[0].BaseURL = "https://u:p@j.local" }), "userinfo"},
		{"query in url", mut(func(c *integrations.Config) { c.Jenkins.Instances[0].BaseURL = "https://j.local/?a=1" }), "query"},
		{"escaped path", mut(func(c *integrations.Config) { c.Jenkins.Instances[0].BaseURL = "https://j.local/a%2Fb" }), "plain"},
		{"two instances no default", mut(func(c *integrations.Config) {
			c.Jenkins.Instances = append(c.Jenkins.Instances, integrations.JenkinsInstance{Name: "b", BaseURL: "https://b.local", Discover: "declared", Multibranch: "auto", Timeout: time.Second * 5, MaxResponseBytes: 1 << 20})
		}), "exactly one must set default"},
		{"bad name", mut(func(c *integrations.Config) { c.Jenkins.Instances[0].Name = "Main_1" }), "does not match"},
		{"user without token", mut(func(c *integrations.Config) { c.Jenkins.Instances[0].User = "bot" }), "set together"},
		{"cidr zero", mut(func(c *integrations.Config) { c.Jenkins.Instances[0].AllowPrivateCIDRs = []string{"0.0.0.0/0"} }), "every address"},
		{"sweep_timeout >= ttl", mut(func(c *integrations.Config) { c.Jenkins.SweepTimeout = 6 * time.Minute }), "shorter than ttl"},
		{"snyk ok", snykOK(), ""},
		{"snyk no token", func() integrations.Config { c := snykOK(); c.Snyk.Token = secret.New(""); return c }(), "required when snyk.enabled"},
		{"snyk org not uuid", func() integrations.Config { c := snykOK(); c.Snyk.OrgID = "acme"; return c }(), "UUID"},
		{"snyk custom host refused", func() integrations.Config { c := snykOK(); c.Snyk.BaseURL = "https://snyk.corp.example"; return c }(), "region-specific"},
		{"snyk custom host allowed", func() integrations.Config {
			c := snykOK()
			c.Snyk.BaseURL = "https://snyk.corp.example"
			c.Snyk.AllowCustomHost = true
			return c
		}(), ""},
		{"snyk eu", func() integrations.Config { c := snykOK(); c.Snyk.BaseURL = "https://api.eu.snyk.io"; return c }(), ""},
		{"snyk http", func() integrations.Config { c := snykOK(); c.Snyk.BaseURL = "http://api.snyk.io"; return c }(), "bare https origin"},
		{"snyk path in base", func() integrations.Config { c := snykOK(); c.Snyk.BaseURL = "https://api.snyk.io/rest"; return c }(), "bare https origin"},
		{"snyk bad template placeholder", func() integrations.Config {
			c := snykOK()
			c.Snyk.DetailURLTemplate = "https://a.example/{token}"
			return c
		}(), "unknown placeholder"},
		{"snyk http template", func() integrations.Config {
			c := snykOK()
			c.Snyk.DetailURLTemplate = "http://a.example/{repo}"
			return c
		}(), "https URL"},
		{"snyk keys while disabled", func() integrations.Config { c := integrations.DefaultConfig(); c.Snyk.OrgID = "x"; return c }(), "ignored"},
		{"snyk bad version", func() integrations.Config { c := snykOK(); c.Snyk.APIVersion = "latest"; return c }(), "api_version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// ---- job paths ----

func TestParseJobPath(t *testing.T) {
	good := map[string]string{"alpha": "/job/alpha", "team/alpha": "/job/team/job/alpha", "my team/a.b_c-d": "/job/my%20team/job/a.b_c-d"}
	for in, want := range good {
		jp, err := integrations.ParseJobPath(in)
		if err != nil || jp.URLPath() != want || jp.String() != in {
			t.Errorf("%q: %v %q", in, err, jp.URLPath())
		}
	}
	for _, bad := range []string{"", "/a", "a/", "a//b", ".", "..", "a/../b", "a/./b", "a%2Fb", "a?x=1", "a#b", "a\\b", "a\nb", " a", "a ", "x/ y", strings.Repeat("a", 101), strings.Repeat("a/", 200), "ä"} {
		if _, err := integrations.ParseJobPath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func FuzzParseJobPath(f *testing.F) {
	for _, s := range []string{"a", "a/b", "../x", "a%2f", "a b/c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		jp, err := integrations.ParseJobPath(s)
		if err != nil {
			return
		}
		u := jp.URLPath()
		// invariant: the rendered path has exactly 2 slashes per segment, no dot segments, no raw or double-encoded separators
		if strings.Count(u, "/") != 2*len(jp) || strings.Contains(u, "/./") || strings.Contains(u, "/../") || strings.Contains(u, "%2F") || strings.Contains(u, "%2f") || strings.Contains(u, "//") {
			t.Fatalf("unsafe URL path %q from %q", u, s)
		}
	})
}

// ---- fixtures for service tests ----

type env struct {
	pub    *readmodel.Publisher
	repo   readmodel.Slot[freshness.Value[scm.Repository]]
	mem    readmodel.Global[scm.Membership]
	status readmodel.Slot[integrations.Statuses]
}

func newEnv(t *testing.T, repos []scm.Repository, groups []scm.GroupMeta, byGroup map[scm.GroupID][]scm.Slug) *env {
	t.Helper()
	reg := &readmodel.Registry{}
	e := &env{}
	e.repo = readmodel.Register[freshness.Value[scm.Repository]](reg, "catalog.repo", readmodel.Existence())
	e.mem = readmodel.RegisterGlobal[scm.Membership](reg, "catalog.membership")
	e.status = readmodel.Register[integrations.Statuses](reg, "integrations.status")
	e.pub = readmodel.NewPublisher(reg)
	_, err := e.pub.Apply(func(b *readmodel.Builder) error {
		for _, r := range repos {
			if err := readmodel.Put(b, readmodel.EntryID(r.Slug), e.repo, freshness.Value[scm.Repository]{V: r}); err != nil {
				return err
			}
		}
		readmodel.PutGlobal(b, e.mem, scm.NewMembership(groups, byGroup))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func repo(name string, vis scm.Visibility) scm.Repository {
	return scm.Repository{Ref: scm.RepoRef{Source: "acme", ID: scm.RepoID(len(name)), Owner: "acme", Name: name}, Slug: scm.Slug(strings.ToLower(name)), DefaultBranch: "main", Visibility: vis}
}

type ovr map[scm.Slug]map[string]map[string]string

func (o ovr) IntegrationOverrides(_ *readmodel.Snapshot, s scm.Slug) map[string]map[string]string {
	return o[s]
}

func TestResolveJenkinsTable(t *testing.T) {
	repos := []scm.Repository{repo("Alpha", 0), repo("Beta", 0), repo("Gamma", 0), repo("Delta", 0)}
	arch := repo("Old", 0)
	arch.Archived = true
	repos = append(repos, arch)
	groups := []scm.GroupMeta{
		{ID: "g1", Integrations: map[string]map[string]string{"jenkins": {"job_prefix": "Team-"}}},
		{ID: "g2", Integrations: map[string]map[string]string{"jenkins": {"job_prefix": "Other-"}}},
		{ID: "all"},
	}
	by := map[scm.GroupID][]scm.Slug{"g1": {"alpha"}, "g2": {"alpha", "beta"}, "all": {"alpha", "beta", "gamma", "delta", "old"}}
	e := newEnv(t, repos, groups, by)
	ov := ovr{"gamma": {"jenkins": {"job": "team/gamma-ci"}}, "delta": {"jenkins": {"job": "../escape"}}}
	cfg := jcfg().Jenkins
	snap := e.pub.Current()
	mem, _ := readmodel.GetGlobal(snap, e.mem)
	targets, bad := integrations.ResolveJenkins(cfg, snap, e.repo, mem, ov)
	got := map[string]string{}
	for _, tg := range targets {
		got[tg.Repo.Name] = tg.Instance + ":" + tg.Job + "@" + tg.Branch
	}
	want := map[string]string{
		"Alpha": "main:Team-Alpha@main", // first group in configuration order that defines the key wins (g1, not g2)
		"Beta":  "main:Other-Beta@main",
		"Gamma": "main:team/gamma-ci@main", // descriptor beats group default
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("targets = %v\nwant     %v", got, want)
	}
	if len(bad) != 1 || bad[0].Slug != "delta" {
		t.Fatalf("invalid = %+v", bad)
	}
	// discover: all adds a blind target for every repo without a declaration (archived never)
	cfg.Instances[0].Discover, cfg.Instances[0].JobPrefix = "all", "ci/"
	targets, _ = integrations.ResolveJenkins(cfg, snap, e.repo, mem, ov)
	if len(targets) != 3 || targets[slices.IndexFunc(targets, func(x integrations.Target) bool { return x.Repo.Name == "Gamma" })].Job != "team/gamma-ci" {
		t.Fatalf("discover all: %+v", targets)
	}
}

// Membership order must not matter (GR-08): the same configuration with groups listed in different membership order resolves identically.
func TestResolveIndependentOfMembershipOrder(t *testing.T) {
	repos := []scm.Repository{repo("Alpha", 0)}
	groups := []scm.GroupMeta{
		{ID: "g1", Integrations: map[string]map[string]string{"jenkins": {"job_prefix": "A-"}}},
		{ID: "g2", Integrations: map[string]map[string]string{"jenkins": {"job_prefix": "B-"}}},
	}
	for _, by := range []map[scm.GroupID][]scm.Slug{{"g1": {"alpha"}, "g2": {"alpha"}}, {"g2": {"alpha"}, "g1": {"alpha"}}} {
		e := newEnv(t, repos, groups, by)
		snap := e.pub.Current()
		mem, _ := readmodel.GetGlobal(snap, e.mem)
		ts, _ := integrations.ResolveJenkins(jcfg().Jenkins, snap, e.repo, mem, nil)
		if len(ts) != 1 || ts[0].Job != "A-Alpha" {
			t.Fatalf("targets = %+v", ts)
		}
	}
}

func TestResolveSnyk(t *testing.T) {
	repos := []scm.Repository{repo("Alpha", 0), repo("Beta", 0), repo("Gamma", 0), repo("Delta", 0)}
	groups := []scm.GroupMeta{{ID: "g1", Integrations: map[string]map[string]string{"snyk": {"project_prefix": "x-"}}}}
	e := newEnv(t, repos, groups, map[scm.GroupID][]scm.Slug{"g1": {"beta"}})
	ov := ovr{"gamma": {"snyk": {"project": "gamma-service"}}, "delta": {"snyk": {"project": "a/b"}}}
	snap := e.pub.Current()
	mem, _ := readmodel.GetGlobal(snap, e.mem)
	ts, bad := integrations.ResolveSnyk(snap, e.repo, mem, ov)
	got := map[string]string{}
	for _, x := range ts {
		got[x.Repo.Name] = x.Project
	}
	want := "map[Alpha:acme/Alpha Beta:acme/x-Beta Gamma:acme/gamma-service]"
	if fmt.Sprint(got) != want || len(bad) != 1 || bad[0].Slug != "delta" {
		t.Fatalf("got %v bad %+v", got, bad)
	}
}

// ---- Fact ----

func TestFactOf(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	val := func(p string, s integrations.State, counts map[string]int, fetched time.Time, st freshness.State) freshness.Value[integrations.Status] {
		return freshness.Value[integrations.Status]{V: integrations.Status{Provider: p, State: s, Counts: counts},
			Info: freshness.Info{State: st, FetchedAt: fetched, CheckedAt: now, TTL: 5 * time.Minute}}
	}
	tests := []struct {
		name string
		v    freshness.Value[integrations.Status]
		want integrations.Fact
	}{
		{"jenkins ok", val("jenkins", integrations.StateOK, nil, now, freshness.StateOK), integrations.Fact{Provider: "jenkins", Message: "passing", Tone: integrations.ToneGood}},
		{"jenkins failing", val("jenkins", integrations.StateFailing, nil, now, freshness.StateOK), integrations.Fact{Provider: "jenkins", Message: "failing", Tone: integrations.ToneBad}},
		{"jenkins unstable", val("jenkins", integrations.StateUnstable, nil, now, freshness.StateOK), integrations.Fact{Provider: "jenkins", Message: "unstable", Tone: integrations.ToneWarn}},
		{"jenkins stale goes grey but keeps message", val("jenkins", integrations.StateOK, nil, now.Add(-time.Hour), freshness.StateOK), integrations.Fact{Provider: "jenkins", Message: "passing", Stale: true}},
		{"jenkins error without value", val("jenkins", integrations.StateUnknown, nil, time.Time{}, freshness.StateError), integrations.Fact{Provider: "jenkins", Message: "unknown"}},
		{"snyk clean", val("snyk", integrations.StateOK, map[string]int{}, now, freshness.StateOK), integrations.Fact{Provider: "snyk", Message: "no issues", Tone: integrations.ToneGood}},
		{"snyk top severity", val("snyk", integrations.StateFailing, map[string]int{"high": 3, "low": 9}, now, freshness.StateOK), integrations.Fact{Provider: "snyk", Message: "3 high", Tone: integrations.ToneBad}},
		{"snyk medium unstable", val("snyk", integrations.StateUnstable, map[string]int{"medium": 2}, now, freshness.StateOK), integrations.Fact{Provider: "snyk", Message: "2 medium", Tone: integrations.ToneWarn}},
		{"never fetched", freshness.Value[integrations.Status]{V: integrations.Status{Provider: "jenkins"}, Info: freshness.Info{State: freshness.StateNever}}, integrations.Fact{Provider: "jenkins", Message: "unknown"}},
		{"not_found is not green", val("jenkins", integrations.StateUnknown, nil, now, freshness.StateNotFound), integrations.Fact{Provider: "jenkins", Message: "unknown"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := integrations.FactOf(tc.v, now); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

// ---- service ----

type fakeProvider struct {
	name string
	mu   sync.Mutex
	fn   func(t integrations.Target) (integrations.Status, error)
	n    int
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Fetch(_ context.Context, t integrations.Target) (integrations.Status, error) {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
	return f.fn(t)
}

type memStore struct {
	mu    sync.Mutex
	items map[store.ItemKey]store.Item
	ops   []string
}

func newMemStore() *memStore { return &memStore{items: map[store.ItemKey]store.Item{}} }
func (m *memStore) PutItem(_ context.Context, it store.Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[it.Key] = it
	m.ops = append(m.ops, "put")
	return nil
}
func (m *memStore) Revalidated(_ context.Context, k store.ItemKey, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, "reval")
	return nil
}
func (m *memStore) RecordFailure(_ context.Context, k store.ItemKey, f store.Failure) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, "fail:"+f.Code)
	return nil
}

func okStatus(string) func(integrations.Target) (integrations.Status, error) {
	return func(integrations.Target) (integrations.Status, error) {
		return integrations.Status{State: integrations.StateOK, Summary: "build #1 passed"}, nil
	}
}

func svc(t *testing.T, e *env, cfg integrations.Config, ms *memStore, ov integrations.Overrides, ps ...integrations.StatusProvider) *integrations.Service {
	t.Helper()
	s, err := integrations.New(cfg, integrations.Deps{Publisher: e.pub, Repos: e.repo, Membership: e.mem, Status: e.status, Overrides: ov, Store: ms, Providers: ps})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func declared(slugs ...string) (ovr, []scm.Repository) {
	o := ovr{}
	var rs []scm.Repository
	for _, s := range slugs {
		o[scm.Slug(s)] = map[string]map[string]string{"jenkins": {"job": s}}
		rs = append(rs, repo(strings.ToUpper(s[:1])+s[1:], 0))
	}
	return o, rs
}

func TestNewChecksWiring(t *testing.T) {
	e := newEnv(t, nil, nil, nil)
	if _, err := integrations.New(jcfg(), integrations.Deps{Publisher: e.pub, Status: e.status}); err == nil || !strings.Contains(err.Error(), "enabled=true") {
		t.Fatalf("missing provider not detected: %v", err)
	}
	if _, err := integrations.New(integrations.DefaultConfig(), integrations.Deps{Publisher: e.pub, Status: e.status, Providers: []integrations.StatusProvider{&fakeProvider{name: "jenkins"}}}); err == nil {
		t.Fatal("provider registered while disabled must be refused")
	}
	s, err := integrations.New(integrations.DefaultConfig(), integrations.Deps{Publisher: e.pub, Status: e.status})
	if err != nil || len(s.Jobs()) != 0 {
		t.Fatalf("all off: %v %d", err, len(s.Jobs()))
	}
	// disabled provider reads as disabled, never ok (GR-41)
	v := s.StatusOf(e.pub.Current(), "x", "jenkins")
	if v.V.State != integrations.StateDisabled {
		t.Fatalf("state = %v", v.V.State)
	}
	if _, ok := s.FactFor(e.pub.Current(), "x", "jenkins", time.Now()); ok {
		t.Fatal("fact for a disabled provider must be unavailable")
	}
}

func TestJobsSpecs(t *testing.T) {
	e := newEnv(t, nil, nil, nil)
	s := svc(t, e, jcfg(), nil, nil, &fakeProvider{name: "jenkins", fn: okStatus("")})
	js := s.Jobs()
	if len(js) != 1 {
		t.Fatalf("jobs = %d", len(js))
	}
	j := js[0]
	if got := s.Providers(); len(got) != 1 || got[0] != "jenkins" {
		t.Fatalf("Providers = %v", got)
	}
	if j.Kind != "integrations.jenkins" || j.Upstream != "jenkins" || j.Scope != job.ScopeGlobal || j.Policy.TTL != 5*time.Minute || j.Timeout != 4*time.Minute {
		t.Fatalf("spec = %+v", j)
	}
	if k := job.NewKey(j.Kind, "_"); k != "integrations.jenkins/_" {
		t.Fatalf("key = %q", k)
	}
}

func TestSweepStaleWhileErrorAndRevalidate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha", "beta")
		e := newEnv(t, repos, nil, nil)
		ms := newMemStore()
		var failBeta bool
		fp := &fakeProvider{name: "jenkins"}
		fp.fn = func(tg integrations.Target) (integrations.Status, error) {
			if tg.Repo.Name == "Beta" && failBeta {
				return integrations.Status{}, errors.New("connection reset")
			}
			return integrations.Status{State: integrations.StateOK, Summary: "build #1 passed"}, nil
		}
		s := svc(t, e, jcfg(), ms, ov, fp)
		ctx := context.Background()
		out, err := s.Sweep(ctx, "jenkins")
		if err != nil || out.Unchanged {
			t.Fatalf("first sweep: %+v %v", out, err)
		}
		a := s.StatusOf(e.pub.Current(), "alpha", "jenkins")
		if a.V.State != integrations.StateOK || a.Info.State != freshness.StateOK || a.V.Provider != "jenkins" {
			t.Fatalf("alpha = %+v", a)
		}
		// second sweep, same answers: unchanged outcome, store sees revalidations not rewrites
		time.Sleep(5 * time.Minute)
		ms.ops = nil
		out, err = s.Sweep(ctx, "jenkins")
		if err != nil || !out.Unchanged {
			t.Fatalf("second sweep: %+v %v", out, err)
		}
		if strings.Join(ms.ops, ",") != "reval,reval" {
			t.Fatalf("ops = %v", ms.ops)
		}
		// beta fails: previous good value is kept, info flips to error with a code, freshness derives stale
		failBeta = true
		time.Sleep(5 * time.Minute)
		ms.ops = nil
		if _, err = s.Sweep(ctx, "jenkins"); err != nil {
			t.Fatalf("a single transient failure is a per-target result, not a sweep error: %v", err)
		}
		b := s.StatusOf(e.pub.Current(), "beta", "jenkins")
		if b.V.State != integrations.StateOK || b.Info.State != freshness.StateError || b.Info.ErrCode != "transient" || b.Info.Attempts != 1 {
			t.Fatalf("beta = %+v", b)
		}
		if !slices.Contains(ms.ops, "fail:transient") {
			t.Fatalf("ops = %v", ms.ops)
		}
		if got := b.Info.Derive(time.Now()); got != freshness.StatusStale {
			t.Fatalf("derive = %v", got)
		}
		time.Sleep(time.Hour)
		if got := b.Info.Derive(time.Now()); got != freshness.StatusStale {
			t.Fatalf("derive after an hour = %v", got)
		}
		if f, _ := s.FactFor(e.pub.Current(), "beta", "jenkins", time.Now()); !f.Stale || f.Tone != integrations.ToneNeutral || f.Message != "passing" {
			t.Fatalf("fact = %+v", f)
		}
	})
}

func TestSweepNotFoundCarry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha")
		e := newEnv(t, repos, nil, nil)
		fp := &fakeProvider{name: "jenkins", fn: func(integrations.Target) (integrations.Status, error) {
			return integrations.Status{}, fmt.Errorf("job not found: %w", scm.ErrNotFound)
		}}
		s := svc(t, e, jcfg(), newMemStore(), ov, fp)
		if _, err := s.Sweep(context.Background(), "jenkins"); err != nil {
			t.Fatal(err)
		}
		v := s.StatusOf(e.pub.Current(), "alpha", "jenkins")
		if v.Info.State != freshness.StateNotFound || v.V.State != integrations.StateUnknown {
			t.Fatalf("v = %+v", v)
		}
		time.Sleep(5 * time.Minute)
		s.Sweep(context.Background(), "jenkins")
		if fp.n != 1 {
			t.Fatalf("404 job probed again within not_found_ttl: %d calls", fp.n)
		}
		time.Sleep(time.Hour)
		s.Sweep(context.Background(), "jenkins")
		if fp.n != 2 {
			t.Fatalf("404 job not re-probed after not_found_ttl: %d calls", fp.n)
		}
	})
}

func TestSweepAbortsInstanceAfterConsecutiveFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		names := []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10"}
		ov, repos := declared(names...)
		e := newEnv(t, repos, nil, nil)
		cfg := jcfg()
		cfg.Jenkins.Concurrency, cfg.Jenkins.AbortAfter = 1, 3
		fp := &fakeProvider{name: "jenkins", fn: func(integrations.Target) (integrations.Status, error) {
			return integrations.Status{}, errors.New("dial tcp: i/o timeout")
		}}
		s := svc(t, e, cfg, newMemStore(), ov, fp)
		out, err := s.Sweep(context.Background(), "jenkins")
		_ = out
		if err == nil || !strings.Contains(err.Error(), "unavailable after 3 consecutive failures") {
			t.Fatalf("err = %v", err)
		}
		if fp.n != 3 {
			t.Fatalf("provider called %d times after the abort threshold", fp.n)
		}
		// the remaining targets are recorded as aborted, not silently dropped
		v := s.StatusOf(e.pub.Current(), "a9", "jenkins")
		if v.Info.State != freshness.StateError || v.Info.ErrCode != "aborted" {
			t.Fatalf("a9 = %+v", v)
		}
		if h := s.Health()[0]; h.LastError != "transient" && h.LastError == "" {
			t.Fatalf("health = %+v", h)
		}
	})
}

func TestSweepErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		perm    bool
		rateLim bool
	}{
		{"auth", &integrations.PermanentError{Provider: "jenkins", Code: "auth", Err: errors.New("403")}, true, false},
		{"rate limit", &integrations.RateLimitError{Provider: "jenkins", At: time.Now().Add(time.Minute), Err: errors.New("429")}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ov, repos := declared("alpha", "beta", "gamma")
				e := newEnv(t, repos, nil, nil)
				fp := &fakeProvider{name: "jenkins", fn: func(integrations.Target) (integrations.Status, error) { return integrations.Status{}, tc.err }}
				cfg := jcfg()
				cfg.Jenkins.Concurrency = 1
				s := svc(t, e, cfg, newMemStore(), ov, fp)
				_, err := s.Sweep(context.Background(), "jenkins")
				var p job.Permanent
				var rl job.RateLimited
				if got := errors.As(err, &p); got != tc.perm {
					t.Errorf("job.Permanent match = %v", got)
				}
				if got := errors.As(err, &rl); got != tc.rateLim {
					t.Errorf("job.RateLimited match = %v", got)
				}
				if fp.n != 1 {
					t.Errorf("auth/rate-limit must stop the sweep after the first call, got %d", fp.n)
				}
			})
		})
	}
}

// A transient error must NOT satisfy the engine's behaviour interfaces (otherwise every blip would park or pause the upstream).
func TestTransientErrorHasNoBehaviour(t *testing.T) {
	var err error = fmt.Errorf("x: %w", errors.New("boom"))
	var p job.Permanent
	var rl job.RateLimited
	if errors.As(err, &p) || errors.As(err, &rl) {
		t.Fatal("plain error matched a behaviour interface")
	}
}

func TestTargetLostIsRemoved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha", "beta")
		e := newEnv(t, repos, nil, nil)
		s := svc(t, e, jcfg(), newMemStore(), ov, &fakeProvider{name: "jenkins", fn: okStatus("")})
		s.Sweep(context.Background(), "jenkins")
		delete(ov, "beta") // descriptor edited: beta no longer declares a job
		out, err := s.Sweep(context.Background(), "jenkins")
		if err != nil || out.Unchanged {
			t.Fatalf("%+v %v", out, err)
		}
		if v := s.StatusOf(e.pub.Current(), "beta", "jenkins"); v.Info.State != freshness.StateNever {
			t.Fatalf("beta still has a status: %+v", v)
		}
		if v := s.StatusOf(e.pub.Current(), "alpha", "jenkins"); v.V.State != integrations.StateOK {
			t.Fatalf("alpha = %+v", v)
		}
	})
}

// A repository removed by a concurrent reconcile between resolve and publish must not fail the publish or resurrect the repo.
func TestRemovedRepoDuringSweep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha", "beta")
		e := newEnv(t, repos, nil, nil)
		var s *integrations.Service
		fp := &fakeProvider{name: "jenkins"}
		fp.fn = func(tg integrations.Target) (integrations.Status, error) {
			if tg.Repo.Name == "Alpha" {
				e.pub.Apply(func(b *readmodel.Builder) error { b.Remove("beta"); return nil })
			}
			return integrations.Status{State: integrations.StateOK, Summary: "ok"}, nil
		}
		cfg := jcfg()
		cfg.Jenkins.Concurrency = 1
		s = svc(t, e, cfg, newMemStore(), ov, fp)
		if _, err := s.Sweep(context.Background(), "jenkins"); err != nil {
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(e.pub.Current(), "beta", e.repo); ok {
			t.Fatal("beta was resurrected")
		}
		if v := s.StatusOf(e.pub.Current(), "alpha", "jenkins"); v.V.State != integrations.StateOK {
			t.Fatalf("alpha = %+v", v)
		}
	})
}

type bulkFake struct {
	fakeProvider
	rs func(ts []integrations.Target) ([]integrations.Result, error)
}

func (b *bulkFake) FetchBulk(_ context.Context, ts []integrations.Target) ([]integrations.Result, error) {
	return b.rs(ts)
}

// Two providers publishing for the same repository concurrently must never lose each other's entry (lost-update test, run with -race).
func TestConcurrentProvidersMerge(t *testing.T) {
	ov, repos := declared("alpha", "beta", "gamma", "delta")
	e := newEnv(t, repos, nil, nil)
	cfg := jcfg()
	cfg.Snyk.Enabled, cfg.Snyk.Token, cfg.Snyk.OrgID = true, secret.New("t"), "4a18d42f-0706-4ad0-b127-24078731fbed"
	jp := &fakeProvider{name: "jenkins", fn: okStatus("")}
	sp := &bulkFake{fakeProvider: fakeProvider{name: "snyk"}}
	sp.rs = func(ts []integrations.Target) ([]integrations.Result, error) {
		out := make([]integrations.Result, len(ts))
		for i := range ts {
			out[i].Status = integrations.Status{State: integrations.StateOK, Summary: "no known issues", Counts: map[string]int{}}
		}
		return out, nil
	}
	s := svc(t, e, cfg, newMemStore(), ov, jp, sp)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.Sweep(context.Background(), "jenkins") }()
		go func() { defer wg.Done(); s.Sweep(context.Background(), "snyk") }()
	}
	wg.Wait()
	for _, slug := range []scm.Slug{"alpha", "beta", "gamma", "delta"} {
		for _, p := range []string{"jenkins", "snyk"} {
			if v := s.StatusOf(e.pub.Current(), slug, p); v.V.State != integrations.StateOK {
				t.Fatalf("%s/%s lost: %+v", slug, p, v)
			}
		}
	}
}

func TestBulkWrongLengthIsProtocolError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, []scm.Repository{repo("Alpha", 0)}, nil, nil)
		cfg := integrations.DefaultConfig()
		cfg.Snyk.Enabled, cfg.Snyk.Token, cfg.Snyk.OrgID = true, secret.New("t"), "4a18d42f-0706-4ad0-b127-24078731fbed"
		sp := &bulkFake{fakeProvider: fakeProvider{name: "snyk"}}
		sp.rs = func(ts []integrations.Target) ([]integrations.Result, error) { return nil, nil }
		s := svc(t, e, cfg, newMemStore(), nil, sp)
		_, err := s.Sweep(context.Background(), "snyk")
		if !errors.Is(err, integrations.ErrProtocol) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestBulkNoDataIsNoContent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, []scm.Repository{repo("Alpha", 0)}, nil, nil)
		cfg := integrations.DefaultConfig()
		cfg.Snyk.Enabled, cfg.Snyk.Token, cfg.Snyk.OrgID = true, secret.New("t"), "4a18d42f-0706-4ad0-b127-24078731fbed"
		sp := &bulkFake{fakeProvider: fakeProvider{name: "snyk"}}
		sp.rs = func(ts []integrations.Target) ([]integrations.Result, error) {
			return []integrations.Result{{Err: integrations.ErrNoData}}, nil
		}
		s := svc(t, e, cfg, newMemStore(), nil, sp)
		if _, err := s.Sweep(context.Background(), "snyk"); err != nil {
			t.Fatal(err)
		}
		if v := s.StatusOf(e.pub.Current(), "alpha", "snyk"); v.Info.State != freshness.StateNoContent {
			t.Fatalf("v = %+v", v)
		}
	})
}

func TestWarmItemRoundTrip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha")
		e := newEnv(t, repos, nil, nil)
		ms := newMemStore()
		s := svc(t, e, jcfg(), ms, ov, &fakeProvider{name: "jenkins", fn: okStatus("")})
		if _, err := s.Sweep(context.Background(), "jenkins"); err != nil {
			t.Fatal(err)
		}
		var item store.Item
		for _, it := range ms.items {
			item = it
		}
		// a fresh process: new publisher with only the existence slot, then WarmItem
		e2 := newEnv(t, repos, nil, nil)
		s2 := svc(t, e2, jcfg(), newMemStore(), ov, &fakeProvider{name: "jenkins", fn: okStatus("")})
		_, err := e2.pub.Apply(func(b *readmodel.Builder) error { return s2.WarmItem(b, "alpha", item) })
		if err != nil {
			t.Fatal(err)
		}
		v := s2.StatusOf(e2.pub.Current(), "alpha", "jenkins")
		if v.V.State != integrations.StateOK || v.V.Summary != "build #1 passed" || v.Info.State != freshness.StateOK {
			t.Fatalf("restored = %+v", v)
		}
		// corrupt and foreign rows are skipped, never fatal
		bad := item
		bad.Payload = []byte(`{"schema":99}`)
		foreign := item
		foreign.Key.Kind = "docs.readme"
		_, err = e2.pub.Apply(func(b *readmodel.Builder) error {
			if err := s2.WarmItem(b, "alpha", bad); err != nil {
				return err
			}
			return s2.WarmItem(b, "alpha", foreign)
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestSummaryCap(t *testing.T) {
	if got := integrations.CapSummary(strings.Repeat("é", 500)); len([]rune(got)) != 120 {
		t.Fatalf("len = %d", len([]rune(got)))
	}
}

func TestValidateGroupDefaults(t *testing.T) {
	cfg := jcfg()
	g := func(prov, k, v string) []scm.GroupMeta {
		return []scm.GroupMeta{{ID: "g1", Integrations: map[string]map[string]string{prov: {k: v}}}}
	}
	tests := []struct {
		name   string
		groups []scm.GroupMeta
		want   string
	}{
		{"prefix ok", g("jenkins", "job_prefix", "Team-"), ""},
		{"folder prefix ok", g("jenkins", "job_prefix", "org/"), ""},
		{"empty prefix ok", g("jenkins", "job_prefix", ""), ""},
		{"traversal", g("jenkins", "job_prefix", "../"), "job path"},
		{"unknown instance", g("jenkins", "instance", "nope"), "unknown instance"},
		{"known instance", g("jenkins", "instance", "main"), ""},
		{"unknown key", g("jenkins", "job_name", "x"), "unknown integration key"},
		{"snyk not configured", g("snyk", "project_prefix", "x-"), "not configured"},
		{"unknown provider", g("circleci", "x", "y"), "unknown integration key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := cfg.ValidateGroupDefaults(tc.groups)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A sweep cut short by its deadline still publishes what it learned.
func TestDeadlineStillPublishes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha", "beta", "gamma")
		e := newEnv(t, repos, nil, nil)
		cfg := jcfg()
		cfg.Jenkins.Concurrency = 1
		fp := &fakeProvider{name: "jenkins"}
		fp.fn = func(tg integrations.Target) (integrations.Status, error) {
			if tg.Repo.Name == "Beta" {
				time.Sleep(time.Hour) // never returns before the deadline
			}
			return integrations.Status{State: integrations.StateOK, Summary: "ok"}, nil
		}
		s := svc(t, e, cfg, newMemStore(), ov, fp)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, err := s.Sweep(ctx, "jenkins")
		if err == nil {
			t.Fatal("expected a deadline error for the engine to back off on")
		}
		if v := s.StatusOf(e.pub.Current(), "alpha", "jenkins"); v.V.State != integrations.StateOK {
			t.Fatalf("alpha = %+v", v)
		}
		if v := s.StatusOf(e.pub.Current(), "gamma", "jenkins"); v.Info.State != freshness.StateError {
			t.Fatalf("gamma = %+v", v)
		}
	})
}

// GR-14: the embedded default points at no company and enables nothing.
func TestDefaultConfigHasNoForeignHosts(t *testing.T) {
	c := integrations.DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.JenkinsEnabled() || c.SnykEnabled() || len(c.Jenkins.Instances) != 0 {
		t.Fatal("default must enable nothing")
	}
	dump := fmt.Sprintf("%+v", c)
	for _, bad := range []string{"jenkins.", "ci.", ".internal", ".local", "snyk.io/test"} {
		if strings.Contains(dump, bad) {
			t.Errorf("default config mentions %q: %s", bad, dump)
		}
	}
	// the only host in the default is Snyk's documented US region, and it is inert while disabled
	if !strings.Contains(dump, "https://api.snyk.io") {
		t.Error("unexpected default snyk base_url")
	}
}
