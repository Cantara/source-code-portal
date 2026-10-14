package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/docs"
	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/media"
	"github.com/cantara/source-code-portal/internal/pages"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/reactor"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// Compile-time proofs of every cross-feature wiring this design relies on (architecture 5.5).
var (
	_ docs.ImageSigner = (*media.Service)(nil)
	_ docs.ItemStore   = store.Store(nil)
	_ httpapi.Mounter  = (*docs.Service)(nil)
	_ httpapi.Mounter  = (*pages.Service)(nil)
	_ pages.Renderer   = pageBridge{}
	_ reactor.Func     = (*docs.Service)(nil).React
	_ http.Handler     = (*media.Service)(nil).External()
	_ media.HTTPDoer   = (*http.Client)(nil)
)

// pageBridge is cmd/scp/internal/bridge.PageRenderer: it converts docs' result type to pages' type.
type pageBridge struct{ d *docs.Service }

func (b pageBridge) RenderPage(ctx context.Context, src []byte) (pages.RenderedPage, error) {
	r, err := b.d.RenderPage(ctx, src)
	if err != nil {
		return pages.RenderedPage{}, err
	}
	out := pages.RenderedPage{HTML: r.HTML, Title: r.Title, Description: r.Description, Order: r.Order, Hidden: r.Hidden, Tags: r.Tags}
	for _, t := range r.TOC {
		out.TOC = append(out.TOC, pages.TOCEntry{Depth: t.Depth, Text: t.Text, ID: t.ID})
	}
	for _, p := range r.Problems {
		out.Problems = append(out.Problems, pages.Problem{Code: p.Code, Message: p.Message})
	}
	return out, nil
}

type nilFiles struct{}

func (nilFiles) Readme(context.Context, scm.RepoRef, string, scm.Validators) (scm.Result[scm.File], error) {
	return scm.Result[scm.File]{}, scm.ErrNotFound
}
func (nilFiles) ReadFile(context.Context, scm.RepoRef, string, string, scm.Validators) (scm.Result[scm.File], error) {
	return scm.Result[scm.File]{}, scm.ErrNotFound
}
func (nilFiles) ListTree(context.Context, scm.RepoRef, string, scm.TreeOptions) (scm.Result[scm.Tree], error) {
	return scm.Result[scm.Tree]{}, scm.ErrNotFound
}
func (nilFiles) RenderedFile(context.Context, scm.RepoRef, string, string, scm.Validators) (scm.Result[string], error) {
	return scm.Result[string]{}, scm.ErrNotFound
}
func (nilFiles) ReadRepoImage(context.Context, scm.RepoRef, string, string, int64) (scm.File, error) {
	return scm.File{}, scm.ErrNotFound
}

type enq struct{ keys []job.Key }

func (e *enq) Enqueue(k job.Key, _ job.Priority, _ time.Time, _ bool) error {
	e.keys = append(e.keys, k)
	return nil
}

func TestComposition(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := &readmodel.Registry{}
	repos := readmodel.Register[freshness.Value[scm.Repository]](reg, "catalog.repo", readmodel.Existence())
	stats := readmodel.Register[freshness.Value[scm.RepoStats]](reg, "catalog.stats")
	aliases := readmodel.RegisterGlobal[map[scm.Slug]scm.Slug](reg, "catalog.aliases")
	docsSlot := readmodel.Register[docs.RepoDocs](reg, docs.SlotName)
	pagesG := readmodel.RegisterGlobal[pages.Set](reg, pages.GlobalName)
	reg.Freeze()
	pub := readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{})

	m, err := media.New(media.Deps{Cfg: media.DefaultConfig(), Images: nilFiles{}, Client: http.DefaultClient, Pub: pub, Repos: repos, Authz: authz.PublicOnly{}, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	cfg := docs.DefaultConfig()
	d, err := docs.New(docs.Deps{Cfg: cfg, Files: nilFiles{}, Trees: nilFiles{}, Rendered: nilFiles{}, Signer: m, Store: store.Store(nil), Pub: pub,
		Repos: repos, Stats: stats, Aliases: aliases, DocsSlot: docsSlot, Authz: authz.PublicOnly{}, PublicURL: "https://scp.example.org", Log: log})
	if err == nil {
		t.Fatal("a nil Store must be refused")
	}
	d, err = docs.New(docs.Deps{Cfg: cfg, Files: nilFiles{}, Trees: nilFiles{}, Rendered: nilFiles{}, Signer: m, Store: noStore{}, Pub: pub,
		Repos: repos, Stats: stats, Aliases: aliases, DocsSlot: docsSlot, Authz: authz.PublicOnly{}, PublicURL: "https://scp.example.org", Log: log})
	if err != nil {
		t.Fatal(err)
	}
	if d.Pipeline() == "" || len(d.Jobs()) != 1 || d.Jobs()[0].Kind != "docs.refresh" || d.Jobs()[0].Policy.TTL != time.Hour {
		t.Fatal(d.Pipeline(), d.Jobs())
	}
	if _, err := pages.New(pages.Deps{Cfg: pages.DefaultConfig(), Renderer: pageBridge{d}, Pub: pub, Global: pagesG, Log: log}); err != nil {
		t.Fatal(err)
	}
	e := &enq{}
	if err := d.React(context.Background(), scm.Event{Kind: scm.EventPing}, e); err != nil || len(e.keys) != 0 {
		t.Fatal(err, e.keys)
	}
	_ = secret.Secret{}
}

type noStore struct{}

func (noStore) PutItem(context.Context, store.Item) error                         { return nil }
func (noStore) Revalidated(context.Context, store.ItemKey, time.Time) error       { return nil }
func (noStore) RecordFailure(context.Context, store.ItemKey, store.Failure) error { return nil }
