package overview_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/overview"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// ---- authorizers ----

// userAuthz models access mode "trusted proxy": an identified principal sees everything, an anonymous one only public.
type userAuthz struct{}

func (userAuthz) CanView(_ context.Context, p authz.Principal, v scm.Visibility) bool {
	return v == scm.VisPublic || !p.IsAnonymous()
}

// asUser sets the principal from the X-User header, as the access middleware of unit 14 does.
func asUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := r.Header.Get("X-User"); u != "" {
			r = r.WithContext(authz.WithPrincipal(r.Context(), authz.NewPrincipal(u, nil)))
		}
		next.ServeHTTP(w, r)
	})
}

// ---- fixture sources ----

type repoSpec struct {
	slug, name string
	vis        scm.Visibility
	archived   bool
	desc       string
	topics     []string
	pushed     time.Time
	fetched    time.Time // zero: never fetched
	failing    bool
	meta       *overview.MetaView
	docs       []overview.DocEntry
	bodies     map[string]string // doc id -> normalised body
	commits    []scm.Commit
	stats      *scm.RepoStats
	groups     []string
}

type groupSpec struct {
	id, name, desc string
	members        []string
	dflt           string
}

type fakeDocs struct {
	meta   map[scm.Slug]overview.MetaView
	idx    map[scm.Slug][]overview.DocEntry
	info   map[scm.Slug]freshness.Info
	bodies map[scm.Slug]map[string]string
	panics bool
}

func (f *fakeDocs) Meta(_ *readmodel.Snapshot, s scm.Slug) (overview.MetaView, bool) {
	if f.panics {
		panic("boom")
	}
	m, ok := f.meta[s]
	return m, ok
}
func (f *fakeDocs) Index(_ *readmodel.Snapshot, s scm.Slug) ([]overview.DocEntry, freshness.Info, bool) {
	e, ok := f.idx[s]
	return e, f.info[s], ok
}
func (f *fakeDocs) Body(_ context.Context, s scm.Slug, id string) (string, bool) {
	b, ok := f.bodies[s][id]
	return b, ok
}

type fakeCommits struct {
	by   map[scm.Slug][]scm.Commit
	info map[scm.Slug]freshness.Info
}

func (f *fakeCommits) Recent(_ *readmodel.Snapshot, s scm.Slug, n int) ([]scm.Commit, freshness.Info, bool) {
	c, ok := f.by[s]
	if !ok {
		return nil, freshness.Info{}, false
	}
	if len(c) > n {
		c = c[:n]
	}
	return c, f.info[s], true
}

type fakeManifests struct {
	on bool
}

func (f fakeManifests) Enabled() bool { return f.on }
func (f fakeManifests) Technology(*readmodel.Snapshot, scm.Slug) (overview.TechView, bool) {
	return overview.TechView{}, false
}

type fakeInteg struct {
	providers []string
	st        map[string]freshness.Value[overview.ProviderStatusView] // key provider/slug
}

func (f *fakeInteg) Providers() []string { return f.providers }
func (f *fakeInteg) StatusOf(_ *readmodel.Snapshot, s scm.Slug, p string) freshness.Value[overview.ProviderStatusView] {
	if v, ok := f.st[p+"/"+string(s)]; ok {
		return v
	}
	return freshness.Value[overview.ProviderStatusView]{V: overview.ProviderStatusView{Provider: p, State: "unknown"}}
}

type fakeBadges struct{}

func (fakeBadges) For(s scm.Slug, providers []string) []openapi.Badge {
	return []openapi.Badge{{Kind: "issues", Url: "/badges/" + string(s) + "/issues.svg", Label: "open issues"}}
}

type fakePages struct {
	slugs []string
	ann   *overview.AnnouncementView
}

func (f *fakePages) Announcement() (overview.AnnouncementView, bool) {
	if f.ann == nil {
		return overview.AnnouncementView{}, false
	}
	return *f.ann, true
}
func (f *fakePages) Pages(*readmodel.Snapshot) []overview.PageEntry {
	var out []overview.PageEntry
	for _, s := range f.slugs {
		out = append(out, overview.PageEntry{Slug: s, UpdatedAt: now.Add(-time.Hour)})
	}
	return out
}

// ---- the world ----

type world struct {
	t       *testing.T
	reg     *readmodel.Registry
	sl      overview.Slots
	pub     *readmodel.Publisher
	docs    *fakeDocs
	commits *fakeCommits
	integ   *fakeInteg
	pages   *fakePages
	svc     *overview.Service
	h       http.Handler
	cfg     overview.Config
	az      authz.Authorizer
	manif   *fakeManifests
	now     time.Time
}

type opts struct {
	cfg     func(*overview.Config)
	az      authz.Authorizer
	private bool
	noURL   bool // server.public.public_url unset
	meter   metric.Meter
}

func newWorld(t *testing.T, repos []repoSpec, groups []groupSpec, aliases map[scm.Slug]scm.Slug, o opts) *world {
	t.Helper()
	w := &world{t: t, reg: &readmodel.Registry{}, now: now,
		docs:    &fakeDocs{meta: map[scm.Slug]overview.MetaView{}, idx: map[scm.Slug][]overview.DocEntry{}, info: map[scm.Slug]freshness.Info{}, bodies: map[scm.Slug]map[string]string{}},
		commits: &fakeCommits{by: map[scm.Slug][]scm.Commit{}, info: map[scm.Slug]freshness.Info{}},
		integ:   &fakeInteg{providers: []string{"jenkins"}, st: map[string]freshness.Value[overview.ProviderStatusView]{}},
		pages:   &fakePages{slugs: []string{"about"}},
		manif:   &fakeManifests{on: true}, cfg: overview.DefaultConfig(), az: o.az}
	if w.az == nil {
		w.az = authz.PublicOnly{}
	}
	w.cfg.Site.Title = "Acme Portal"
	w.cfg.Site.Description = "Repositories of Acme."
	w.cfg.Site.Nav = []overview.NavEntry{{Label: "About", Page: "about"}, {Label: "Gone", Page: "missing"}, {Label: "Status", URL: "https://status.acme.example"}}
	if o.cfg != nil {
		o.cfg(&w.cfg)
	}
	w.sl = overview.Slots{
		Repos:      readmodel.Register[freshness.Value[scm.Repository]](w.reg, "catalog.repo", readmodel.Existence()),
		Stats:      readmodel.Register[freshness.Value[scm.RepoStats]](w.reg, "catalog.stats"),
		Membership: readmodel.RegisterGlobal[scm.Membership](w.reg, "catalog.membership"),
		Aliases:    readmodel.RegisterGlobal[map[scm.Slug]scm.Slug](w.reg, "catalog.aliases"),
		Site:       overview.RegisterSlots(w.reg),
	}
	w.pub = readmodel.NewPublisher(w.reg, []readmodel.Projector{overview.SiteProjector(w.sl, func(*readmodel.Snapshot) time.Time { return now.Add(-10 * time.Minute) })}, nil, readmodel.Deps{})
	w.publish(repos, groups, aliases)

	pu, _ := url.Parse("https://portal.acme.example")
	if o.noURL {
		pu = nil
	}
	svc, err := overview.New(w.cfg, overview.Deps{
		Meter: o.meter, Snapshots: w.pub, Slots: w.sl, Authz: w.az, Docs: w.docs, Commits: w.commits, Manifests: w.manif, Integrations: w.integ,
		Badges: fakeBadges{}, Pages: w.pages, Build: openapi.BuildInfo{Version: "1.0.0", Commit: "abc1234", BuildId: "abc1234"},
		PublicURL: pu, Private: o.private,
		Readme: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			fmt.Fprintf(w, "README of %s\n", r.PathValue("repo"))
		}),
		Shell: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html>shell") }),
		ShellNotFound: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
			fmt.Fprint(w, "<!doctype html>shell404")
		}),
		Now: func() time.Time { return w.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	w.svc = svc
	mux := http.NewServeMux()
	svc.Mount(mux)
	// webapp's explicit SPA routes: they beat the catch-all, which is why the alias check is a middleware
	for _, p := range []string{"GET /repos/{repo}/commits", "GET /repos/{repo}/dependencies", "GET /repos/{repo}/readme.md", "GET /api/v1/repos/{repo}/docs"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "explicit "+r.URL.Path) })
	}
	w.h = asUser(svc.AliasRedirect(mux))
	return w
}

func (w *world) publish(repos []repoSpec, groups []groupSpec, aliases map[scm.Slug]scm.Slug) {
	t := w.t
	t.Helper()
	var mr []scm.MemberRepo
	slugGroups := map[string][]string{}
	for _, g := range groups {
		for _, m := range g.members {
			slugGroups[m] = append(slugGroups[m], g.id)
		}
	}
	_, err := w.pub.Apply(func(b *readmodel.Builder) error {
		for _, r := range repos {
			ref := scm.RepoRef{Source: "gh", Owner: "acme", Name: r.name}
			repo := scm.Repository{Ref: ref, Slug: scm.Slug(r.slug), Description: r.desc, DefaultBranch: "main", Topics: r.topics, Visibility: r.vis,
				Archived: r.archived, HTMLURL: "https://github.com/acme/" + r.name, PushedAt: r.pushed}
			info := freshness.Info{State: freshness.StateOK, FetchedAt: r.fetched, CheckedAt: r.fetched, TTL: 30 * time.Minute}
			if r.fetched.IsZero() {
				info = freshness.Info{State: freshness.StateNever}
			}
			if r.failing {
				info.State, info.ErrCode, info.Attempts = freshness.StateError, "github.unavailable", 3
			}
			if err := readmodel.Put(b, readmodel.EntryID(r.slug), w.sl.Repos, freshness.Value[scm.Repository]{V: repo, Info: info}); err != nil {
				return err
			}
			if r.stats != nil {
				if err := readmodel.Put(b, readmodel.EntryID(r.slug), w.sl.Stats, freshness.Value[scm.RepoStats]{V: *r.stats, Info: info}); err != nil {
					return err
				}
			}
			mr = append(mr, scm.MemberRepo{Slug: scm.Slug(r.slug), Ref: ref, Visibility: r.vis})
			if r.meta != nil {
				w.docs.meta[scm.Slug(r.slug)] = *r.meta
			}
			if r.docs != nil {
				w.docs.idx[scm.Slug(r.slug)] = r.docs
				w.docs.info[scm.Slug(r.slug)] = freshness.Info{State: freshness.StateOK, FetchedAt: now.Add(-5 * time.Minute), CheckedAt: now.Add(-5 * time.Minute), TTL: time.Hour}
			}
			if r.bodies != nil {
				w.docs.bodies[scm.Slug(r.slug)] = r.bodies
			}
			if r.commits != nil {
				w.commits.by[scm.Slug(r.slug)] = r.commits
				w.commits.info[scm.Slug(r.slug)] = freshness.Info{State: freshness.StateOK, FetchedAt: now.Add(-time.Minute), CheckedAt: now.Add(-time.Minute), TTL: 30 * time.Minute}
			}
		}
		spec := scm.MembershipSpec{Repos: mr}
		for _, g := range groups {
			var ms []scm.Slug
			for _, m := range g.members {
				ms = append(ms, scm.Slug(m))
			}
			sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
			spec.Groups = append(spec.Groups, scm.GroupSpec{Meta: scm.GroupMeta{ID: scm.GroupID(g.id), DisplayName: g.name, Description: g.desc}, Members: ms, Default: scm.Slug(g.dflt)})
		}
		mem, err := scm.NewMembership(spec)
		if err != nil {
			return err
		}
		if err := readmodel.PutGlobal(b, w.sl.Membership, mem); err != nil {
			return err
		}
		if aliases == nil {
			aliases = map[scm.Slug]scm.Slug{}
		}
		return readmodel.PutGlobal(b, w.sl.Aliases, aliases)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// do issues a request through the handler stack and returns the recorder.
func (w *world) do(method, target string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	return rec
}

func (w *world) get(target string, hdr ...string) *httptest.ResponseRecorder {
	return w.do("GET", target, hdr...)
}

// std is the small catalogue most tests use.
func std(t *testing.T, o opts) *world {
	readme := func(slug string) overview.DocEntry {
		return overview.DocEntry{Kind: overview.DocReadme, ID: "readme", Path: "README.md", Title: "Overview", Format: "markdown", State: "resident",
			BlobSHA: strings.Repeat("a", 40), UpdatedAt: now.Add(-48 * time.Hour), SourceURL: "https://github.com/acme/" + slug + "/blob/main/README.md"}
	}
	doc := func(slug, p string) overview.DocEntry {
		return overview.DocEntry{Kind: overview.DocDoc, ID: strings.ReplaceAll(p, "/", "~"), Path: p, Title: "Install", Description: "How to install", Format: "markdown", State: "resident",
			BlobSHA: strings.Repeat("b", 40), UpdatedAt: now.Add(-24 * time.Hour), SourceURL: "https://github.com/acme/" + slug + "/blob/main/" + p}
	}
	c1 := scm.Commit{SHA: strings.Repeat("c", 40), Subject: "Fix billing", AuthorLogin: "ola", AuthorName: "Ola", CommittedAt: now.Add(-3 * time.Hour), URL: "https://github.com/acme/billing/commit/ccc"}
	fresh := now.Add(-10 * time.Minute)
	repos := []repoSpec{
		{slug: "billing", name: "Billing", vis: scm.VisPublic, desc: "Billing service", topics: []string{"java", "payments"}, pushed: now.Add(-2 * time.Hour), fetched: fresh,
			stats: &scm.RepoStats{OpenIssues: 4, Stars: 9, License: "Apache-2.0", HeadSHA: strings.Repeat("d", 40)}, commits: []scm.Commit{c1},
			meta: &overview.MetaView{Title: &overview.Sourced{V: "Billing Service", Source: "descriptor"}, Summary: &overview.Sourced{V: "Invoices and payments", Source: "descriptor"},
				Type: &overview.Sourced{V: "service", Source: "descriptor"}, Lifecycle: &overview.Sourced{V: "production", Source: "descriptor"},
				Owners: []overview.OwnerView{{Handle: "@acme/billing", Kind: "team", Source: "codeowners"}}},
			docs: []overview.DocEntry{readme("Billing"), doc("Billing", "docs/install.md"),
				{Kind: overview.DocAgents, ID: "agents", Path: "AGENTS.md", Title: "AGENTS.md", Format: "markdown", State: "resident", BlobSHA: strings.Repeat("e", 40)}},
			bodies: map[string]string{"readme": "# Billing\n\nHello.", "docs~install.md": "Install it.", "agents": "Agents."}},
		{slug: "core-lib", name: "core-lib", vis: scm.VisPublic, desc: "Core library", topics: []string{"java"}, pushed: now.Add(-30 * time.Hour), fetched: fresh,
			stats: &scm.RepoStats{OpenIssues: 1}, docs: []overview.DocEntry{readme("core-lib")}, bodies: map[string]string{"readme": "Core."},
			meta: &overview.MetaView{Lifecycle: &overview.Sourced{V: "experimental", Source: "descriptor"}}},
		{slug: "old-thing", name: "old-thing", vis: scm.VisPublic, archived: true, pushed: now.Add(-9000 * time.Hour), fetched: fresh, meta: &overview.MetaView{Lifecycle: &overview.Sourced{V: "deprecated", Source: "descriptor"}}},
		{slug: "secret-tool", name: "secret-tool", vis: scm.VisPrivate, desc: "Internal tool", pushed: now.Add(-time.Hour), fetched: fresh},
		{slug: "hidden-one", name: "hidden-one", vis: scm.VisPublic, pushed: now.Add(-time.Hour), fetched: fresh, meta: &overview.MetaView{Hidden: true}},
	}
	groups := []groupSpec{
		{id: "platform", name: "Platform", desc: "Shared platform", members: []string{"billing", "core-lib", "hidden-one"}, dflt: "core-lib"},
		{id: "internal", name: "Internal", desc: "Only private", members: []string{"secret-tool"}},
		{id: "archive", name: "Archive", members: []string{"old-thing"}},
	}
	return newWorld(t, repos, groups, map[scm.Slug]scm.Slug{"billing-svc": "billing", "secret-old": "secret-tool"}, o)
}
