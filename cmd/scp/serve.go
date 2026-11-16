package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	contract "github.com/cantara/source-code-portal/api"
	"github.com/cantara/source-code-portal/internal/app/fleethealth"
	"github.com/cantara/source-code-portal/internal/app/toolset"
	"github.com/cantara/source-code-portal/internal/feature/announce"
	"github.com/cantara/source-code-portal/internal/feature/assistant"
	"github.com/cantara/source-code-portal/internal/feature/badges"
	"github.com/cantara/source-code-portal/internal/feature/catalog"
	"github.com/cantara/source-code-portal/internal/feature/commits"
	"github.com/cantara/source-code-portal/internal/feature/docs"
	"github.com/cantara/source-code-portal/internal/feature/insights"
	"github.com/cantara/source-code-portal/internal/feature/issues"
	"github.com/cantara/source-code-portal/internal/feature/layout"
	"github.com/cantara/source-code-portal/internal/feature/llms"
	"github.com/cantara/source-code-portal/internal/feature/manifests"
	"github.com/cantara/source-code-portal/internal/feature/pulls"
	"github.com/cantara/source-code-portal/internal/feature/releases"
	"github.com/cantara/source-code-portal/internal/feature/search"
	"github.com/cantara/source-code-portal/internal/feature/visibility"
	"github.com/cantara/source-code-portal/internal/lib/egress"
	"github.com/cantara/source-code-portal/internal/lib/safehtml"
	"github.com/cantara/source-code-portal/internal/platform/analytics"
	"github.com/cantara/source-code-portal/internal/platform/auth"
	"github.com/cantara/source-code-portal/internal/platform/cluster"
	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/github"
	"github.com/cantara/source-code-portal/internal/platform/httpapi"
	"github.com/cantara/source-code-portal/internal/platform/mcpserver"
	"github.com/cantara/source-code-portal/internal/platform/people"
	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
	"github.com/cantara/source-code-portal/internal/platform/sync"
	"github.com/cantara/source-code-portal/internal/platform/tools"
	"github.com/cantara/source-code-portal/internal/platform/webui"
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
			return runServer(cmd, opts, deps{})
		},
	}
	cmd.Flags().String("public-addr", "", "public listener address (default :9090)")
	cmd.Flags().String("admin-addr", "", "admin listener address (default :9091)")
	return cmd
}

// runServer loads the configuration, then runs the portal of d's mode until the command's context ends (scp serve, scp enterprise).
func runServer(cmd *cobra.Command, opts *options, d deps) error {
	cfg, _, err := loadConfig(cmd, opts)
	if err != nil {
		return err
	}
	log := newLogger(cfg.Log, cmd.ErrOrStderr())
	srv, err := newServer(cmd.Context(), cfg, d, log)
	if err != nil {
		return failure{err}
	}
	if err := srv.run(cmd.Context()); err != nil {
		return failure{err}
	}
	return nil
}

// api composes the strict server from the features that implement its methods. The aliases give the embedded fields distinct names.
type (
	catalogAPI   = catalog.API
	commitsAPI   = commits.API
	pullsAPI     = pulls.API
	issuesAPI    = issues.API
	releasesAPI  = releases.API
	insightsAPI  = insights.API
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
		insightsAPI
		docsAPI
		manifestsAPI
		layoutAPI
		httpapi.SnapshotAPI
		httpapi.MeAPI
		*assistant.Assistant
		toolsAPI
		searchAPI
		fleethealth.RulesAPI
		*prereqs
	}
)

// server owns the store, the snapshot, the sync engine and the two listeners.
type server struct {
	mode              store.Mode // shared, personal or enterprise: see deps.mode
	routes            []string   // the patterns of the routes the features and sign-in add, sorted: the tests compare them between modes
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
	visibility        *visibility.Set
	prereqs           *prereqs
	people            *people.People // the enterprise mode's people; nil in the other modes
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
	analytics  http.RoundTripper                              // transport to the analytics endpoint (the default when nil): a test's loopback
	cluster    func(context.Context) (cluster.Backend, error) // dial for the cluster when cluster.addresses is set
	launch     *auth.Launch                                   // the personal edition's one-use sign-in link
	// mode is what this process is, set by the command that starts it and by nothing in a configuration: the zero value is the shared
	// site (scp serve), scp personal says store.Personal and scp enterprise store.Enterprise. The store is opened for it, and each
	// mode's own parts are wired only in it.
	mode store.Mode
}

// checkedMode is the process's mode (the shared site unless the command said otherwise), after the checks that tie a part of a mode
// to it: a launch link signs in without a secret of the portal's, so only one person's portal may have one; a configuration whose
// sources are read with people's tokens is the enterprise mode's, and that mode has no other.
func (d deps) checkedMode(cfg config.Config) (store.Mode, error) {
	mode := cmp.Or(d.mode, store.Shared)
	switch {
	case d.launch != nil && mode != store.Personal:
		return "", errors.New("a launch link belongs to scp personal")
	case cfg.People() && mode != store.Enterprise:
		return "", errors.New("the sources are read with people's tokens (enterprise.github[].people): run scp enterprise")
	case !cfg.People() && mode == store.Enterprise:
		return "", errors.New("scp enterprise reads its sources with people's tokens: set enterprise.github[].people on every source")
	}
	return mode, nil
}

// views are what a request may see (view) and what its pictures may name (assets): the full snapshot for a private viewer, else its
// public view, and the forks no source shows and no group pins only when the request asked for them (?forks=show). A badge and a
// document's image are fetched by the browser as pictures, with nothing to say that the reader asked for forks, and they are named by
// the repository's own address: they answer for every repository of the viewer's view, hidden forks included. In the enterprise mode
// each person reads what their access list holds, and nothing without one (fail closed).
func (s *server) views(log *slog.Logger) (view, assets func(context.Context) *snapshot.Snapshot) {
	if s.mode == store.Enterprise {
		people := newPeopleViews(s.pub.Current, allowedOf(s.people), log)
		return func(ctx context.Context) *snapshot.Snapshot { return people.of(ctx, httpapi.ShowForks(ctx)) },
			func(ctx context.Context) *snapshot.Snapshot { return people.of(ctx, true) }
	}
	return func(ctx context.Context) *snapshot.Snapshot {
			return s.pub.Current().View(httpapi.Private(ctx), httpapi.ShowForks(ctx))
		},
		func(ctx context.Context) *snapshot.Snapshot { return s.pub.Current().View(httpapi.Private(ctx), true) }
}

// ready is the readiness probe: not stopping, a catalogue loaded and the web UI server up. The enterprise mode has a catalogue once a
// person has linked a token, and serves its onboarding before that.
func (s *server) ready() bool {
	catalogued := s.pub.Current().Catalogued || s.mode == store.Enterprise
	return !s.stopping.Load() && catalogued && (s.web == nil || s.web.Ready())
}

// identityHeaders are the trusted proxy's identity headers; none in the enterprise mode, whose viewer is who OIDC signed in, never a
// header a proxy may pass on from the client, and none with access.proxy_identity: false.
func identityHeaders(a config.Access, mode store.Mode) (user, groups string) {
	if mode == store.Enterprise || !a.ProxyIdentity {
		return "", ""
	}
	return a.UserHeader, a.GroupsHeader
}

// vaultResolved refuses a source whose credential is in the vault of scp personal and was not resolved: serve reads files.
func vaultResolved(cfg config.Config) error {
	for i, src := range cfg.GitHub.Sources {
		if src.App != nil && src.App.Credential && !src.App.PrivateKey.IsSet() { // scp personal resolves it from the vault before it gets here
			return fmt.Errorf("github.sources[%d].app.credential: the key is in the vault of scp personal; serve reads app.private_key_file", i)
		}
		if src.Credential && !src.Token.IsSet() {
			return fmt.Errorf("github.sources[%d].credential: the credential is in the vault of scp personal; serve reads app or token_file", i)
		}
	}
	return nil
}

// newServer opens the store, wires the features and binds both listeners.
func newServer(ctx context.Context, cfg config.Config, d deps, log *slog.Logger) (_ *server, err error) {
	mode, err := d.checkedMode(cfg)
	if err != nil {
		return nil, err
	}
	if err := vaultResolved(cfg); err != nil {
		return nil, err
	}
	gh, err := github.New(cfg.GitHub, d.github, log)
	if err != nil {
		return nil, err
	}
	rd := readersOf(cfg, gh)
	ids := rd.ids
	f := cfg.Features // a switched-off feature is absent: no job, index, route, tool or page (design/features.md)
	s := &server{mode: mode, log: log, grace: cfg.Server.ShutdownGrace, pub: snapshot.NewPublisher(publishDebounce, publishMaxDelay, log, indexersOf(cfg)...)}
	s.pub.Retire(func(e *snapshot.RepoEntry) bool { return catalog.Retired(e) != "" }) // a retired repository is in the Archive, not its group
	if err := s.open(ctx, cfg, gh); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = s.store.Close()
		}
	}()
	s.engine = sync.New(s.store, s.pub, gh, ids, cfg.Sync.Concurrency, log)
	s.engine.SetPolling(append(polledSources(cfg), unloggedSources(cfg)...)...)
	bi := readBuildInfo()
	s.engine.Release(bi.Version + "+" + bi.Build) // a new build runs failed jobs again
	s.joinCluster(cfg.Cluster, d, log)
	cat, err := catalog.New(cfg.Catalog, cfg.GitHub.Sources, rd.sources, s.engine.Notify, log)
	if err != nil {
		return nil, err
	}
	documents := docs.New(rd.trees, safehtml.New())
	if err := s.registerJobs(cfg, d, rd, cat, documents); err != nil {
		return nil, err
	}

	view, assets := s.views(log)
	events := httpapi.NewEvents(view, s.pub.Published)
	g := gauges{current: s.pub.Current, depth: s.engine.Depth, rate: gh.RateRemaining, streams: events.Streams}
	if s.member != nil {
		g.leader = s.member.Leader
	}
	m := newMetrics(g)
	gh.Observe(m.githubResponse)
	s.engine.Observe(m.job)
	look, themeRoutes, err := themed(cfg.Web)
	if err != nil {
		return nil, err
	}
	var upstream *url.URL
	if cfg.Web.Dir != "" {
		if s.web, err = webui.New(cfg.Web.Dir, cfg.Web.Node, readBuildInfo().Version, look.Env(), analytics.Env(cfg.Web.PlausibleDomain), log); err != nil {
			return nil, err
		}
		upstream = s.web.URL()
	}
	ready := s.ready
	if err := s.openDocuments(ctx, cfg, log); err != nil {
		return nil, err
	}
	feed := cfg.Receiver() || cfg.Webhook.CatchUp // GitHub tells the portal what changed: through its receiver, or from the App's delivery log
	s.prereqs = &prereqs{sources: ids, source: gh.Source, current: s.pub.Current, told: toldSources(cfg),
		needs: needs(f), log: log.With("component", "prerequisites")}
	st := &status{mode: string(mode), ready: ready, prereqs: s.prereqs.check, alive: s.engine.Alive, current: s.pub.Current, depth: s.engine.Depth, rate: gh.RateRemaining,
		config: func() statusConfig {
			return statusConfig{File: cfg.Baseline.File != nil, Changes: configChanges{Visibility: s.visibility.Changes(),
				Announcements: s.notices.Changes(), Layouts: s.layouts.Changes()}}
		},
		accounts: appAccounts(gh),
		cadence:  s.engine.Cadence, webhook: feed, receiver: cfg.Receiver(), catchUp: cfg.Webhook.CatchUp, layouts: func() []string {
			return slices.Concat(s.layouts.Problems(), s.notices.Problems(), s.visibility.Problems())
		},
		deliveries: map[string]int{}}
	st.polled = polledSet(cfg)
	if s.member != nil {
		st.cluster = s.member
	}

	private := slices.ContainsFunc(cfg.GitHub.Sources, func(src config.Source) bool { return src.IncludePrivate })
	opts := httpapi.Options{Log: log, HSTS: strings.HasPrefix(cfg.Server.PublicURL, "https://"), PublicURL: cfg.Server.PublicURL,
		WebToken: newToken(), TrustedProxies: cfg.Server.TrustedProxies,
		Access: httpapi.Access{PrivateGroups: cfg.Access.PrivateGroups,
			AdminEmails: cfg.Auth.AdminEmails, AdminGroups: cfg.Auth.AdminGroups, Vary: private},
		Observe: m.request, ObservePrivate: m.privateView, RequireAuth: cfg.Access.RequireAuth}
	opts.NoIndex = func(r *http.Request) bool { return s.visibility.NoIndex(s.pub.Current().Public(), r.URL.Path) } // what search engines see is the public view
	opts.Access.UserHeader, opts.Access.GroupsHeader = identityHeaders(cfg.Access, mode)
	md := llms.New(view, cfg.Server.PublicURL, llmsOptions(opts.Origin, f, s.visibility))
	external := d.images
	if external == nil {
		external = egress.Guarded(10 * time.Second)
	}
	routes := []httpapi.Route{{Pattern: "GET /badges/", Handler: badges.Handler(assets,
		badgeData(f))},
		{Pattern: "GET /images/{owner}/{name}/{id}", Handler: documents.Images(assets, external)},
		{Pattern: "GET /llms.txt", Handler: md.Index()}, {Pattern: "GET /llms-full.txt", Handler: md.Full()},
		{Pattern: "GET /knowledge.yaml", Handler: md.KCP()}, {Pattern: "GET /.well-known/kcp.json", Handler: md.WellKnown()},
		{Pattern: "GET /sitemap.xml", Handler: md.Sitemap()}, {Pattern: "GET /sitemaps/{file}", Handler: md.Sitemap()},
		{Pattern: "GET /robots.txt", Handler: robots(cfg.Access.RequireAuth, md.Robots())}, {Pattern: "GET /opensearch.xml", Handler: md.OpenSearch()},
		{Pattern: "GET /repos/{owner}/{name}/readme.md", Handler: md.Readme()},
		{Pattern: "GET /repos/{owner}/{name}", Handler: md.Repo(), Markdown: true},
		{Pattern: "GET /repos/{owner}/{name}/docs/{path...}", Handler: md.Doc(), Markdown: true},
		{Pattern: "GET /groups/{group}", Handler: md.Group(), Markdown: true}, events.Route()}
	routes = append(routes, themeRoutes...)
	routes = append(routes, catalog.Legacy(view)...)
	routes = append(routes, s.eventRoutes(cfg, gh, m, st, feed, log)...)
	routes = append(routes, httpapi.Route{Pattern: "GET /api/v1/admin/status", Handler: httpapi.AdminOnly(st)})
	if cfg.Web.PlausibleDomain != "" { // the tracker posts to the portal, which forwards each event to Plausible's endpoint
		routes = append(routes, httpapi.Route{Pattern: "POST " + analytics.Path, MaxBody: 16 << 10,
			Handler: analytics.New(cfg.Web.PlausibleEndpoint, opts.ClientIP, d.analytics).Handler()})
	}
	me := httpapi.MeAPI{}
	if cfg.Auth.SignIn() || len(cfg.Auth.Tokens) > 0 {
		a, err := newAuth(cfg, log, d.launch)
		if err != nil {
			return nil, err
		}
		opts.Auth, me.SignIn = a, a.Methods
		if u, err := url.Parse(cfg.Auth.OIDC.Issuer); err == nil && u.Host != "" { // sign-out ends at the provider's end session
			opts.FormTargets = []string{u.Scheme + "://" + u.Host}
		}
		routes = append(routes, a.Routes()...)
	}
	catalogAPI := catalogWith(view, f)
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
	catalogue, err := toolset.Reads{Find: searchAPI, Catalog: catalogAPI, Commits: commitsAPI, Pulls: pullsAPI, Issues: issuesAPI, Releases: newReleases(view, documents), Insights: newInsights(view), Repo: md.Repo(), Doc: md.Doc(), Group: md.Group(), Features: f}.Catalogue()
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
	routes = append(routes, s.peopleRoutes(cfg)...)
	routes = append(routes, s.notices.Routes(cfg.Server.PublicURL, log.With("component", "announcements"))...)
	routes = append(routes, s.visibility.Routes(cfg.Server.PublicURL, s.pub.Current, log.With("component", "visibility"))...)
	routes = append(routes, httpapi.Route{Pattern: "GET " + configPath, Handler: configHandler(cfg.Baseline.File, s.visibility, s.notices, s.layouts)})
	features := api{catalogAPI, commitsAPI, pullsAPI, issuesAPI, newReleases(view, documents), newInsights(view), docs.API{Current: view}, manifests.API{Current: view},
		layout.API{Current: view, Store: s.layouts, Notices: s.notices.For, Off: offElements(f)},
		httpapi.SnapshotAPI{Current: view}, me, ask, agents, searchAPI, fleethealth.RulesAPI{Features: f, People: s.people != nil}, s.prereqs}
	if s.web != nil {
		opts.Build = s.web.Build
	}
	for _, r := range routes {
		s.routes = append(s.routes, r.Pattern)
	}
	slices.Sort(s.routes)
	s.public = httpapi.Server(httpapi.Public(opts, opts.Pages(upstream), contract.OpenAPI, features, routes...), log)
	s.public.RegisterOnShutdown(events.Close)
	s.admin = httpapi.Server(httpapi.Admin(opts, s.engine.Alive, ready, adminRoutes(cfg.Ops, m, st)...), log)
	if err := s.listen(ctx, cfg.Server); err != nil {
		return nil, err
	}
	return s, nil
}

// run serves until ctx is cancelled, then shuts down within the grace period: readiness drops, the servers drain, the web UI server
// stops, the instance leaves the cluster (another takes the lead), the engine stops (jobs in flight are cancelled and run again after a restart), pending
// snapshot updates are dropped and the store closes.
func (s *server) run(ctx context.Context) error {
	engineCtx, stopEngine := context.WithCancel(context.WithoutCancel(ctx))
	engineDone := make(chan error, 1)
	go func() { engineDone <- s.engine.Run(engineCtx) }()
	sideCtx, stopSide := context.WithCancel(ctx) // the background readers of the inbox and the prerequisites
	inboxDone := make(chan struct{})
	go func() {
		if s.inbox != nil {
			s.inbox.run(sideCtx, s.engine.Warm())
		}
		close(inboxDone)
	}()
	defer func() { stopSide(); <-inboxDone }()
	checksDone := make(chan struct{})
	go func() { s.watchPrereqs(sideCtx); close(checksDone) }()
	defer func() { stopSide(); <-checksDone }()
	peopleCtx, stopPeople := context.WithCancel(context.WithoutCancel(ctx)) // the enterprise mode's listings: stopped before the store closes
	peopleDone := make(chan struct{})
	go func() {
		if s.people != nil {
			s.people.Run(peopleCtx)
		}
		close(peopleDone)
	}()
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
		s.log.Info("listening", "mode", string(s.mode), "listener", p.name, "addr", p.ln.Addr().String())
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
	stopPeople()
	<-peopleDone
	s.pub.Close()
	return errors.Join(err, s.store.Close())
}
