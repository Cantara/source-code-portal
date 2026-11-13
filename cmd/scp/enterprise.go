package main

import (
	"context"
	"log/slog"
	gosync "sync"

	"github.com/spf13/cobra"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/lru"
	"github.com/cantara/source-code-portal/internal/people"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// newEnterpriseCmd is scp enterprise (design/enterprise.md, EN3): a portal of GitHub Enterprise sources read with the tokens of the
// people who sign in, each of whom sees only what GitHub lets them read. Its store belongs to the mode, and its configuration is one
// of the mode (enterprise.github[].people): scp serve refuses that configuration and this command any other.
func newEnterpriseCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enterprise",
		Short: "Run a GitHub Enterprise portal read with its people's own tokens, until SIGINT or SIGTERM",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServer(cmd, opts, deps{mode: store.Enterprise})
		},
	}
	cmd.Flags().String("public-addr", "", "public listener address (default :9090)")
	cmd.Flags().String("admin-addr", "", "admin listener address (default :9091)")
	return cmd
}

// allowedFunc is what a request's viewer may read in the enterprise mode: the repositories by their GitHub ids, and a key that is equal
// for equal sets (the views are kept by it). Nil or empty: nothing, which is what a viewer without a fresh access list gets (fail
// closed).
type allowedFunc func(ctx context.Context) (key string, allowed map[snapshot.RepoID]bool)

// peopleViews is the one place the enterprise mode decides what a request sees: the published snapshot restricted to the viewer's
// set (snapshot.Restrict), kept per snapshot, set and choice of forks, the least recently used dropped first.
type peopleViews struct {
	current func() *snapshot.Snapshot
	allowed allowedFunc
	log     *slog.Logger

	mu    gosync.Mutex
	cache *lru.Cache[viewKey, *snapshot.Snapshot]
	keyed *snapshot.Snapshot                   // the snapshot keys is of
	keys  map[snapshot.RepoID]snapshot.RepoKey // its repositories' addresses by GitHub id
}

type viewKey struct {
	of    *snapshot.Snapshot
	set   string
	forks bool
}

// viewsKept is how many restricted views are held: a few per distinct set while a snapshot is replaced.
const viewsKept = 64

func newPeopleViews(current func() *snapshot.Snapshot, allowed allowedFunc, log *slog.Logger) *peopleViews {
	return &peopleViews{current: current, allowed: allowed, log: log, cache: lru.New[viewKey, *snapshot.Snapshot](viewsKept, nil)}
}

// of is the view the request's viewer reads, with the hidden forks when forks is true.
func (p *peopleViews) of(ctx context.Context, forks bool) *snapshot.Snapshot {
	s := p.current()
	key, ids := p.allowed(ctx)
	if len(ids) == 0 {
		key = ""
	}
	k := viewKey{of: s, set: key, forks: forks}
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.cache.Get(k); ok {
		return v
	}
	if p.keyed != s {
		p.keyed, p.keys = s, make(map[snapshot.RepoID]snapshot.RepoKey, len(s.Repos))
		for k, e := range s.Repos {
			p.keys[e.Repo.ID] = k
		}
	}
	allowed := make(map[snapshot.RepoKey]bool, len(ids))
	for id := range ids {
		if k, ok := p.keys[id]; ok {
			allowed[k] = true
		}
	}
	v := s.Restrict(allowed, forks, p.log)
	p.cache.Put(k, v)
	return v
}

// nobody is the access of every viewer when there are no people to ask: nothing.
func nobody(context.Context) (string, map[snapshot.RepoID]bool) { return "", nil }

// allowedOf is the enterprise mode's access: the signed-in person's fresh lists, nothing for anyone else.
func allowedOf(pp *people.People) allowedFunc {
	if pp == nil {
		return nobody
	}
	return func(ctx context.Context) (string, map[snapshot.RepoID]bool) {
		v := httpapi.ViewerOf(ctx)
		if v.Via != "oidc" || v.User == "" {
			return "", nil
		}
		return pp.Allowed(v.User)
	}
}

// open opens the store for the server's mode and, in the enterprise mode, the people it holds.
func (s *server) open(ctx context.Context, cfg config.Config, gh *github.Client) error {
	var err error
	if s.store, err = store.OpenAs(ctx, cfg.Store.Path, s.mode, s.log); err != nil {
		return err
	}
	if s.mode != store.Enterprise {
		return nil
	}
	var sources []people.Source
	for _, src := range cfg.GitHub.Sources {
		g := gh.Source(src.ID)
		sources = append(sources, people.Source{ID: src.ID, WebURL: g.WebURL(), Owners: src.Orgs, Login: src.Login, Check: g.CheckToken, List: g.ListWith})
	}
	s.people, err = people.Open(ctx, people.Options{Store: s.store, Key: cfg.Enterprise.TokenKey.Reveal(), Issuer: cfg.Auth.OIDC.Issuer,
		Sources: sources, Portal: cfg.Server.PublicURL, Refresh: cfg.Enterprise.Access.Refresh, MaxAge: cfg.Enterprise.Access.MaxAge,
		Log: s.log.With("component", "people")})
	if err != nil {
		_ = s.store.Close()
	}
	return err
}

// peopleRoutes are the enterprise mode's own routes: a person's GitHub tokens. None in the other modes.
func (s *server) peopleRoutes(cfg config.Config) []httpapi.Route {
	if s.people == nil {
		return nil
	}
	return s.people.Routes(cfg.Server.PublicURL)
}
