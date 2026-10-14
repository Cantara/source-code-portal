// Package catalog discovers the public repositories of the configured organisations, tracks their identity and old addresses,
// assigns them to the configured groups and contributes repositories, groups and aliases to the snapshot.
package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// Kind is the job kind of the per-source listing.
const Kind = "catalog.reconcile"

const schema = 1

// GitHub is what the catalogue reads from one source; *github.Source implements it.
type GitHub interface {
	RefreshInstallations(ctx context.Context) error
	ListRepos(ctx context.Context, owner string) ([]snapshot.Repo, error)
	Tree(ctx context.Context, owner, name, ref string, recursive bool) ([]github.TreeEntry, error)
	Blob(ctx context.Context, owner, name, sha string) ([]byte, error)
}

// Catalog is the catalogue feature.
type Catalog struct {
	groups   *Groups
	sources  map[string]source
	interval time.Duration
	notify   func(sync.Event, snapshot.RepoKey)
	log      *slog.Logger
}

type source struct {
	orgs   []string
	filter Filter
	gh     GitHub
}

// state is the payload of a source's listing item: its tracked repositories.
type state struct {
	Schema int       `json:"schema"`
	Repos  []Tracked `json:"repos"`
}

// New builds the catalogue for the configured sources, reading GitHub through one GitHub per source id. notify (may be nil) receives
// EventPush for every tracked repository whose pushed_at changed in a listing; *sync.Engine's Notify fits.
func New(cfg config.Catalog, configured []config.Source, sources map[string]GitHub, notify func(sync.Event, snapshot.RepoKey),
	log *slog.Logger) (*Catalog, error) {
	groups, findings := Compile(cfg.Groups)
	for _, f := range findings {
		if !f.Error {
			log.Warn("catalog configuration", "warning", f.Message)
		}
	}
	if groups == nil {
		return nil, findingsError(findings)
	}
	if notify == nil {
		notify = func(sync.Event, snapshot.RepoKey) {}
	}
	c := &Catalog{groups: groups, sources: map[string]source{}, interval: cfg.ReconcileInterval, notify: notify, log: log}
	for _, s := range configured {
		g := sources[s.ID]
		if g == nil {
			return nil, fmt.Errorf("catalog: no GitHub for source %s", s.ID)
		}
		c.sources[s.ID] = source{orgs: s.Orgs, gh: g, filter: Filter{IncludeForks: s.IncludeForks, IncludeArchived: s.IncludeArchived}}
	}
	return c, nil
}

func findingsError(fs []Finding) error {
	var msgs []string
	for _, f := range fs {
		if f.Error {
			msgs = append(msgs, f.Message)
		}
	}
	return errors.New("invalid groups:\n  " + strings.Join(msgs, "\n  "))
}

// Spec is the per-source listing job.
func (c *Catalog) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, PerSource: true, Prio: sync.PrioScheduled, TTL: c.interval, On: []sync.Event{sync.EventSourceChanged},
		Run: c.run, Publish: c.publish}
}

// errEmpty marks a complete listing without repositories for a source that had some: usually a token or scope problem.
var errEmpty = errors.New("the listing returned no repositories; keeping the last catalogue")

func (c *Catalog) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	src, ok := c.sources[j.Source]
	if !ok {
		return sync.Result{}, fmt.Errorf("catalog: unknown source %s", j.Source)
	}
	// App installations come and go (installed, suspended, removed); every listing starts from the current set.
	if err := src.gh.RefreshInstallations(ctx); err != nil {
		return sync.Result{}, fmt.Errorf("installations: %w", err)
	}
	var listed []snapshot.Repo
	for _, org := range src.orgs {
		rs, err := src.gh.ListRepos(ctx, org)
		if err != nil {
			return sync.Result{}, fmt.Errorf("list %s: %w", org, err)
		}
		listed = append(listed, rs...)
	}
	prev := decode(j.Prev.Payload)
	if len(listed) == 0 && len(prev.Repos) > 0 {
		return sync.Result{}, errEmpty
	}
	for i := range listed {
		listed[i].ID.Source = j.Source
	}
	next := state{Schema: schema, Repos: Plan(listed, prev.Repos, src.filter)}
	pushed := map[int64]time.Time{}
	for _, t := range prev.Repos {
		pushed[t.Repo.ID.GitHubID] = t.Repo.PushedAt
	}
	for _, t := range next.Repos {
		if at, was := pushed[t.Repo.ID.GitHubID]; was && !at.Equal(t.Repo.PushedAt) {
			c.notify(sync.EventPush, t.Repo.Key())
		}
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return sync.Result{}, err
	}
	res := sync.Result{Payload: payload, Catalogue: make([]store.Repo, 0, len(next.Repos))}
	for _, t := range next.Repos {
		res.Catalogue = append(res.Catalogue, store.Repo{Key: t.Repo.Key(), ID: t.Repo.ID})
	}
	res.Unchanged = j.Prev.State == snapshot.StateOK && bytes.Equal(payload, j.Prev.Payload)
	return res, nil
}

func decode(payload []byte) state {
	var s state
	if len(payload) == 0 || json.Unmarshal(payload, &s) != nil || s.Schema != schema {
		return state{}
	}
	return s
}

// publish puts a source's catalogue into the snapshot and resolves the groups over every source's repositories.
func (c *Catalog) publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	id := strings.TrimPrefix(it.Key, Kind+"/")
	if _, ok := c.sources[id]; !ok {
		return
	}
	st := decode(it.Payload)
	entries := make([]*snapshot.RepoEntry, 0, len(st.Repos))
	aliases := map[snapshot.RepoKey]snapshot.RepoKey{}
	for _, t := range st.Repos {
		if t.Repo.Visibility != "public" {
			continue // fail closed on stored rows too
		}
		entries = append(entries, &snapshot.RepoEntry{Repo: t.Repo, Fresh: fresh})
		for _, a := range t.Aliases {
			aliases[a] = t.Repo.Key()
		}
	}
	b.ReplaceSource(id, entries, aliases, snapshot.SourceStatus{ID: id, Fresh: fresh, Repos: len(entries)})
	b.SetGroups(c.groups.Resolve(b.Repos()))
	listed := 0
	for _, s := range b.Sources() {
		if _, ok := c.sources[s.ID]; ok && !s.Fresh.CheckedAt.IsZero() {
			listed++
		}
	}
	if listed == len(c.sources) {
		b.SetCatalogued()
	}
}
