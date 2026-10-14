package readmodel_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	rm "github.com/cantara/source-code-portal/internal/readmodel"
)

type repo struct{ Name string }
type commits struct{ Latest []string }
type readme struct{ HTML *string }

type kit struct {
	reg     *rm.Registry
	repo    rm.Slot[repo]
	commits rm.Slot[*commits]
	readme  rm.Slot[readme]
	latest  rm.Global[[]string]
}

func newKit() kit {
	r := &rm.Registry{}
	return kit{reg: r,
		repo:    rm.Register[repo](r, "catalog.repo", rm.Existence()),
		commits: rm.Register[*commits](r, "commits.view"),
		readme:  rm.Register[readme](r, "docs.readme"),
		latest:  rm.RegisterGlobal[[]string](r, "commits.latest")}
}

func (k kit) projector() rm.Projector {
	return func(b *rm.Builder) error {
		v := b.View()
		var out []string
		for _, id := range v.Repos() {
			if c, ok := rm.Get(v, id, k.commits); ok && len(c.Latest) > 0 {
				out = append(out, string(id)+":"+c.Latest[0])
			}
		}
		return rm.PutGlobal(b, k.latest, out)
	}
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestImmutabilitySharingAndRev(t *testing.T) {
	k := newKit()
	p := rm.NewPublisher(k.reg, []rm.Projector{k.projector()}, nil, rm.Deps{})
	if p.Current().Version() != 0 || len(p.Current().Repos()) != 0 {
		t.Fatal("initial snapshot must be empty version 0")
	}
	s1, err := p.Apply(func(b *rm.Builder) error {
		must(t, rm.Put(b, "a", k.repo, repo{"a"}))
		must(t, rm.Put(b, "a", k.commits, &commits{[]string{"c1"}}))
		html := "<p>x</p>"
		must(t, rm.Put(b, "a", k.readme, readme{&html}))
		must(t, rm.Put(b, "b", k.repo, repo{"b"}))
		return rm.Put(b, "b", k.commits, &commits{[]string{"c9"}})
	})
	must(t, err)
	s2, err := p.Apply(func(b *rm.Builder) error { return rm.Put(b, "a", k.commits, &commits{[]string{"c2"}}) })
	must(t, err)
	c1, _ := rm.Get(s1, "a", k.commits)
	if c1.Latest[0] != "c1" {
		t.Fatal("old snapshot mutated")
	}
	r1, _ := rm.Get(s1, "a", k.readme)
	r2, _ := rm.Get(s2, "a", k.readme)
	if r1.HTML != r2.HTML {
		t.Fatal("untouched slot must be shared by pointer")
	}
	if &s1.Repos()[0] != &s2.Repos()[0] {
		t.Fatal("names slice must be shared while membership is unchanged")
	}
	if s1.Rev("a") != 1 || s2.Rev("a") != 2 || s2.Rev("b") != 1 || s2.Rev("zz") != 0 {
		t.Fatalf("rev a=%d b=%d", s2.Rev("a"), s2.Rev("b"))
	}
	g, ok := rm.GetGlobal(s2, k.latest)
	if !ok || !slices.Equal(g, []string{"a:c2", "b:c9"}) || s2.Version() != 2 {
		t.Fatalf("%v %v", g, s2.Version())
	}
}

func TestZombiePutIsRejectedAndIsGone(t *testing.T) {
	k := newKit()
	p := rm.NewPublisher(k.reg, nil, nil, rm.Deps{})
	_, err := p.Apply(func(b *rm.Builder) error { return rm.Put(b, "x", k.repo, repo{"x"}) })
	must(t, err)
	_, err = p.Apply(func(b *rm.Builder) error { b.Remove("x"); return nil })
	must(t, err)
	before := p.Current()
	_, err = p.Apply(func(b *rm.Builder) error { return rm.Put(b, "x", k.commits, &commits{}) }) // late job
	if !errors.Is(err, rm.ErrUnknownRepo) || !isGone(err) {
		t.Fatalf("want ErrUnknownRepo and Gone, got %v", err)
	}
	if p.Current() != before || p.Current().Has("x") {
		t.Fatal("zombie entry published")
	}
	// remove then re-create inside one Apply is allowed (existence slot first)
	_, err = p.Apply(func(b *rm.Builder) error {
		must(t, rm.Put(b, "y", k.repo, repo{"y"}))
		b.Remove("y")
		if err := rm.Put(b, "y", k.commits, &commits{}); !errors.Is(err, rm.ErrUnknownRepo) {
			t.Fatal("put after remove in the same builder must fail")
		}
		return rm.Put(b, "y", k.repo, repo{"y2"})
	})
	must(t, err)
	if v, _ := rm.Get(p.Current(), "y", k.repo); v.Name != "y2" {
		t.Fatal("recreate")
	}
}

func TestValidatorFailsClosedAndIsPermanent(t *testing.T) {
	k := newKit()
	bad := func(s *rm.Snapshot) error {
		if s.Has("bad") {
			return fmt.Errorf("bad is forbidden")
		}
		return nil
	}
	p := rm.NewPublisher(k.reg, nil, []rm.Validator{bad}, rm.Deps{})
	s1, err := p.Apply(func(b *rm.Builder) error { return rm.Put(b, "ok", k.repo, repo{}) })
	must(t, err)
	s2, err := p.Apply(func(b *rm.Builder) error { return rm.Put(b, "bad", k.repo, repo{}) })
	if !errors.Is(err, rm.ErrInvalidSnapshot) || !isPermanent(err) || s2 != s1 || p.Current() != s1 {
		t.Fatalf("err=%v", err)
	}
	st := p.Status()
	if st.Rejected != 1 || st.Published != 1 || st.LastRejected == nil {
		t.Fatalf("%+v", st)
	}
	// the version did not advance and the next good publish is version 2
	s3, err := p.Apply(func(b *rm.Builder) error { return rm.Put(b, "ok2", k.repo, repo{}) })
	must(t, err)
	if s3.Version() != 2 {
		t.Fatal(s3.Version())
	}
}

func TestRegistryMisuse(t *testing.T) {
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s: expected panic", name)
			}
		}()
		f()
	}
	mustPanic("no existence", func() { r := &rm.Registry{}; rm.Register[int](r, "a"); r.Freeze() })
	mustPanic("two existence", func() {
		r := &rm.Registry{}
		rm.Register[int](r, "a", rm.Existence())
		rm.Register[int](r, "b", rm.Existence())
	})
	mustPanic("dup", func() { r := &rm.Registry{}; rm.Register[int](r, "a"); rm.Register[int](r, "a") })
	mustPanic("after freeze", func() {
		r := &rm.Registry{}
		rm.Register[int](r, "a", rm.Existence())
		r.Freeze()
		rm.Register[int](r, "b")
	})
	mustPanic("foreign handle", func() {
		k1, k2 := newKit(), newKit()
		p := rm.NewPublisher(k1.reg, nil, nil, rm.Deps{})
		rm.Get(p.Current(), "a", k2.repo)
	})
}

// A producer that keeps mutating a value after Put is a bug the type system cannot catch; this documents what -race flags.
func TestConcurrentReadersDuringWrites(t *testing.T) {
	k := newKit()
	p := rm.NewPublisher(k.reg, []rm.Projector{k.projector()}, nil, rm.Deps{})
	_, err := p.Apply(func(b *rm.Builder) error {
		for i := 0; i < 50; i++ {
			must(t, rm.Put(b, rm.EntryID(fmt.Sprint("r", i)), k.repo, repo{}))
		}
		return nil
	})
	must(t, err)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for g := 0; g < 4; g++ {
		wg.Go(func() {
			var last uint64
			for {
				select {
				case <-stop:
					return
				default:
				}
				s := p.Current()
				if s.Version() < last {
					t.Error("version went backwards")
				}
				last = s.Version()
				for _, id := range s.Repos() {
					rm.Get(s, id, k.commits)
				}
				rm.GetGlobal(s, k.latest)
			}
		})
	}
	var w sync.WaitGroup
	for g := 0; g < 3; g++ {
		w.Go(func() {
			for i := 0; i < 200; i++ {
				_, _ = p.Apply(func(b *rm.Builder) error {
					return rm.Put(b, rm.EntryID(fmt.Sprint("r", i%50)), k.commits, &commits{[]string{fmt.Sprint(g, i)}})
				})
			}
		})
	}
	w.Wait()
	close(stop)
	wg.Wait()
	if p.Current().Version() != 601 {
		t.Fatalf("version %d", p.Current().Version())
	}
}

// Model-based property test: random Put/Remove sequences against a map model.
func TestModelBased(t *testing.T) {
	for seed := uint64(0); seed < 40; seed++ {
		rnd := rand.New(rand.NewPCG(seed, 99))
		k := newKit()
		p := rm.NewPublisher(k.reg, []rm.Projector{k.projector()}, nil, rm.Deps{})
		model := map[rm.EntryID]*commits{} // nil value = exists without commits
		ids := []rm.EntryID{"a", "b", "c", "d", "e", "f"}
		for step := 0; step < 300; step++ {
			id := ids[rnd.IntN(len(ids))]
			var want error
			s, err := p.Apply(func(b *rm.Builder) error {
				switch rnd.IntN(4) {
				case 0:
					model[id] = model[id]
					return rm.Put(b, id, k.repo, repo{string(id)})
				case 1:
					c := &commits{[]string{fmt.Sprint(step)}}
					if _, ok := model[id]; !ok {
						want = rm.ErrUnknownRepo
					} else {
						model[id] = c
					}
					return rm.Put(b, id, k.commits, c)
				case 2:
					delete(model, id)
					b.Remove(id)
				default:
				}
				return nil
			})
			if want != nil {
				if !errors.Is(err, want) {
					t.Fatalf("seed %d step %d: want %v got %v", seed, step, want, err)
				}
				continue
			}
			must(t, err)
			if got, wantN := len(s.Repos()), len(model); got != wantN || !slices.IsSorted(s.Repos()) {
				t.Fatalf("seed %d step %d: repos %v vs model %d", seed, step, s.Repos(), wantN)
			}
			for id, c := range model {
				got, ok := rm.Get(s, id, k.commits)
				if (c == nil) == ok && !(c == nil && !ok) {
					t.Fatalf("seed %d step %d id %s: commits presence", seed, step, id)
				}
				if c != nil && got != c {
					t.Fatalf("value mismatch")
				}
			}
		}
	}
}

func BenchmarkApplyOneRepo1000(b *testing.B) {
	k := newKit()
	p := rm.NewPublisher(k.reg, []rm.Projector{k.projector()}, nil, rm.Deps{})
	_, err := p.Apply(func(bl *rm.Builder) error {
		for i := 0; i < 1000; i++ {
			id := rm.EntryID(fmt.Sprintf("repo-%04d", i))
			if err := rm.Put(bl, id, k.repo, repo{}); err != nil {
				return err
			}
			if err := rm.Put(bl, id, k.commits, &commits{[]string{"x"}}); err != nil {
				return err
			}
		}
		return nil
	})
	must(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		c := &commits{[]string{"y"}}
		_, _ = p.Apply(func(bl *rm.Builder) error {
			return rm.Put(bl, rm.EntryID(fmt.Sprintf("repo-%04d", i%1000)), k.commits, c)
		})
	}
}

func BenchmarkGetParallel(b *testing.B) {
	k := newKit()
	p := rm.NewPublisher(k.reg, nil, nil, rm.Deps{})
	_, _ = p.Apply(func(bl *rm.Builder) error { return rm.Put(bl, "a", k.repo, repo{"a"}) })
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := p.Current()
			rm.Get(s, "a", k.repo)
		}
	})
}

// The behaviour interfaces of job.Gone / job.Permanent, restated locally: readmodel may import nothing of ours, and its tests
// may import only what readmodel may (architecture 5.2). cmd/scp's TestBehaviourInterfacesMatchRealJob checks the real job package.
func isGone(err error) bool {
	var g interface{ Gone() bool }
	return errors.As(err, &g) && g.Gone()
}
func isPermanent(err error) bool {
	var p interface{ Permanent() bool }
	return errors.As(err, &p) && p.Permanent()
}
