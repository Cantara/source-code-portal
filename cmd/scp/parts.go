package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/app/fleethealth"
	"github.com/cantara/source-code-portal/internal/app/searchfeed"
	"github.com/cantara/source-code-portal/internal/feature/announce"
	"github.com/cantara/source-code-portal/internal/feature/badges"
	"github.com/cantara/source-code-portal/internal/feature/catalog"
	"github.com/cantara/source-code-portal/internal/feature/commits"
	"github.com/cantara/source-code-portal/internal/feature/docs"
	"github.com/cantara/source-code-portal/internal/feature/issues"
	"github.com/cantara/source-code-portal/internal/feature/layout"
	"github.com/cantara/source-code-portal/internal/feature/llms"
	"github.com/cantara/source-code-portal/internal/feature/manifests"
	"github.com/cantara/source-code-portal/internal/feature/packages"
	"github.com/cantara/source-code-portal/internal/feature/pulls"
	"github.com/cantara/source-code-portal/internal/feature/releases"
	"github.com/cantara/source-code-portal/internal/feature/search"
	"github.com/cantara/source-code-portal/internal/feature/visibility"
	"github.com/cantara/source-code-portal/internal/lib/egress"
	"github.com/cantara/source-code-portal/internal/platform/auth"
	"github.com/cantara/source-code-portal/internal/platform/cluster"
	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/docstore"
	"github.com/cantara/source-code-portal/internal/platform/github"
	"github.com/cantara/source-code-portal/internal/platform/httpapi"
	"github.com/cantara/source-code-portal/internal/platform/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/sync"
	"github.com/cantara/source-code-portal/internal/platform/theme"
)

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
func llmsOptions(origin func(*http.Request) string, f config.Features, vis *visibility.Set) llms.Options {
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
		Freshness:     func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := commits.Of(e); return f },
		Indexed:       vis.Indexed,
		IndexedGroup:  vis.IndexedGroup,
		IndexRevision: vis.Revision,
		LastCommit:    func(e *snapshot.RepoEntry) time.Time { t, _ := commits.LastCommit(e); return t },
		Origin:        origin,
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
	d := badges.Data{LastCommit: commits.LastCommit, Release: latestRelease, Health: fleethealth.Assess, Product: fleethealth.Group, Issues: fleethealth.IssueCount}
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

// timed passes h on and tells took how long each answer took: the webhook receiver's, against GitHub's 10 seconds.
func timed(h http.Handler, took func(time.Duration)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		took(time.Since(start))
	})
}

// commitRules are the built-in rules of a commit's origin with the deployment's assistants added (commits.assistants, P-127).
func commitRules(c config.Commits) *commits.Rules {
	added := make([]commits.AssistantRule, 0, len(c.Assistants))
	for _, a := range c.Assistants {
		added = append(added, commits.AssistantRule{Name: a.Name, Authors: a.Authors, CoAuthors: a.CoAuthors})
	}
	return commits.NewRules(added)
}

// readers is what each feature reads GitHub through, per source id.
type readers struct {
	ids             []string
	sources         map[string]catalog.GitHub
	fetchers        map[string]commits.Fetcher
	trees           map[string]docs.GitHub
	manifestTrees   map[string]manifests.GitHub
	packageTrees    map[string]packages.GitHub
	releaseFetchers map[string]releases.Fetcher
	openPulls       map[string]pulls.Fetcher
	issueFetchers   map[string]issues.Fetcher
}

func readersOf(cfg config.Config, gh *github.Client) readers {
	r := readers{sources: map[string]catalog.GitHub{}, fetchers: map[string]commits.Fetcher{}, trees: map[string]docs.GitHub{},
		manifestTrees: map[string]manifests.GitHub{}, packageTrees: map[string]packages.GitHub{}, releaseFetchers: map[string]releases.Fetcher{},
		openPulls: map[string]pulls.Fetcher{}, issueFetchers: map[string]issues.Fetcher{}, ids: make([]string, 0, len(cfg.GitHub.Sources))}
	for _, s := range cfg.GitHub.Sources {
		src := gh.Source(s.ID)
		r.sources[s.ID], r.trees[s.ID], r.manifestTrees[s.ID], r.packageTrees[s.ID] = src, src, src, src
		r.fetchers[s.ID], r.releaseFetchers[s.ID], r.openPulls[s.ID] = commits.FromGitHub(src), releases.FromGitHub(src), pulls.FromGitHub(src)
		r.issueFetchers[s.ID] = issues.FromGitHub(src)
		r.ids = append(r.ids, s.ID)
	}
	return r
}

// indexersOf is the snapshot's indexes of the features switched on.
func indexersOf(cfg config.Config) []snapshot.Indexer {
	indexers := []snapshot.Indexer{commits.Feeds(commitRules(cfg.Commits)), manifests.Graph(), search.Indexer(searchfeed.Options())}
	if cfg.Features.Pulls {
		indexers = append(indexers, pulls.Feeds())
	}
	if cfg.Features.Issues {
		indexers = append(indexers, issues.Feeds())
	}
	return indexers
}

// joinCluster makes the engine one of several instances when cluster addresses are configured.
func (s *server) joinCluster(c config.Cluster, d deps, log *slog.Logger) {
	if len(c.Addresses) == 0 {
		return
	}
	dial := d.cluster
	if dial == nil {
		dial = cluster.DialHazelcast(c.Addresses, c.Name, c.Namespace, log)
	}
	s.member = cluster.New(dial, log)
	s.engine.Share(s.member.Share)
}

// registerJobs registers the jobs of the features switched on. Registration order is publish order at the warm start: the catalogue
// first, then what hangs off its repositories.
func (s *server) registerJobs(cfg config.Config, d deps, rd readers, cat *catalog.Catalog, documents *docs.Docs) error {
	catalogued := func(k snapshot.RepoKey) (snapshot.Repo, bool) {
		if e := s.pub.Current().Repos[k]; e != nil {
			return e.Repo, true
		}
		return snapshot.Repo{}, false
	}
	specs := []sync.Spec{cat.Spec(), commits.New(rd.fetchers).Spec(), documents.Spec(), cat.DescriptorSpec(),
		releases.New(rd.releaseFetchers).Spec(), manifests.New(rd.manifestTrees).Spec(), newPackages(cfg.Packages, rd.packageTrees, d, catalogued).Spec()}
	if cfg.Features.Pulls {
		specs = append(specs, pulls.New(rd.openPulls, pulls.Options{Repo: catalogued}).Spec())
	}
	if cfg.Features.Issues {
		specs = append(specs, issues.New(rd.issueFetchers, issues.Options{Repo: catalogued}).Spec())
	}
	for _, spec := range specs {
		if err := s.engine.Register(spec); err != nil {
			return err
		}
	}
	return nil
}

// openDocuments opens what admins edit in the pages: the layouts, the announcements and who sees what, each over its base in the
// configuration (design/config-baseline.md), after taking over what layout.dir held.
func (s *server) openDocuments(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	imported, err := docstore.Import(ctx, s.store, cfg.Layout.Dir, append(layout.Names(), announce.Name))
	if err != nil {
		return fmt.Errorf("layout.dir: %w", err)
	}
	if len(imported) > 0 {
		log.Info("layouts and announcements from layout.dir are now kept in the store; the files are no longer read", "imported", imported)
	}
	baseVisibility, baseLayouts, problems := baseline(cfg.Baseline)
	if len(problems) > 0 {
		return &config.Invalid{Problems: problems}
	}
	s.layouts = layout.Open(ctx, s.store, baseLayouts, log)
	s.notices = announce.Open(ctx, cfg.Announcements, s.store, time.Now, log)
	// A save of who sees what publishes again: the views are rebuilt with the new audiences (design/visibility.md).
	s.visibility = visibility.Open(ctx, s.store, baseVisibility, func() { s.pub.Update(func(*snapshot.Builder) {}) }, log)
	s.pub.Audience(s.visibility.Audience)
	return nil
}

// polledSet is polledSources as a set.
func polledSet(cfg config.Config) map[string]bool {
	out := map[string]bool{}
	for _, id := range polledSources(cfg) {
		out[id] = true
	}
	return out
}

// catalogWith is the catalog API with the hooks other features add to its answers; the hooks that add to a repository's summary
// come in the order of its fields, the pull requests' only when the feature is on.
func catalogWith(view func(context.Context) *snapshot.Snapshot, f config.Features) catalog.API {
	repoHooks := []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.Repo){commits.Repo, releases.Repo, manifests.Repo, packages.Repo}
	if f.Pulls {
		repoHooks = append(repoHooks, pulls.Repo)
	}
	repoHooks = append(repoHooks, fleethealth.RepoIssuesAPI, fleethealth.RepoAPI)
	a := catalog.API{Current: view,
		Dashboard:    []func(*snapshot.Snapshot, *oapi.Dashboard){commits.Dashboard, fleethealth.DashboardAPI, fleethealth.DashboardSummary},
		Group:        []func(*snapshot.Snapshot, snapshot.Group, *oapi.Group){fleethealth.GroupAPI},
		Repo:         repoHooks,
		Detail:       []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.RepoDetail){commits.Detail},
		ArchiveEntry: []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.ArchiveEntry){manifests.ArchiveEntry, commits.ArchiveEntry}}
	if f.Pulls {
		a.Dashboard = append(a.Dashboard, pulls.Dashboard)
	}
	return a
}

// adminRoutes are the admin port's routes: the metrics, the status and, with ops.pprof, the profiler.
func adminRoutes(o config.Ops, m *metrics, st *status) []httpapi.Route {
	admin := []httpapi.Route{{Pattern: "GET /metrics", Handler: m.handler()}, {Pattern: "GET /api/v1/status", Handler: st}}
	if o.Pprof {
		admin = append(admin, httpapi.Route{Pattern: "GET /debug/pprof/", Handler: http.HandlerFunc(pprof.Index)},
			httpapi.Route{Pattern: "GET /debug/pprof/cmdline", Handler: http.HandlerFunc(pprof.Cmdline)},
			httpapi.Route{Pattern: "GET /debug/pprof/profile", Handler: http.HandlerFunc(pprof.Profile)},
			httpapi.Route{Pattern: "GET /debug/pprof/symbol", Handler: http.HandlerFunc(pprof.Symbol)},
			httpapi.Route{Pattern: "GET /debug/pprof/trace", Handler: http.HandlerFunc(pprof.Trace)})
	}
	return admin
}

// listen opens the public and the admin listeners.
func (s *server) listen(ctx context.Context, c config.Server) error {
	var lc net.ListenConfig
	var err error
	if s.publicLn, err = lc.Listen(ctx, "tcp", c.PublicAddr); err != nil {
		return err
	}
	if s.adminLn, err = lc.Listen(ctx, "tcp", c.AdminAddr); err != nil {
		_ = s.publicLn.Close()
		return err
	}
	return nil
}
