package catalog

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

type lister struct {
	repos map[string][]snapshot.Repo
	err   error
	errs  map[string]error  // by owner, before err
	files map[string]string // path -> content, for the descriptor job
}

func (l *lister) RefreshInstallations(context.Context) error { return nil }

func (l *lister) Tree(_ context.Context, _, _, ref string, _ bool) ([]github.TreeEntry, error) {
	var out []github.TreeEntry
	dirs := map[string]bool{}
	prefix := ""
	if ref != "HEAD" {
		prefix = ref + "/" // the fake uses the directory name as its tree SHA
	}
	for p, c := range l.files {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		if dir, _, isDir := strings.Cut(rest, "/"); isDir {
			if !dirs[dir] {
				dirs[dir] = true
				out = append(out, github.TreeEntry{Path: dir, Type: "tree", SHA: prefix + dir})
			}
			continue
		}
		out = append(out, github.TreeEntry{Path: rest, Type: "blob", SHA: "sha:" + c, Size: int64(len(c))})
	}
	return out, nil
}

func (l *lister) Blob(_ context.Context, _, _, sha string) ([]byte, error) {
	return []byte(strings.TrimPrefix(sha, "sha:")), nil
}

func (l *lister) ListRepos(_ context.Context, owner string) ([]snapshot.Repo, error) {
	if err := l.errs[owner]; err != nil {
		return nil, err
	}
	return l.repos[owner], l.err
}

// appLister reads through a GitHub App: its listing is GitHub's word on what the installation was given.
type appLister struct{ *lister }

func (appLister) App() bool { return true }

func newCatalog(t *testing.T, l GitHub) *Catalog {
	t.Helper()
	c, err := New(config.Catalog{ReconcileInterval: 5 * time.Minute, Groups: []config.Group{
		{ID: "whydah", Name: "Whydah", DefaultRepo: "Whydah-Core", Match: []config.Match{{Prefix: "whydah-"}}},
		{ID: "other", Name: "Other", DefaultRepo: "x", Match: []config.Match{{CatchAll: true}}},
	}}, []config.Source{{ID: "gh", Orgs: []string{"acme", "beta"}}}, map[string]GitHub{"gh": l}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// runAndPublish runs the listing job once with prev and publishes the result as sync would after a success.
func runAndPublish(t *testing.T, c *Catalog, p *snapshot.Publisher, prev store.Item) (store.Item, sync.Result, error) {
	t.Helper()
	spec := c.Spec()
	res, err := spec.Run(t.Context(), sync.Job{Key: Kind + "/gh", Source: "gh", Prev: prev})
	if err != nil {
		return prev, res, err
	}
	it := prev
	it.Key, it.Kind, it.CheckedAt = Kind+"/gh", Kind, time.Now()
	if !res.Unchanged {
		it.State, it.Payload = snapshot.StateOK, res.Payload
	}
	p.Update(func(b *snapshot.Builder) {
		spec.Publish(b, it, snapshot.Freshness{State: it.State, CheckedAt: it.CheckedAt})
	})
	p.Flush()
	return it, res, nil
}

func TestReconcilePublishes(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{
		"acme": {pub(1, "Whydah-Core"), pub(2, "whydah-sso")},
		"beta": {func() snapshot.Repo { r := pub(3, "tool"); r.Owner = "beta"; return r }()},
	}}
	c := newCatalog(t, l)
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, res, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil {
		t.Fatal(err)
	}
	s := p.Current()
	if !s.Catalogued || len(s.Repos) != 3 || len(res.Catalogue) != 3 {
		t.Fatalf("snapshot: catalogued %v, repos %d, catalogue %d", s.Catalogued, len(s.Repos), len(res.Catalogue))
	}
	w, _ := s.Group("whydah")
	if len(w.Members) != 2 || w.DefaultRepo != "acme/whydah-core" {
		t.Errorf("whydah = %+v", w)
	}
	if o, _ := s.Group("other"); len(o.Members) != 3 || !o.DefaultMissing {
		t.Errorf("other = %+v", o)
	}

	// The same listing again is unchanged.
	if _, res, err := runAndPublish(t, c, p, it); err != nil || !res.Unchanged {
		t.Errorf("repeat: unchanged %v, err %v", res.Unchanged, err)
	}

	// A rename publishes an alias.
	l.repos["acme"][1].Name = "whydah-login"
	if _, _, err := runAndPublish(t, c, p, it); err != nil {
		t.Fatal(err)
	}
	if e, moved := p.Current().Repo("acme/whydah-sso"); !moved || e == nil || e.Repo.Name != "whydah-login" {
		t.Errorf("old address: moved %v, entry %v", moved, e)
	}

	// An empty listing for a source with repositories fails and keeps the catalogue.
	empty := &lister{repos: map[string][]snapshot.Repo{}}
	c2 := newCatalog(t, empty)
	if _, _, err := runAndPublish(t, c2, p, it); !errors.Is(err, errEmpty) {
		t.Errorf("empty listing: err = %v", err)
	}
}

func TestReconcileListingErrorFails(t *testing.T) {
	c := newCatalog(t, &lister{err: errors.New("boom")})
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	if _, _, err := runAndPublish(t, c, p, store.Item{}); err == nil {
		t.Error("a failed page did not fail the run")
	}
	if p.Current().Catalogued {
		t.Error("catalogued without a listing")
	}
}

func TestPushedAtNotifies(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a"), pub(2, "b")}}}
	var pushed []snapshot.RepoKey
	c, err := New(config.Catalog{ReconcileInterval: 5 * time.Minute, Groups: []config.Group{
		{ID: "all", Name: "All", DefaultRepo: "a", Match: []config.Match{{CatchAll: true}}},
	}}, []config.Source{{ID: "gh", Orgs: []string{"acme"}}}, map[string]GitHub{"gh": l},
		func(ev sync.Event, k snapshot.RepoKey) {
			if ev == sync.EventPush {
				pushed = append(pushed, k)
			}
		}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, _, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil || len(pushed) != 0 {
		t.Fatalf("first listing: pushed %v, err %v (new repositories are backfilled, not notified)", pushed, err)
	}
	if _, _, err := runAndPublish(t, c, p, it); err != nil || len(pushed) != 0 {
		t.Fatalf("unchanged sweep notified %v", pushed)
	}
	l.repos["acme"][1].PushedAt = time.Now()
	if _, _, err := runAndPublish(t, c, p, it); err != nil || len(pushed) != 1 || pushed[0] != "acme/b" {
		t.Errorf("after a push to b: notified %v, err %v", pushed, err)
	}
}

// TestOpenCountNotifies: a changed count of open issues and pull requests in the listing asks the jobs that read them to run, the
// safety net for a webhook that never came.
func TestOpenCountNotifies(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a"), pub(2, "b")}}}
	var opened []snapshot.RepoKey
	c, err := New(config.Catalog{ReconcileInterval: 5 * time.Minute, Groups: []config.Group{
		{ID: "all", Name: "All", DefaultRepo: "a", Match: []config.Match{{CatchAll: true}}},
	}}, []config.Source{{ID: "gh", Orgs: []string{"acme"}}}, map[string]GitHub{"gh": l},
		func(ev sync.Event, k snapshot.RepoKey) {
			if ev == sync.EventOpen {
				opened = append(opened, k)
			}
		}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, _, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil || len(opened) != 0 {
		t.Fatalf("first listing: %v, err %v", opened, err)
	}
	if it, _, err = runAndPublish(t, c, p, it); err != nil || len(opened) != 0 {
		t.Fatalf("unchanged sweep notified %v, err %v", opened, err)
	}
	l.repos["acme"][1].OpenIssues++
	if _, _, err := runAndPublish(t, c, p, it); err != nil || len(opened) != 1 || opened[0] != "acme/b" {
		t.Errorf("after an issue opened in b: notified %v, err %v", opened, err)
	}
}

// TestMissConfirmedSoon: a listing that misses a tracked repository (deleted, transferred) asks to run again within confirmAfter,
// and that listing removes it; a listing with no miss keeps the interval.
func TestMissConfirmedSoon(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a"), pub(2, "gone")}}}
	c := newCatalog(t, l)
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, res, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil || res.Again != 0 {
		t.Fatalf("first listing: again %v, err %v", res.Again, err)
	}
	l.repos["acme"] = l.repos["acme"][:1]
	it, res, err = runAndPublish(t, c, p, it)
	if err != nil || res.Again != confirmAfter || len(p.Current().Repos) != 2 {
		t.Fatalf("missed once: again %v, repos %d, err %v", res.Again, len(p.Current().Repos), err)
	}
	if _, res, err = runAndPublish(t, c, p, it); err != nil || res.Again != 0 || len(p.Current().Repos) != 1 {
		t.Errorf("confirmed: again %v, repos %d, err %v", res.Again, len(p.Current().Repos), err)
	}
}

func beta(id int64, name string) snapshot.Repo { r := pub(id, name); r.Owner = "beta"; return r }

// An App's empty answer is GitHub's word: nothing selected, or the App removed from the account. Its repositories leave (at the
// confirming listing, as any miss) instead of the last catalogue being kept forever, as a token's empty listing still is.
func TestAppSelectionEmptiesTheAccount(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a")}, "beta": {beta(2, "b")}}}
	c := newCatalog(t, appLister{l})
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, _, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil || len(p.Current().Repos) != 2 {
		t.Fatalf("first listing: %d repos, %v", len(p.Current().Repos), err)
	}
	l.repos = map[string][]snapshot.Repo{} // acme: nothing selected
	l.errs = map[string]error{"beta": &github.Error{Kind: github.Permanent, Code: "no_installation", Err: errors.New("not installed")}}
	for range 2 {
		if it, _, err = runAndPublish(t, c, p, it); err != nil {
			t.Fatalf("an App's empty listing failed: %v", err)
		}
	}
	if n := len(p.Current().Repos); n != 0 {
		t.Errorf("after the confirming listing: %d repos, want none", n)
	}
}

// One account that fails to list keeps its repositories; the others are still listed, and the listing runs again soon.
func TestOneAccountFailingKeepsItsRepositories(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a")}, "beta": {beta(2, "b")}}}
	c := newCatalog(t, l)
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, _, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil {
		t.Fatal(err)
	}
	l.repos["acme"] = append(l.repos["acme"], pub(3, "new"))
	l.errs = map[string]error{"beta": &github.Error{Kind: github.Transient, Code: "transient", Err: errors.New("502")}}
	for _, want := range []time.Duration{retryAccount, 2 * retryAccount, 4 * retryAccount, 5 * time.Minute} { // backs off to the interval
		var res sync.Result
		if it, res, err = runAndPublish(t, c, p, it); err != nil || res.Again != want {
			t.Fatalf("one account failing: again %v, err %v; want again %v", res.Again, err, want)
		}
	}
	s := p.Current()
	if e, _ := s.Repo("acme/new"); e == nil || len(s.Repos) != 3 {
		t.Errorf("repos = %d (acme/new %v), want acme's new one and beta's kept", len(s.Repos), e)
	}
	if e, _ := s.Repo("beta/b"); e == nil {
		t.Error("the failing account's repository left")
	}
	l.errs["acme"] = l.errs["beta"]
	if _, _, err := runAndPublish(t, c, p, it); err == nil {
		t.Error("every account failing did not fail the run")
	}
}

// A rate limit or a refused credential is the source's: the run fails, so the engine pauses the source until the reset.
func TestSourceWideFailureFailsTheRun(t *testing.T) {
	for _, kind := range []github.Kind{github.RateLimited, github.Auth} {
		l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a")}, "beta": {beta(2, "b")}}}
		c := newCatalog(t, l)
		p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
		it, _, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(time.Hour)
		l.errs = map[string]error{"beta": &github.Error{Kind: kind, RetryAt: at, Code: "limited", Err: errors.New("403")}}
		_, _, err = runAndPublish(t, c, p, it)
		var ge *github.Error
		if !errors.As(err, &ge) || ge.Kind != kind || !ge.RetryAt.Equal(at) {
			t.Errorf("kind %d: err %v, want the GitHub error passed on", kind, err)
		}
	}
}

// An account GitHub does not have (deleted, renamed) lists nothing: its repositories leave at the confirming listing.
func TestAccountGoneLeaves(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a")}, "beta": {beta(2, "b")}}}
	c := newCatalog(t, l)
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, _, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil {
		t.Fatal(err)
	}
	l.errs = map[string]error{"beta": &github.Error{Kind: github.NotFound, Code: "not_found", Err: errors.New("404")}}
	for range missesToRemove {
		var res sync.Result
		if it, res, err = runAndPublish(t, c, p, it); err != nil || res.Again == retryAccount {
			t.Fatalf("gone account: again %v, err %v; want no account retry", res.Again, err)
		}
	}
	if e, _ := p.Current().Repo("beta/b"); e != nil {
		t.Error("the gone account's repository stayed")
	}
}

// An account's failure keeps its entries as they were: a repository the last listing missed is not seen again, and leaves at the
// next listing that misses it.
func TestFailureKeepsTheMisses(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a")}, "beta": {beta(2, "b"), beta(3, "deleted")}}}
	c := newCatalog(t, l)
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	it, _, err := runAndPublish(t, c, p, store.Item{State: snapshot.StateNever})
	if err != nil {
		t.Fatal(err)
	}
	l.repos["beta"] = []snapshot.Repo{beta(2, "b")} // deleted: one miss
	if it, _, err = runAndPublish(t, c, p, it); err != nil {
		t.Fatal(err)
	}
	l.errs = map[string]error{"beta": &github.Error{Kind: github.Transient, Code: "transient", Err: errors.New("502")}}
	for range 3 {
		if it, _, err = runAndPublish(t, c, p, it); err != nil {
			t.Fatal(err)
		}
	}
	if e, _ := p.Current().Repo("beta/deleted"); e == nil {
		t.Fatal("the failing account's missed repository left while the account failed")
	}
	l.errs = nil
	if _, _, err = runAndPublish(t, c, p, it); err != nil {
		t.Fatal(err)
	}
	if e, _ := p.Current().Repo("beta/deleted"); e != nil {
		t.Error("the missed repository was not removed at the next listing that missed it: its count was reset")
	}
	if e, _ := p.Current().Repo("beta/b"); e == nil {
		t.Error("beta/b left")
	}
}
