package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/egress"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/insights"
	"github.com/cantara/source-code-portal/internal/issues"
	"github.com/cantara/source-code-portal/internal/layout"
	"github.com/cantara/source-code-portal/internal/llms"
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/mcpserver"
	"github.com/cantara/source-code-portal/internal/packages"
	"github.com/cantara/source-code-portal/internal/people"
	"github.com/cantara/source-code-portal/internal/pulls"
	"github.com/cantara/source-code-portal/internal/releases"
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/search"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/theme"
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
		rulesAPI
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
// header a proxy may pass on from the client.
func identityHeaders(a config.Access, mode store.Mode) (user, groups string) {
	if mode == store.Enterprise {
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
	s := &server{mode: mode, log: log, grace: cfg.Server.ShutdownGrace, pub: snapshot.NewPublisher(publishDebounce, publishMaxDelay, log, indexers...)}
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
		if s.web, err = webui.New(cfg.Web.Dir, cfg.Web.Node, readBuildInfo().Version, look.Env(), log); err != nil {
			return nil, err
		}
		upstream = s.web.URL()
	}
	ready := s.ready
	imported, err := docstore.Import(ctx, s.store, cfg.Layout.Dir, append(layout.Names(), announce.Name))
	if err != nil {
		return nil, fmt.Errorf("layout.dir: %w", err)
	}
	if len(imported) > 0 {
		log.Info("layouts and announcements from layout.dir are now kept in the store; the files are no longer read", "imported", imported)
	}
	s.layouts = layout.Open(ctx, s.store, log)
	s.notices = announce.Open(ctx, cfg.Announcements, s.store, time.Now, log)
	feed := cfg.Webhook.Secret.IsSet() || cfg.Webhook.CatchUp // GitHub tells the portal what changed: through its receiver, or from the App's delivery log
	s.prereqs = &prereqs{sources: ids, source: gh.Source, current: s.pub.Current, webhooks: feed,
		needs: needs(f), log: log.With("component", "prerequisites")}
	st := &status{mode: string(mode), ready: ready, prereqs: s.prereqs.check, alive: s.engine.Alive, current: s.pub.Current, depth: s.engine.Depth, rate: gh.RateRemaining,
		cadence: s.engine.Cadence, webhook: feed, receiver: cfg.Webhook.Secret.IsSet(), catchUp: cfg.Webhook.CatchUp, layouts: func() []string { return append(s.layouts.Problems(), s.notices.Problems()...) },
		deliveries: map[string]int{}}
	st.polled = map[string]bool{}
	for _, id := range polledSources(cfg) {
		st.polled[id] = true
	}
	if s.member != nil {
		st.cluster = s.member
	}

	private := slices.ContainsFunc(cfg.GitHub.Sources, func(src config.Source) bool { return src.IncludePrivate })
	opts := httpapi.Options{Log: log, HSTS: strings.HasPrefix(cfg.Server.PublicURL, "https://"), PublicURL: cfg.Server.PublicURL,
		WebToken: newToken(), TrustedProxies: cfg.Server.TrustedProxies,
		Access: httpapi.Access{PrivateGroups: cfg.Access.PrivateGroups,
			AdminEmails: cfg.Auth.AdminEmails, AdminGroups: cfg.Auth.AdminGroups, Vary: private},
		Observe: m.request, ObservePrivate: m.privateView, RequireAuth: cfg.Access.RequireAuth}
	opts.Access.UserHeader, opts.Access.GroupsHeader = identityHeaders(cfg.Access, mode)
	md := llms.New(view, cfg.Server.PublicURL, llmsOptions(opts.Origin, f))
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
	// The hooks that add to a repository's summary, in the order of its fields; the pull requests' only when the feature is on.
	repoHooks := []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.Repo){commits.Repo, releases.Repo, manifests.Repo, packages.Repo}
	if f.Pulls {
		repoHooks = append(repoHooks, pulls.Repo)
	}
	repoHooks = append(repoHooks, repoIssuesAPI, repoHealthAPI)
	catalogAPI := catalog.API{Current: view,
		Dashboard:    []func(*snapshot.Snapshot, *oapi.Dashboard){commits.Dashboard, dashboardHealthAPI, dashboardSummaryAPI},
		Group:        []func(*snapshot.Snapshot, snapshot.Group, *oapi.Group){groupHealthAPI},
		Repo:         repoHooks,
		Detail:       []func(*snapshot.RepoEntry, *oapi.RepoDetail){commits.Detail},
		ArchiveEntry: []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.ArchiveEntry){manifests.ArchiveEntry, commits.ArchiveEntry}}
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
	catalogue, err := toolReads{find: searchAPI, catalog: catalogAPI, commits: commitsAPI, pulls: pullsAPI, issues: issuesAPI, releases: newReleases(view, documents), insights: newInsights(view), repo: md.Repo(), doc: md.Doc(), group: md.Group(), features: f}.catalogue()
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
	features := api{catalogAPI, commitsAPI, pullsAPI, issuesAPI, newReleases(view, documents), newInsights(view), docs.API{Current: view}, manifests.API{Current: view},
		layout.API{Current: view, Store: s.layouts, Notices: s.notices.For, Off: offElements(f)},
		httpapi.SnapshotAPI{Current: view}, me, ask, agents, searchAPI, rulesAPI{f}, s.prereqs}
	if s.web != nil {
		opts.Build = s.web.Build
	}
	for _, r := range routes {
		s.routes = append(s.routes, r.Pattern)
	}
	slices.Sort(s.routes)
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
func newAuth(cfg config.Config, log *slog.Logger, launch *auth.Launch) (*auth.Auth, error) {
	a := cfg.Auth
	named := slices.Concat(cfg.Access.PrivateGroups, a.AdminGroups)
	o := auth.Options{SessionTTL: a.SessionTTL, PublicURL: cfg.Server.PublicURL, Log: log, Launch: launch,
		Keep: func(groups []string) []string {
			return slices.DeleteFunc(slices.Clone(groups), func(g string) bool { return !slices.Contains(named, g) })
		}}
	if a.SignIn() {
		o.SessionKey = a.SessionKey.Reveal()
	}
	for _, t := range a.Tokens {
		o.Tokens = append(o.Tokens, auth.Token{Name: t.Name, Secret: t.Secret.Reveal(), Groups: t.Groups, Admin: t.Admin, Login: t.Login})
	}
	if a.OIDC.Issuer != "" {
		o.OIDC = &auth.OIDC{Issuer: a.OIDC.Issuer, ClientID: a.OIDC.ClientID, ClientSecret: string(a.OIDC.ClientSecret.Reveal()),
			Scopes: a.OIDC.Scopes, GroupsClaim: a.OIDC.GroupsClaim, RequireVerifiedEmail: a.OIDC.RequireVerifiedEmail,
			IdentityClaim: a.OIDC.IdentityClaim, NoUserInfo: !a.OIDC.UserInfo}
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

// eventRoutes wires what tells the portal about changes: the inbox that makes events survive a restart, the catch-up of each App's
// delivery log, and with a webhook secret the receiver (the returned route).
func (s *server) eventRoutes(cfg config.Config, gh *github.Client, m *metrics, st *status, feed bool, log *slog.Logger) []httpapi.Route {
	if !feed {
		return nil
	}
	s.inbox = &inbox{park: s.store, deliver: s.deliver, handled: s.engine.Handled, log: log, now: time.Now}
	observe := func(event, result string) { m.delivery(event, result); st.delivery(event, result) }
	for _, src := range cfg.GitHub.Sources {
		if g := gh.Source(src.ID); g.App() {
			s.inbox.catchUps = append(s.inbox.catchUps, catchUp(g, src.ID, s.store, s.pub.Current, webhookSink(s.inbox.take, cfg.GitHub.Sources), observe, log))
		}
	}
	st.catchUps = func() []webhook.CatchUpState {
		out := make([]webhook.CatchUpState, 0, len(s.inbox.catchUps))
		for _, c := range s.inbox.catchUps {
			out = append(out, c.State())
		}
		return out
	}
	if !cfg.Webhook.Secret.IsSet() { // GitHub cannot reach the portal: the delivery logs are the only way events arrive
		return nil
	}
	rc := webhook.New(cfg.Webhook.Secret.Reveal(), cfg.Webhook.PreviousSecret.Reveal(), webhookSink(s.inbox.take, cfg.GitHub.Sources), log)
	if s.member != nil {
		rc.Dedupe(s.member.First)
	}
	rc.Observe(observe)
	return []httpapi.Route{{Pattern: webhook.Pattern, MaxBody: webhook.MaxBody, Handler: rc}}
}

// unloggedSources are the sources a receiver tells about changes but whose missed deliveries nothing can recover: no App, so no
// delivery log to read (EV4, EV5). They are in "events" mode, but a delivery sent while the portal was down is lost, so the jobs
// with a faster poll under events (sync.Spec.PollTTL: the releases) run at it as for a polled source.
func unloggedSources(cfg config.Config) []string {
	var out []string
	for _, src := range cfg.GitHub.Sources {
		if cfg.Webhook.Secret.IsSet() && src.App == nil {
			out = append(out, src.ID)
		}
	}
	return out
}

// polledSources are the sources GitHub does not tell about changes: with a webhook receiver every source is told (the operator set the
// webhooks), without one only an App whose delivery log is read (webhook.catch_up) is; the others are polled, and the jobs whose TTL is
// only a net under events (sync.Spec.PollTTL) run at their poll cadence.
func polledSources(cfg config.Config) []string {
	var out []string
	for _, src := range cfg.GitHub.Sources {
		if !cfg.Webhook.Secret.IsSet() && (!cfg.Webhook.CatchUp || src.App == nil) {
			out = append(out, src.ID)
		}
	}
	return out
}

// themed loads the deployment's look (design/theme.md) and the route that serves its files, which exists only with web.theme_dir.
func themed(w config.Web) (*theme.Theme, []httpapi.Route, error) {
	look, err := theme.Load(w.ThemeDir, w.ProductName)
	if err != nil {
		return nil, nil, fmt.Errorf("web: %w", err)
	}
	if w.ThemeDir == "" {
		return look, nil, nil
	}
	return look, []httpapi.Route{{Pattern: "GET " + theme.Prefix + "{file...}", Handler: look.Handler()}}, nil
}
