package integration_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
	tnoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	jobpkg "github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/cargo"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/golang"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/gradle"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/maven"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/npm"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/python"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// fakeGit is the fake GitHub: trees and blobs of in-memory repositories, with call counters.
type fakeGit struct {
	trees     map[string]scm.Tree
	blobs     map[string][]byte
	treeCalls atomic.Int32
	blobCalls atomic.Int32
}

func (f *fakeGit) ListTree(_ context.Context, r scm.RepoRef, _ string, _ scm.TreeOptions) (scm.Result[scm.Tree], error) {
	f.treeCalls.Add(1)
	return scm.Result[scm.Tree]{Value: f.trees[r.Name]}, nil
}

func (f *fakeGit) ReadBlob(_ context.Context, _ scm.RepoRef, sha string, _ int64) (scm.Blob, error) {
	f.blobCalls.Add(1)
	b, ok := f.blobs[sha]
	if !ok {
		return scm.Blob{}, scm.ErrNotFound
	}
	return scm.Blob{SHA: sha, Data: b}, nil
}

type fakeStore struct {
	mu    sync.Mutex
	cache map[string][]byte
}

func (s *fakeStore) PutItem(context.Context, store.Item) error                         { return nil }
func (s *fakeStore) Revalidated(context.Context, store.ItemKey, time.Time) error       { return nil }
func (s *fakeStore) RecordFailure(context.Context, store.ItemKey, store.Failure) error { return nil }
func (s *fakeStore) GetCache(_ context.Context, ns, k string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.cache[ns+"|"+k]
	return b, ok, nil
}
func (s *fakeStore) PutCache(_ context.Context, ns, k string, d []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[ns+"|"+k] = d
	return nil
}

func gitSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// register wires the six ecosystems exactly as cmd/scp does (fixed order).
func register(t testing.TB) *manifests.Registry {
	t.Helper()
	reg := manifests.NewRegistry()
	for _, f := range []func(*manifests.Registry) error{
		func(r *manifests.Registry) error { return golang.Register(r) },
		func(r *manifests.Registry) error { return npm.Register(r) },
		func(r *manifests.Registry) error { return python.Register(r) },
		func(r *manifests.Registry) error { return maven.Register(r) },
		func(r *manifests.Registry) error { return gradle.Register(r) },
		func(r *manifests.Registry) error { return cargo.Register(r) },
	} {
		if err := f(reg); err != nil {
			t.Fatal(err)
		}
	}
	reg.Freeze()
	return reg
}

type env struct {
	svc   *manifests.Service
	git   *fakeGit
	pub   *readmodel.Publisher
	slots manifests.Slots
}

func newEnv(t testing.TB, repos map[string]map[string][]byte) *env {
	t.Helper()
	rr := &readmodel.Registry{}
	repoSlot := readmodel.Register[freshness.Value[scm.Repository]](rr, "catalog.repo", readmodel.Existence())
	statSlot := readmodel.Register[freshness.Value[scm.RepoStats]](rr, "catalog.stats")
	memSlot := readmodel.RegisterGlobal[scm.Membership](rr, "catalog.membership")
	factSlot, graphSlot := manifests.RegisterSlots(rr)
	pub := readmodel.NewPublisher(rr, nil, nil, readmodel.Deps{})
	git := &fakeGit{trees: map[string]scm.Tree{}, blobs: map[string][]byte{}}
	_, err := pub.Apply(func(b *readmodel.Builder) error {
		for name, files := range repos {
			var entries []scm.TreeEntry
			for p, data := range files {
				sha := gitSHA(data)
				git.blobs[sha] = data
				entries = append(entries, scm.TreeEntry{Path: p, SHA: sha, Size: int64(len(data))})
			}
			slices.SortFunc(entries, func(a, b scm.TreeEntry) int { return strings.Compare(a.Path, b.Path) })
			git.trees[name] = scm.NewTree("tree-"+name, entries, false)
			ref := scm.RepoRef{Source: "github", Owner: "acme", Name: name}
			id := readmodel.EntryID(name)
			if err := readmodel.Put(b, id, repoSlot, freshness.Value[scm.Repository]{V: scm.Repository{Ref: ref, Slug: scm.Slug(name), DefaultBranch: "main", Visibility: scm.VisPublic, HTMLURL: "https://github.com/acme/" + name}}); err != nil {
				return err
			}
			if err := readmodel.Put(b, id, statSlot, freshness.Value[scm.RepoStats]{V: scm.RepoStats{HeadSHA: "c-" + name}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slots := manifests.Slots{Repos: repoSlot, Stats: statSlot, Facts: factSlot, Graph: graphSlot, Membership: memSlot}
	svc, err := manifests.New(manifests.DefaultConfig(), manifests.Deps{Registry: register(t), Trees: git, Blobs: git, Store: &fakeStore{cache: map[string][]byte{}}, Publisher: pub, Slots: slots,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Meter: noop.NewMeterProvider().Meter("t"), Tracer: tnoop.NewTracerProvider().Tracer("t")})
	if err != nil {
		t.Fatal(err)
	}
	return &env{svc: svc, git: git, pub: pub, slots: slots}
}

// cases loads every golden input tree of the six packages as one repository each ("<eco>-<case>").
func cases(t testing.TB) map[string]map[string][]byte {
	t.Helper()
	out := map[string]map[string][]byte{}
	for _, eco := range []string{"golang", "npm", "python", "maven", "gradle", "cargo"} {
		dirs, _ := filepath.Glob(filepath.Join("..", eco, "testdata", "*", "input"))
		for _, d := range dirs {
			files := map[string][]byte{}
			_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(d, p)
				b, err := os.ReadFile(p)
				if err == nil {
					files[filepath.ToSlash(rel)] = b
				}
				return nil
			})
			if len(files) > 0 {
				out[eco+"-"+filepath.Base(filepath.Dir(d))] = files
			}
		}
	}
	return out
}

func codes(ds []manifests.Diagnostic) []string {
	var out []string
	for _, d := range ds {
		if strings.HasPrefix(d.Code, "tree.") {
			continue
		}
		out = append(out, d.Code+"@"+d.Loc.Path)
	}
	slices.Sort(out)
	return out
}

// TestRealRunnerMatchesRunTree: for every golden input tree, the units published by the real manifests.refresh job equal the units of the pure
// RunTree harness (identity, directory, role, dependency and locked counts, diagnostics), and no assembler mutated its input.
func TestRealRunnerMatchesRunTree(t *testing.T) {
	repos := cases(t)
	if len(repos) < 20 {
		t.Fatalf("only %d golden trees found", len(repos))
	}
	e := newEnv(t, repos)
	reg := register(t)
	cfg := parsertest.TreeConfig()
	spec := e.svc.Jobs()[0]
	for name, files := range repos {
		t.Run(name, func(t *testing.T) {
			if _, err := spec.Run(context.Background(), job(name)); err != nil {
				t.Fatalf("refresh: %v", err)
			}
			v, ok := readmodel.Get(e.pub.Current(), readmodel.EntryID(name), e.slots.Facts)
			if !ok {
				t.Fatal("no facts published")
			}
			want := manifests.RunTree(context.Background(), reg, cfg, files)
			got := v.V
			if len(got.Units) != len(want.Units) {
				t.Fatalf("units: runner %d, RunTree %d", len(got.Units), len(want.Units))
			}
			for i := range got.Units {
				g, w := got.Units[i], want.Units[i]
				if g.ID != w.ID || g.Dir != w.Dir || g.Ecosystem != w.Ecosystem || g.Role != w.Role || len(g.Dependencies) != len(w.Dependencies) || g.LockedCount != w.LockedCount || len(g.Provides) != len(w.Provides) {
					t.Errorf("unit %d differs:\n runner  %s %s %v deps=%d locked=%d provides=%d\n RunTree %s %s %v deps=%d locked=%d provides=%d", i,
						g.ID, g.Dir, g.Role, len(g.Dependencies), g.LockedCount, len(g.Provides), w.ID, w.Dir, w.Role, len(w.Dependencies), w.LockedCount, len(w.Provides))
				}
				if a, b := codes(g.Diagnostics), codes(w.Diagnostics); !slices.Equal(a, b) {
					t.Errorf("unit %s diagnostics differ:\n runner  %v\n RunTree %v", g.ID, a, b)
				}
			}
			for _, d := range got.Diagnostics {
				if d.Code == "assemble.mutated-input" || d.Code == manifests.DiagParsePanic || d.Code == "assemble.failed" {
					t.Errorf("runner diagnostic %s: %s", d.Code, d.Message)
				}
			}
		})
	}
}

// TestUnchangedTreeCostsNothing: with the HEAD tree id equal to the stored facts' tree, a second refresh makes no tree and no blob call.
func TestUnchangedTreeCostsNothing(t *testing.T) {
	repos := map[string]map[string][]byte{"app": {"go.mod": []byte("module example.com/app\n\ngo 1.22\n\nrequire golang.org/x/text v0.14.0\n"), "go.sum": []byte("")}}
	e := newEnv(t, repos)
	spec := e.svc.Jobs()[0]
	if _, err := spec.Run(context.Background(), job("app")); err != nil {
		t.Fatal(err)
	}
	if e.git.treeCalls.Load() != 1 || e.git.blobCalls.Load() == 0 {
		t.Fatalf("first run: tree=%d blob=%d", e.git.treeCalls.Load(), e.git.blobCalls.Load())
	}
	v, _ := readmodel.Get(e.pub.Current(), "app", e.slots.Facts)
	if len(v.V.Units) != 1 || v.V.Units[0].Ecosystem != manifests.Golang {
		t.Fatalf("%+v", v.V.Units)
	}
	sv, _ := readmodel.Get(e.pub.Current(), "app", e.slots.Stats)
	sv.V.HeadTreeOID = v.V.TreeSHA
	if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "app", e.slots.Stats, sv) }); err != nil {
		t.Fatal(err)
	}
	t0, b0 := e.git.treeCalls.Load(), e.git.blobCalls.Load()
	out, err := spec.Run(context.Background(), job("app"))
	if err != nil || !out.Unchanged || e.git.treeCalls.Load() != t0 || e.git.blobCalls.Load() != b0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func job(name string) jobpkg.Key { return jobpkg.NewKey(manifests.KindRefresh, "github", "acme", name) }
