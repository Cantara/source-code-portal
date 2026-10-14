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
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// Kind is the job kind of the per-source listing.
const Kind = "catalog.reconcile"

const schema = 1

// Lister lists an organisation's repositories; *github.Source implements it.
type Lister interface {
	ListRepos(ctx context.Context, owner string) ([]snapshot.Repo, error)
}

// Catalog is the catalogue feature.
type Catalog struct {
	groups   *Groups
	sources  map[string]source
	interval time.Duration
	log      *slog.Logger
}

type source struct {
	orgs   []string
	filter Filter
	lister Lister
}

// state is the payload of a source's listing item: its tracked repositories.
type state struct {
	Schema int       `json:"schema"`
	Repos  []Tracked `json:"repos"`
}

// New builds the catalogue for the configured sources. listers has one Lister per source id.
func New(cfg config.Catalog, sources []config.Source, listers map[string]Lister, log *slog.Logger) (*Catalog, error) {
	groups, findings := Compile(cfg.Groups)
	for _, f := range findings {
		if !f.Error {
			log.Warn("catalog configuration", "warning", f.Message)
		}
	}
	if groups == nil {
		return nil, findingsError(findings)
	}
	c := &Catalog{groups: groups, sources: map[string]source{}, interval: cfg.ReconcileInterval, log: log}
	for _, s := range sources {
		l := listers[s.ID]
		if l == nil {
			return nil, fmt.Errorf("catalog: no lister for source %s", s.ID)
		}
		c.sources[s.ID] = source{orgs: s.Orgs, lister: l, filter: Filter{IncludeForks: s.IncludeForks, IncludeArchived: s.IncludeArchived}}
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
	var listed []snapshot.Repo
	for _, org := range src.orgs {
		rs, err := src.lister.ListRepos(ctx, org)
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
