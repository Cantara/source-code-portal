package integ

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
	tnoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/memory"
	"github.com/cantara/source-code-portal/internal/store/sqlite"
)

type lister struct {
	mu    sync.Mutex
	repos []scm.Repository
}

func (l *lister) set(r []scm.Repository) { l.mu.Lock(); l.repos = r; l.mu.Unlock() }
func (l *lister) ListRepositories(_ context.Context, _ scm.SourceID, cursor string, _ scm.Validators) (scm.Result[scm.Page[scm.Repository]], error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return scm.Result[scm.Page[scm.Repository]]{Value: scm.Page[scm.Repository]{Items: slices.Clone(l.repos)}}, nil
}

type prober struct{}

func (prober) ProbeHeads(_ context.Context, rs []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error) {
	out := map[scm.RepoID]scm.RepoStats{}
	for _, r := range rs {
		out[r.ID] = scm.RepoStats{HeadSHA: fmt.Sprintf("sha%d", r.ID), HeadTreeOID: "t", Stars: 5}
	}
	return out, nil
}

func repo(id int, owner, name string) scm.Repository {
	return scm.Repository{Ref: scm.RepoRef{Source: "github", ID: scm.RepoID(id), Owner: owner, Name: name}, DefaultBranch: "main", Visibility: scm.VisPublic, Topics: []string{"iam"}}
}

func build(t *testing.T, st catalog.CatalogueStore, l catalog.RepositoryLister) (*catalog.Service, *readmodel.Publisher, catalog.Slots) {
	cfg := catalog.DefaultConfig()
	cfg.Groups = []catalog.GroupConfig{{ID: "widget", DisplayName: "Widget", DefaultRepo: "Widget-STS", Match: []catalog.Matcher{{Kind: catalog.KindRegexp, Value: "Widget-.*"}}}}
	reg := &readmodel.Registry{}
	slots := catalog.RegisterSlots(reg)
	pub := readmodel.NewPublisher(reg, nil, []readmodel.Validator{slots.Validator()}, readmodel.Deps{})
	svc, err := catalog.New(catalog.Deps{Config: cfg, Sources: []catalog.SourceDeps{{ID: "github", Policy: catalog.DefaultDiscoveryPolicy()}}, Lister: l, Prober: prober{}, Store: st,
		Publisher: pub, Slots: slots, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Meter: noop.NewMeterProvider().Meter("t"), Tracer: tnoop.NewTracerProvider().Tracer("t")})
	if err != nil {
		t.Fatal(err)
	}
	return svc, pub, slots
}

func run(t *testing.T, name string, st catalog.CatalogueStore, full store.Store) {
	ctx := context.Background()
	l := &lister{}
	l.set([]scm.Repository{repo(1, "Acme", "Widget-STS"), repo(2, "Acme", "Widget-Admin"), repo(3, "Acme", "ACS")})
	svc, pub, slots := build(t, st, l)
	specs := map[string]job.Spec{}
	for _, sp := range svc.Jobs() {
		if err := sp.Validate(); err != nil {
			t.Fatal(err)
		}
		specs[sp.Kind] = sp
	}
	rk := job.NewKey(catalog.JobReconcile, "_")
	if _, err := specs[catalog.JobReconcile].Run(ctx, rk); err != nil {
		t.Fatal(err)
	}
	if _, err := specs[catalog.JobHeads].Run(ctx, job.NewKey(catalog.JobHeads, "_")); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.LoadCatalogue(ctx)
	if len(rows) != 3 {
		t.Fatalf("%s: %d rows persisted", name, len(rows))
	}
	n := 0
	st.LoadItems(ctx, func(it store.Item) error { n++; return nil })
	if n != 3 {
		t.Fatalf("%s: %d stats items persisted", name, n)
	}
	// a rename (same id, new name) keeps the item through the real store's rename-by-id
	l.set([]scm.Repository{repo(1, "Acme", "Widget-STS2"), repo(2, "Acme", "Widget-Admin"), repo(3, "Acme", "ACS")})
	time.Sleep(time.Millisecond)
	svc.React(ctx, scm.Event{Kind: scm.EventInstallationRepos, Source: "github", Action: "resync"}, job.EnqueueFunc(func(job.Key, job.Priority, time.Time, bool) error { return nil }))
	if _, err := specs[catalog.JobReconcile].Run(ctx, rk); err != nil {
		t.Fatal(err)
	}
	n = 0
	st.LoadItems(ctx, func(it store.Item) error { n++; return nil })
	if n != 3 {
		t.Fatalf("%s: rename lost stats items: %d", name, n)
	}
	// warm start of a second service from the same store
	svc2, pub2, slots2 := build(t, st, &lister{})
	fn, info, err := svc2.Warm(ctx)
	if err != nil || info.Repos != 3 || info.Skipped != 0 || info.StatsItems != 3 {
		t.Fatalf("%s: warm %+v %v", name, info, err)
	}
	if _, err := pub2.Apply(fn); err != nil {
		t.Fatal(err)
	}
	v, ok := readmodel.Get(pub2.Current(), "widget-sts2", slots2.Repo)
	if !ok || v.V.Ref.Name != "Widget-STS2" || v.V.Ref.Owner != "Acme" {
		t.Fatalf("%s: warm lost display case: %+v", name, v.V.Ref)
	}
	m, _ := readmodel.GetGlobal(pub2.Current(), slots2.Membership)
	if got := m.ReposOf("widget"); !slices.Equal(got, []scm.Slug{"widget-admin", "widget-sts2"}) {
		t.Fatalf("%s: warm membership %v", name, got)
	}
	_ = pub
	_ = slots
}

func TestMemoryStore(t *testing.T) {
	st := memory.New(memory.Options{})
	run(t, "memory", st, st)
}

func TestSQLiteStore(t *testing.T) {
	ctx := context.Background()
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "scp.db")
	st, _, err := sqlite.Open(ctx, cfg, sqlite.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(ctx)
	run(t, "sqlite", st, st)
}

var _ catalog.CatalogueStore = (*sqlite.Store)(nil)
var _ catalog.CatalogueStore = (*memory.Store)(nil)
