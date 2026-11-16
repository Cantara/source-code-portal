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
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/github"
	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
	"github.com/cantara/source-code-portal/internal/platform/sync"
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
	app    bool // reads through a GitHub App: an empty listing is GitHub's word, not a credential problem
}

// state is the payload of a source's listing item: its tracked repositories and the accounts that failed to list in a row.
type state struct {
	Schema  int            `json:"schema"`
	Repos   []Tracked      `json:"repos"`
	Failing map[string]int `json:"failing,omitempty"` // account (lower case) to the listings in a row it failed in
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
		f, err := SourceFilter(s)
		if err != nil {
			return nil, err
		}
		a, _ := g.(app)
		c.sources[s.ID] = source{orgs: s.Orgs, gh: g, filter: f, app: a != nil && a.App()}
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
	return sync.Spec{Kind: Kind, PerSource: true, Lists: true, Prio: sync.PrioScheduled, TTL: c.interval, On: []sync.Event{sync.EventSourceChanged},
		Run: c.run, Publish: c.publish}
}

// errEmpty marks a complete listing without repositories for a token source that had some: usually a token or scope problem. An App's
// listing is GitHub's own word on what the installation was given (none selected, or the App removed), so it may be empty.
var errEmpty = errors.New("the listing returned no repositories; keeping the last catalogue")

// retryAccount is how soon a listing runs again after an account of the source failed to list once (its last repositories were
// kept); it doubles with each listing in a row the account fails in, up to the listing's interval.
const retryAccount = time.Minute

// app is implemented by a GitHub source that reports whether it reads through a GitHub App (*github.Source does).
type app interface{ App() bool }

func (c *Catalog) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	src, ok := c.sources[j.Source]
	if !ok {
		return sync.Result{}, fmt.Errorf("catalog: unknown source %s", j.Source)
	}
	// App installations come and go (installed, suspended, removed); every listing starts from the current set.
	if err := src.gh.RefreshInstallations(ctx); err != nil {
		return sync.Result{}, fmt.Errorf("installations: %w", err)
	}
	prev := decode(j.Prev.Payload)
	got, err := c.list(ctx, j.Source, src, prev)
	if err != nil {
		return sync.Result{}, err
	}
	listed := got.repos
	if len(listed) == 0 && len(got.kept) == 0 && len(prev.Repos) > 0 && !src.app {
		return sync.Result{}, errEmpty
	}
	for i := range listed {
		listed[i].ID.Source = j.Source
	}
	next := state{Schema: schema, Repos: keep(Plan(listed, without(prev.Repos, got.kept), src.filter), got.kept), Failing: got.failing}
	before := map[int64]snapshot.Repo{}
	for _, t := range prev.Repos {
		before[t.Repo.ID.GitHubID] = t.Repo
	}
	for _, t := range next.Repos {
		was, known := before[t.Repo.ID.GitHubID]
		if !known {
			continue
		}
		if !was.PushedAt.Equal(t.Repo.PushedAt) {
			c.notify(sync.EventPush, t.Repo.Key())
		}
		// The count of open issues and pull requests moved: whatever missed its webhook is read within the listing's interval.
		if was.OpenIssues != t.Repo.OpenIssues {
			c.notify(sync.EventOpen, t.Repo.Key())
		}
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return sync.Result{}, err
	}
	res := sync.Result{Payload: payload, Catalogue: make([]store.Repo, 0, len(next.Repos))}
	for _, t := range next.Repos {
		res.Catalogue = append(res.Catalogue, store.Repo{Key: t.Repo.Key(), ID: t.Repo.ID, Visibility: t.Repo.Visibility})
	}
	res.Unchanged = j.Prev.State == snapshot.StateOK && bytes.Equal(payload, j.Prev.Payload)
	if slices.ContainsFunc(next.Repos, func(t Tracked) bool { return t.Misses > 0 }) {
		res.Again = confirmAfter // a repository the listing missed (deleted, transferred) leaves at the confirming listing
	}
	if again := c.retry(got.failing); again > 0 && (res.Again == 0 || res.Again > again) {
		res.Again = again
	}
	return res, nil
}

// retry is how soon the listing runs again for the accounts that failed: retryAccount doubled for each listing in a row the most
// failing one failed in, at most the listing's interval; zero when none failed.
func (c *Catalog) retry(failing map[string]int) time.Duration {
	n := 0
	for _, f := range failing {
		n = max(n, f)
	}
	if n == 0 {
		return 0
	}
	d := retryAccount << min(n-1, 16)
	if c.interval > 0 && d > c.interval {
		d = c.interval
	}
	return d
}

// without is current less the entries kept as they were.
func without(current, kept []Tracked) []Tracked {
	if len(kept) == 0 {
		return current
	}
	ids := map[int64]bool{}
	for _, t := range kept {
		ids[t.Repo.ID.GitHubID] = true
	}
	return slices.DeleteFunc(slices.Clone(current), func(t Tracked) bool { return ids[t.Repo.ID.GitHubID] })
}

// keep adds the entries of the accounts that failed to list, unchanged (their misses neither counted nor reset), unless the listing
// found the repository or its address elsewhere.
func keep(planned, kept []Tracked) []Tracked {
	if len(kept) == 0 {
		return planned
	}
	ids, keys := map[int64]bool{}, map[snapshot.RepoKey]bool{}
	for _, t := range planned {
		ids[t.Repo.ID.GitHubID], keys[t.Repo.Key()] = true, true
	}
	for _, t := range kept {
		if !ids[t.Repo.ID.GitHubID] && !keys[t.Repo.Key()] {
			planned = append(planned, t)
		}
	}
	slices.SortFunc(planned, func(a, b Tracked) int { return strings.Compare(string(a.Repo.Key()), string(b.Repo.Key())) })
	return planned
}

// listing is what one run read of a source's accounts.
type listing struct {
	repos   []snapshot.Repo
	kept    []Tracked      // the last entries of the accounts that failed to list, kept as they were
	failing map[string]int // the accounts that failed, with the listings in a row they failed in
}

// list reads every account of the source.
//   - An account GitHub says the App is not installed on, or does not have (deleted, renamed), lists nothing: its repositories leave as
//     any the listing misses.
//   - A rate limit or a refused credential is the source's, not the account's: it is returned, so the engine pauses the source.
//   - Any other failure keeps the account's last entries as they were, so one account does not stop the others; failing counts the
//     listings in a row it failed in, for the back-off. The error is returned only when no account listed at all.
func (c *Catalog) list(ctx context.Context, id string, src source, prev state) (listing, error) {
	var out listing
	var errs []error
	for _, org := range src.orgs {
		rs, err := src.gh.ListRepos(ctx, org)
		switch {
		case err == nil:
			out.repos = append(out.repos, rs...)
		case github.NotInstalled(err):
			c.log.Info("the GitHub App is not installed on the account; its repositories leave the catalogue", "source", id, "account", org)
		case github.IsNotFound(err):
			c.log.Warn("GitHub does not know the account (deleted or renamed?); its repositories leave the catalogue", "source", id, "account", org)
		case ctx.Err() != nil:
			return listing{}, ctx.Err()
		case sourceWide(err):
			return listing{}, fmt.Errorf("list %s: %w", org, err)
		default:
			errs = append(errs, fmt.Errorf("list %s: %w", org, err))
			if out.failing == nil {
				out.failing = map[string]int{}
			}
			out.failing[strings.ToLower(org)] = prev.Failing[strings.ToLower(org)] + 1
			for _, t := range prev.Repos {
				if strings.EqualFold(t.Repo.Owner, org) {
					out.kept = append(out.kept, t)
				}
			}
		}
	}
	if len(errs) > 0 && len(errs) == len(src.orgs) {
		return listing{}, errs[0]
	}
	if len(errs) > 0 {
		c.log.Warn("an account of the source failed to list; its last repositories are kept", "source", id, "err", errors.Join(errs...))
	}
	return out, nil
}

// sourceWide reports whether err stops every read of the source (a rate limit, a refused credential), not just one account's.
func sourceWide(err error) bool {
	var e *github.Error
	return errors.As(err, &e) && (e.Kind == github.RateLimited || e.Kind == github.Auth)
}

func decode(payload []byte) state {
	var s state
	if len(payload) == 0 || json.Unmarshal(payload, &s) != nil || s.Schema != schema {
		return state{}
	}
	return s
}

// showForks is the sources whose forks are shown by default (include_forks).
func (c *Catalog) showForks() map[string]bool {
	out := map[string]bool{}
	for id, s := range c.sources {
		out[id] = s.filter.ShowForks
	}
	return out
}

// publish puts a source's catalogue into the snapshot and resolves the groups over every source's repositories.
func (c *Catalog) publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	id := strings.TrimPrefix(it.Key, Kind+"/")
	src, ok := c.sources[id]
	if !ok {
		return
	}
	st := decode(it.Payload)
	entries := make([]*snapshot.RepoEntry, 0, len(st.Repos))
	aliases := map[snapshot.RepoKey]snapshot.RepoKey{}
	for _, t := range st.Repos {
		if !src.filter.Visible(t.Repo.Visibility) {
			continue // fail closed on stored rows too
		}
		entries = append(entries, &snapshot.RepoEntry{Repo: t.Repo, Fresh: fresh})
		for _, a := range t.Aliases {
			aliases[a] = t.Repo.Key()
		}
	}
	b.ReplaceSource(id, entries, aliases, snapshot.SourceStatus{ID: id, Fresh: fresh, Repos: len(entries)})
	b.SetGroups(c.groups.Resolve(b.Repos()))
	b.SetHidden(c.groups.Hidden(b.Repos(), c.showForks()))
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
