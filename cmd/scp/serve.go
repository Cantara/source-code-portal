package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	contract "github.com/cantara/source-code-portal/api"
	"github.com/cantara/source-code-portal/internal/announce"
	"github.com/cantara/source-code-portal/internal/assistant"
	"github.com/cantara/source-code-portal/internal/auth"
	"github.com/cantara/source-code-portal/internal/badges"
	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/cluster"
	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/docs"
	"github.com/cantara/source-code-portal/internal/egress"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/issues"
	"github.com/cantara/source-code-portal/internal/layout"
	"github.com/cantara/source-code-portal/internal/llms"
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/mcpserver"
	"github.com/cantara/source-code-portal/internal/packages"
	"github.com/cantara/source-code-portal/internal/pulls"
	"github.com/cantara/source-code-portal/internal/releases"
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/search"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/tools"
	"github.com/cantara/source-code-portal/internal/webhook"
	"github.com/cantara/source-code-portal/internal/webui"
)

const (
	publishDebounce = 200 * time.Millisecond
	publishMaxDelay = time.Second
)

func newServeCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the portal until SIGINT or SIGTERM",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := loadConfig(cmd, opts)
			if err != nil {
				return err
			}
			log := newLogger(cfg.Log, cmd.ErrOrStderr())
			srv, err := newServer(cmd.Context(), cfg, deps{}, log)
			if err != nil {
				return failure{err}
			}
			if err := srv.run(cmd.Context()); err != nil {
				return failure{err}
			}
			return nil
		},
	}
	cmd.Flags().String("public-addr", "", "public listener address (default :9090)")
	cmd.Flags().String("admin-addr", "", "admin listener address (default :9091)")
	return cmd
}

// api composes the strict server from the features that implement its methods. The aliases give the embedded fields distinct names.
type (
	catalogAPI   = catalog.API
	commitsAPI   = commits.API
	pullsAPI     = pulls.API
	issuesAPI    = issues.API
	releasesAPI  = releases.API
	docsAPI      = docs.API
	manifestsAPI = manifests.API
	layoutAPI    = layout.API
	toolsAPI     = tools.API
	searchAPI    = search.API
	api          struct {
		catalogAPI
		commitsAPI
		pullsAPI
		issuesAPI
		releasesAPI
		docsAPI
		manifestsAPI
		layoutAPI
		httpapi.SnapshotAPI
		httpapi.MeAPI
		*assistant.Assistant
		toolsAPI
		searchAPI
		rulesAPI
		*prereqs
	}
)

// server owns the store, the snapshot, the sync engine and the two listeners.
type server struct {
	log               *slog.Logger
	grace             time.Duration
	store             *store.DB
	inbox             *inbox // nil without a webhook secret
	pub               *snapshot.Publisher
	engine            *sync.Engine
	member            *cluster.Member // nil when the instance is alone
	web               *webui.Server   // nil without web.dir
	layouts           *layout.Store
	notices           *announce.Notices
	prereqs           *prereqs
	stopping          atomic.Bool
	public, admin     *http.Server
	publicLn, adminLn net.Listener
}

// deps are the outside services; the zero value is the real ones.
type deps struct {
	github http.RoundTripper // transport to GitHub
	images http.RoundTripper // transport to external images (egress.Guarded when nil)
	// registries is the transport to package registries (egress.Guarded when nil) and public the public registries' addresses by
	// ecosystem in its place (the providers' own when empty): the tests' loopback registries.
	registries http.RoundTripper
	public     map[string]string
	cluster    func(context.Context) (cluster.Backend, error) // dial for the cluster when cluster.addresses is set
}

// newServer opens the store, wires the features and binds both listeners.
func newServer(ctx context.Context, cfg config.Config, d deps, log *slog.Logger) (_ *server, err error) {
	gh, err := github.New(cfg.GitHub, d.github, log)
	if err != nil {
		return nil, err
	}
	sources := map[string]catalog.GitHub{}
	fetchers := map[string]commits.Fetcher{}
	trees := map[string]docs.GitHub{}
	manifestTrees := map[string]manifests.GitHub{}
	packageTrees := map[string]packages.GitHub{}
	releaseFetchers := map[string]releases.Fetcher{}
	openPulls := map[string]pulls.Fetcher{}
	issueFetchers := map[string]issues.Fetcher{}
	ids := make([]string, 0, len(cfg.GitHub.Sources))
	for _, s := range cfg.GitHub.Sources {
		src := gh.Source(s.ID)
		sources[s.ID], trees[s.ID], manifestTrees[s.ID], packageTrees[s.ID] = src, src, src, src
		fetchers[s.ID], releaseFetchers[s.ID], openPulls[s.ID] = commits.FromGitHub(src), releases.FromGitHub(src), pulls.FromGitHub(src)
		issueFetchers[s.ID] = issues.FromGitHub(src)
		ids = append(ids, s.ID)
	}

	f := cfg.Features // a switched-off feature is absent: no job, index, route, tool or page (design/features.md)
	indexers := []snapshot.Indexer{commits.Feeds(), manifests.Graph(), search.Indexer(searchOptions())}
	if f.Pulls {
		indexers = append(indexers, pulls.Feeds())
	}
	if f.Issues {
		indexers = append(indexers, issues.Feeds())
	}
	s := &server{log: log, grace: cfg.Server.ShutdownGrace, pub: snapshot.NewPublisher(publishDebounce, publishMaxDelay, log, indexers...)}
	if s.store, err = store.Open(ctx, cfg.Store.Path, log); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = s.store.Close()
		}
	}()
	s.engine = sync.New(s.store, s.pub, gh, ids, cfg.Sync.Concurrency, log)
	bi := readBuildInfo()
	s.engine.Release(bi.Version + "+" + bi.Build) // a new build runs failed jobs again
	if len(cfg.Cluster.Addresses) > 0 {
		dial := d.cluster
		if dial == nil {
			dial = cluster.DialHazelcast(cfg.Cluster.Addresses, cfg.Cluster.Name, cfg.Cluster.Namespace, log)
		}
		s.member = cluster.New(dial, log)
		s.engine.Share(s.member.Share)
	}
	cat, err := catalog.New(cfg.Catalog, cfg.GitHub.Sources, sources, s.engine.Notify, log)
	if err != nil {
		return nil, err
	}
	documents := docs.New(trees, safehtml.New())
	// Registration order is publish order at the warm start: the catalogue first, then what hangs off its repositories.
	catalogued := func(k snapshot.RepoKey) (snapshot.Repo, bool) {
		if e := s.pub.Current().Repos[k]; e != nil {
			return e.Repo, true
		}
		return snapshot.Repo{}, false
	}
	specs := []sync.Spec{cat.Spec(), commits.New(fetchers).Spec(), documents.Spec(), cat.DescriptorSpec(),
		releases.New(releaseFetchers).Spec(), manifests.New(manifestTrees).Spec(), newPackages(cfg.Packages, packageTrees, d, catalogued).Spec()}
	if f.Pulls {
		specs = append(specs, pulls.New(openPulls, pulls.Options{Repo: catalogued}).Spec())
	}
	if f.Issues {
		specs = append(specs, issues.New(issueFetchers, issues.Options{Repo: catalogued}).Spec())
	}
	for _, spec := range specs {
		if err := s.engine.Register(spec); err != nil {
			return nil, err
		}
	}

	// view is what a request may see: the full snapshot for a private viewer, else its public view.
	view := func(ctx context.Context) *snapshot.Snapshot {
		cur := s.pub.Current()
		if httpapi.Private(ctx) {
			return cur
		}
		return cur.Public()
	}
	events := httpapi.NewEvents(view, s.pub.Published)
	g := gauges{current: s.pub.Current, depth: s.engine.Depth, rate: gh.RateRemaining, streams: events.Streams}
	if s.member != nil {
		g.leader = s.member.Leader
	}
	m := newMetrics(g)
	gh.Observe(m.githubResponse)
	s.engine.Observe(m.job)
	var upstream *url.URL
	if cfg.Web.Dir != "" {
		if s.web, err = webui.New(cfg.Web.Dir, cfg.Web.Node, readBuildInfo().Version, log); err != nil {
			return nil, err
		}
		upstream = s.web.URL()
	}
	ready := func() bool {
		return !s.stopping.Load() && s.pub.Current().Catalogued && (s.web == nil || s.web.Ready())
	}
	s.layouts = layout.Open(cfg.Layout.Dir, log)
	s.notices = announce.Open(cfg.Announcements, cfg.Layout.Dir, time.Now, log)
	s.prereqs = &prereqs{sources: ids, source: gh.Source, current: s.pub.Current, webhooks: cfg.Webhook.Secret.IsSet(),
		needs: needs(f), log: log.With("component", "prerequisites")}
	st := &status{ready: ready, prereqs: s.prereqs.check, alive: s.engine.Alive, current: s.pub.Current, depth: s.engine.Depth, rate: gh.RateRemaining,
		webhook: cfg.Webhook.Secret.IsSet(), layouts: func() []string { return append(s.layouts.Problems(), s.notices.Problems()...) },
		deliveries: map[string]int{}}
	if s.member != nil {
		st.cluster = s.member
	}

	private := slices.ContainsFunc(cfg.GitHub.Sources, func(src config.Source) bool { return src.IncludePrivate })
	opts := httpapi.Options{Log: log, HSTS: strings.HasPrefix(cfg.Server.PublicURL, "https://"), PublicURL: cfg.Server.PublicURL,
		WebToken: newToken(), TrustedProxies: cfg.Server.TrustedProxies,
		Access: httpapi.Access{UserHeader: cfg.Access.UserHeader, GroupsHeader: cfg.Access.GroupsHeader, PrivateGroups: cfg.Access.PrivateGroups,
			AdminEmails: cfg.Auth.AdminEmails, AdminGroups: cfg.Auth.AdminGroups, Vary: private},
		Observe: m.request, RequireAuth: cfg.Access.RequireAuth}
	md := llms.New(view, cfg.Server.PublicURL, llmsOptions(opts.Origin, f))
	external := d.images
	if external == nil {
		external = egress.Guarded(10 * time.Second)
	}
	routes := []httpapi.Route{{Pattern: "GET /badges/", Handler: badges.Handler(view,
		badgeData(f))},
		{Pattern: "GET /images/{owner}/{name}/{id}", Handler: documents.Images(view, external)},
		{Pattern: "GET /llms.txt", Handler: md.Index()}, {Pattern: "GET /llms-full.txt", Handler: md.Full()},
		{Pattern: "GET /knowledge.yaml", Handler: md.KCP()}, {Pattern: "GET /.well-known/kcp.json", Handler: md.WellKnown()},
		{Pattern: "GET /sitemap.xml", Handler: md.Sitemap()}, {Pattern: "GET /sitemaps/{file}", Handler: md.Sitemap()},
		{Pattern: "GET /robots.txt", Handler: robots(cfg.Access.RequireAuth, md.Robots())}, {Pattern: "GET /opensearch.xml", Handler: md.OpenSearch()},
		{Pattern: "GET /repos/{owner}/{name}/readme.md", Handler: md.Readme()},
		{Pattern: "GET /repos/{owner}/{name}", Handler: md.Repo(), Markdown: true},
		{Pattern: "GET /repos/{owner}/{name}/docs/{path...}", Handler: md.Doc(), Markdown: true},
		{Pattern: "GET /groups/{group}", Handler: md.Group(), Markdown: true}, events.Route()}
	routes = append(routes, catalog.Legacy(view)...)
	if cfg.Webhook.Secret.IsSet() {
		s.inbox = &inbox{park: s.store, deliver: s.deliver, handled: s.engine.Handled, log: log, now: time.Now}
		rc := webhook.New(cfg.Webhook.Secret.Reveal(), cfg.Webhook.PreviousSecret.Reveal(), webhookSink(s.inbox.take, cfg.GitHub.Sources), log)
		if s.member != nil {
			rc.Dedupe(s.member.First)
		}
		observe := func(event, result string) { m.delivery(event, result); st.delivery(event, result) }
		rc.Observe(observe)
		for _, src := range cfg.GitHub.Sources {
			if g := gh.Source(src.ID); g.App() {
				s.inbox.catchUps = append(s.inbox.catchUps, catchUp(g, src.ID, s.store, s.pub.Current, webhookSink(s.inbox.take, cfg.GitHub.Sources), observe, log))
			}
		}
		routes = append(routes, httpapi.Route{Pattern: webhook.Pattern, MaxBody: webhook.MaxBody, Handler: rc})
	}
	routes = append(routes, httpapi.Route{Pattern: "GET /api/v1/admin/status", Handler: httpapi.AdminOnly(st)})
	me := httpapi.MeAPI{}
	if cfg.Auth.SignIn() || len(cfg.Auth.Tokens) > 0 {
		a, err := newAuth(cfg, log)
		if err != nil {
			return nil, err
		}
		opts.Auth, me.SignIn = a, a.Methods
		if u, err := url.Parse(cfg.Auth.OIDC.Issuer); err == nil && u.Host != "" { // sign-out ends at the provider's end session
			opts.FormTargets = []string{u.Scheme + "://" + u.Host}
		}
		routes = append(routes, a.Routes()...)
	}
	// The hooks that add to a repository's summary, in the order of its fields; the pull requests' only when the feature is on.
	repoHooks := []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.Repo){commits.Repo, releases.Repo, manifests.Repo, packages.Repo}
	if f.Pulls {
		repoHooks = append(repoHooks, pulls.Repo)
	}
	repoHooks = append(repoHooks, repoIssuesAPI, repoHealthAPI)
	catalogAPI := catalog.API{Current: view,
		Dashboard: []func(*snapshot.Snapshot, *oapi.Dashboard){commits.Dashboard, dashboardHealthAPI, dashboardSummaryAPI},
		Group:     []func(*snapshot.Snapshot, snapshot.Group, *oapi.Group){groupHealthAPI},
		Repo:      repoHooks,
		Detail:    []func(*snapshot.RepoEntry, *oapi.RepoDetail){commits.Detail},
		Alumnus:   []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.Alumnus){manifests.Alumnus, commits.Alumnus}}
	if f.Pulls {
		catalogAPI.Dashboard = append(catalogAPI.Dashboard, pulls.Dashboard)
	}
	commitsAPI := commits.API{Current: view}
	pullsAPI, issuesAPI := pulls.API{Current: view, Off: !f.Pulls}, issues.API{Current: view, Off: !f.Issues}
	groupNames := func(ctx context.Context) []string {
		var names []string
		for _, g := range view(ctx).Groups {
			names = append(names, g.Name+" (id "+g.ID+")")
		}
		return names
	}
	searchAPI := search.API{Current: view, Observe: m.search}
	catalogue, err := toolReads{find: searchAPI, catalog: catalogAPI, commits: commitsAPI, pulls: pullsAPI, issues: issuesAPI, releases: releases.API{Current: view}, repo: md.Repo(), doc: md.Doc(), group: md.Group(), features: f}.catalogue()
	if err != nil {
		return nil, err
	}
	ask, err := newAssistant(cfg, catalogue, groupNames, m.assistant, log)
	if err != nil {
		return nil, err
	}
	routes = append(routes, ask.Routes()...)
	agents := tools.API{Catalogue: catalogue, PublicURL: cfg.Server.PublicURL}
	routes = append(routes, agents.Routes()...)
	routes = append(routes, mcpserver.Routes(mcpserver.Options{Catalogue: catalogue, Version: version, PublicURL: cfg.Server.PublicURL,
		RequireAuth: cfg.Access.RequireAuth, Log: log.With("component", "mcp")})...)
	routes = append(routes, s.layouts.Routes(cfg.Server.PublicURL)...)
	routes = append(routes, s.notices.Routes(cfg.Server.PublicURL, log.With("component", "announcements"))...)
	features := api{catalogAPI, commitsAPI, pullsAPI, issuesAPI, releases.API{Current: view}, docs.API{Current: view}, manifests.API{Current: view},
		layout.API{Current: view, Store: s.layouts, Notices: s.notices.For, Off: offElements(f)},
		httpapi.SnapshotAPI{Current: view}, me, ask, agents, searchAPI, rulesAPI{f}, s.prereqs}
	if s.web != nil {
		opts.Build = s.web.Build
	}
	s.public = httpapi.Server(httpapi.Public(opts, opts.Pages(upstream), contract.OpenAPI, features, routes...), log)
	s.public.RegisterOnShutdown(events.Close)
	admin := []httpapi.Route{{Pattern: "GET /metrics", Handler: m.handler()}, {Pattern: "GET /api/v1/status", Handler: st}}
	if cfg.Ops.Pprof {
		admin = append(admin, httpapi.Route{Pattern: "GET /debug/pprof/", Handler: http.HandlerFunc(pprof.Index)},
			httpapi.Route{Pattern: "GET /debug/pprof/cmdline", Handler: http.HandlerFunc(pprof.Cmdline)},
			httpapi.Route{Pattern: "GET /debug/pprof/profile", Handler: http.HandlerFunc(pprof.Profile)},
			httpapi.Route{Pattern: "GET /debug/pprof/symbol", Handler: http.HandlerFunc(pprof.Symbol)},
			httpapi.Route{Pattern: "GET /debug/pprof/trace", Handler: http.HandlerFunc(pprof.Trace)})
	}
	s.admin = httpapi.Server(httpapi.Admin(opts, s.engine.Alive, ready, admin...), log)
	var lc net.ListenConfig
	if s.publicLn, err = lc.Listen(ctx, "tcp", cfg.Server.PublicAddr); err != nil {
		return nil, err
	}
	if s.adminLn, err = lc.Listen(ctx, "tcp", cfg.Server.AdminAddr); err != nil {
		_ = s.publicLn.Close()
		return nil, err
	}
	return s, nil
}

// newAuth builds sign-in from the config. A session keeps only the provider's groups the access lists name, so it fits a cookie;
// a group added to a list applies at the next sign-in, one removed at once.
func newAuth(cfg config.Config, log *slog.Logger) (*auth.Auth, error) {
	a := cfg.Auth
	named := slices.Concat(cfg.Access.PrivateGroups, a.AdminGroups)
	o := auth.Options{SessionTTL: a.SessionTTL, PublicURL: cfg.Server.PublicURL, Log: log,
		Keep: func(groups []string) []string {
			return slices.DeleteFunc(slices.Clone(groups), func(g string) bool { return !slices.Contains(named, g) })
		}}
	if a.SignIn() {
		o.SessionKey = a.SessionKey.Reveal()
	}
	for _, t := range a.Tokens {
		o.Tokens = append(o.Tokens, auth.Token{Name: t.Name, Secret: t.Secret.Reveal(), Groups: t.Groups, Admin: t.Admin})
	}
	if a.OIDC.Issuer != "" {
		o.OIDC = &auth.OIDC{Issuer: a.OIDC.Issuer, ClientID: a.OIDC.ClientID, ClientSecret: string(a.OIDC.ClientSecret.Reveal()),
			Scopes: a.OIDC.Scopes, GroupsClaim: a.OIDC.GroupsClaim}
	}
	return auth.New(o)
}

// llmsOptions is what the Markdown surface shows of the other features: documents, the descriptor, the manifests' summary and the
// commits' freshness.
func llmsOptions(origin func(*http.Request) string, f config.Features) llms.Options {
	o := llms.Options{
		Docs: func(e *snapshot.RepoEntry) ([]llms.Doc, snapshot.Freshness) {
			ds, fresh := docs.Of(e)
			out := make([]llms.Doc, 0, len(ds))
			for _, d := range ds {
				out = append(out, llms.Doc{Path: d.Path, BlobSHA: d.BlobSHA, README: d.README, Truncated: d.Truncated, Hidden: d.Hidden,
					Source: d.Source})
			}
			return out, fresh
		},
		About: func(e *snapshot.RepoEntry) llms.About {
			d, _ := catalog.DescriptorOf(e)
			return llms.About{Description: d.Description, Lifecycle: d.Lifecycle, Owners: d.Owners}
		},
		Technology: func(s *snapshot.Snapshot, e *snapshot.RepoEntry) llms.Technology {
			sum := manifests.Summarise(s, e)
			t := llms.Technology{Languages: sum.Technology.Languages, BuildTools: sum.Technology.BuildTools, Uses: sum.Uses, UsedBy: sum.UsedBy}
			for _, tc := range sum.Technology.Toolchains {
				for _, v := range tc.Versions {
					t.Toolchains = append(t.Toolchains, string(tc.Name)+" "+v)
				}
			}
			return t
		},
		Freshness:  func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := commits.Of(e); return f },
		LastCommit: func(e *snapshot.RepoEntry) time.Time { t, _ := commits.LastCommit(e); return t },
		Origin:     origin,
		Release: func(e *snapshot.RepoEntry) (llms.Release, bool) {
			rel, _ := releases.Latest(e)
			if rel == nil {
				return llms.Release{}, false
			}
			return llms.Release{Tag: rel.Tag, URL: rel.URL, Published: rel.Published}, true
		},
		Retired: func(e *snapshot.RepoEntry) string { return string(catalog.Retired(e)) },
	}
	if f.Pulls {
		o.Pulls = pulls.Badge
	}
	if f.Issues {
		o.Issues = issues.Summary
	}
	return o
}

// badgeData is what the badges show of the other features; without pull requests there is no pulls badge.
// latestRelease hands the badges a repository's latest stable release from the releases feature.
func latestRelease(e *snapshot.RepoEntry) (*badges.Release, snapshot.Freshness) {
	rel, fresh := releases.Latest(e)
	if rel == nil {
		return nil, fresh
	}
	return &badges.Release{Tag: rel.Tag, Name: rel.Name, URL: rel.URL, Published: rel.Published}, fresh
}

func badgeData(f config.Features) badges.Data {
	d := badges.Data{LastCommit: commits.LastCommit, Release: latestRelease, Health: assess, Product: groupHealth, Issues: issueCount}
	if f.Pulls {
		d.Pulls = pulls.Badge
	}
	return d
}

// newToken is the secret of this process the web UI server shows on its API calls (httpapi.Options.WebToken).
func newToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// loopback is the address the web UI server reaches the public listener at: its own IP, or loopback when it listens on all.
func loopback(addr net.Addr) string {
	host, port, _ := net.SplitHostPort(addr.String())
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// watchPrereqs checks the prerequisites at every publish (the App's installations are listed again with each source's listing),
// so a change is logged when it happens, not when a page asks.
func (s *server) watchPrereqs(ctx context.Context) {
	for {
		s.prereqs.check()
		select {
		case <-ctx.Done():
			return
		case <-s.pub.Published():
		}
	}
}

// deliver hands a webhook event to the engine of every instance (through the cluster when there is one, else or when it fails to
// this one's): per repository when repo is set, else per source.
func (s *server) deliver(ev sync.Event, repo snapshot.RepoKey, source string) {
	if s.member != nil {
		err := s.member.Event(context.Background(), ev, repo, source)
		if err == nil {
			return
		}
		s.log.Warn("webhook event not sent to the cluster; handled here", "err", err)
	}
	if repo == "" {
		s.engine.NotifySource(ev, source)
	} else {
		s.engine.Notify(ev, repo)
	}
}

// webhookSink hands webhook events on: repository events to the repository's jobs, owner-wide events to the listing of the source
// that has the owner among its organisations. Owners no source lists are ignored.
func webhookSink(take func(sync.Event, snapshot.RepoKey, string) error, sources []config.Source) webhook.Sink {
	sourceOf := map[string]string{}
	for _, s := range sources {
		for _, org := range s.Orgs {
			sourceOf[strings.ToLower(org)] = s.ID
		}
	}
	return func(ev sync.Event, owner, name string) error {
		id, ok := sourceOf[strings.ToLower(owner)]
		switch {
		case !ok:
			return nil
		case name == "" || ev == sync.EventSourceChanged:
			return take(ev, "", id)
		default:
			return take(ev, snapshot.KeyOf(owner, name), "")
		}
	}
}

// run serves until ctx is cancelled, then shuts down within the grace period: readiness drops, the servers drain, the web UI server
// stops, the instance leaves the cluster (another takes the lead), the engine stops (jobs in flight are cancelled and run again after a restart), pending
// snapshot updates are dropped and the store closes.
func (s *server) run(ctx context.Context) error {
	engineCtx, stopEngine := context.WithCancel(context.WithoutCancel(ctx))
	engineDone := make(chan error, 1)
	go func() { engineDone <- s.engine.Run(engineCtx) }()
	layoutsCtx, stopLayouts := context.WithCancel(ctx)
	layoutsDone := make(chan struct{})
	go func() { s.layouts.Run(layoutsCtx); close(layoutsDone) }()
	noticesDone := make(chan struct{})
	go func() { s.notices.Run(layoutsCtx); close(noticesDone) }()
	defer func() { stopLayouts(); <-noticesDone }()
	defer func() { stopLayouts(); <-layoutsDone }()
	inboxDone := make(chan struct{})
	go func() {
		if s.inbox != nil {
			s.inbox.run(layoutsCtx, s.engine.Warm())
		}
		close(inboxDone)
	}()
	defer func() { stopLayouts(); <-inboxDone }()
	checksDone := make(chan struct{})
	go func() { s.watchPrereqs(layoutsCtx); close(checksDone) }()
	defer func() { stopLayouts(); <-checksDone }()
	memberCtx, stopMember := context.WithCancel(context.WithoutCancel(ctx))
	memberDone := make(chan error, 1)
	if s.member != nil {
		go func() { memberDone <- s.member.Run(memberCtx, s.engine) }()
	} else {
		memberDone <- nil
	}

	errc := make(chan error, 2)
	for _, p := range []struct {
		name string
		srv  *http.Server
		ln   net.Listener
	}{{"public", s.public, s.publicLn}, {"admin", s.admin, s.adminLn}} {
		s.log.Info("listening", "listener", p.name, "addr", p.ln.Addr().String())
		go func() { errc <- p.srv.Serve(p.ln) }()
	}

	webCtx, stopWeb := context.WithCancel(context.WithoutCancel(ctx))
	webDone := make(chan error, 1)
	if s.web != nil {
		go func() { webDone <- s.web.Run(webCtx, loopback(s.publicLn.Addr())) }()
	} else {
		webDone <- nil
	}

	var serveErr error
	serversLeft, engineRunning := 2, true
	select {
	case <-ctx.Done():
		s.log.Info("shutting down", "grace", s.grace.String())
	case serveErr = <-errc:
		serversLeft--
		s.log.Error("listener failed", "err", serveErr)
	case serveErr = <-engineDone:
		engineRunning = false
		s.log.Error("sync engine stopped", "err", serveErr)
	}
	s.stopping.Store(true)

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.grace)
	defer cancel()
	err := errors.Join(serveErr, s.public.Shutdown(shutdownCtx), s.admin.Shutdown(shutdownCtx))
	for range serversLeft {
		if e := <-errc; !errors.Is(e, http.ErrServerClosed) {
			err = errors.Join(err, e)
		}
	}
	stopWeb()
	err = errors.Join(err, <-webDone)
	stopMember()
	err = errors.Join(err, <-memberDone)
	stopEngine()
	if engineRunning {
		err = errors.Join(err, <-engineDone)
	}
	s.pub.Close()
	return errors.Join(err, s.store.Close())
}

// robots is robots.txt: the one llms builds, or, when the portal answers signed-in viewers only, one that keeps every crawler out.
func robots(closed bool, open http.Handler) http.Handler {
	if !closed {
		return open
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "User-agent: *\nDisallow: /\n")
	})
}

// newPackages is the packages feature with its providers, in the order a repository's packages are read.
func newPackages(cfg config.Packages, trees map[string]packages.GitHub, d deps, repo func(snapshot.RepoKey) (snapshot.Repo, bool)) *packages.Packages {
	public, private := d.registries, d.registries
	if public == nil {
		public, private = egress.Guarded(10*time.Second), http.DefaultTransport
	}
	providers := []packages.Provider{packages.Maven{Central: d.public["maven"]}, packages.NPM{Registry: d.public["npm"]},
		packages.PyPI{Simple: d.public["pypi"]}, packages.Cargo{API: d.public["cargo"]}, packages.Go{Proxy: d.public["go"]},
		packages.Swift{}, packages.CocoaPods{Trunk: d.public["cocoapods"]}, packages.NuGet{Flat: d.public["nuget"]},
		packages.RubyGems{Server: d.public["rubygems"]}, packages.Packagist{Repository: d.public["packagist"]}}
	registries := make([]packages.Registry, 0, len(cfg.Registries))
	for _, r := range cfg.Registries {
		u := r.URL
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		reg := packages.Registry{ID: r.ID, Ecosystem: r.Ecosystem, URL: u, Private: r.Network == "private"}
		switch a := r.Auth; {
		case a.User != "":
			reg.Credential = &packages.Credential{User: a.User, Secret: a.Password.Reveal()}
		case a.TokenFile != "":
			reg.Credential = &packages.Credential{Secret: a.Token.Reveal(), Header: a.Header}
		}
		registries = append(registries, reg)
	}
	return packages.New(trees, packages.Options{Providers: providers, Client: packages.NewClient(public, private, registries), Registries: registries,
		Public: cfg.Public, Repo: repo})
}
