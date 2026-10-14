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
	return l.repos[owner], l.err
}

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
