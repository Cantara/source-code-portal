package readmodel

import (
	"sync"
	"testing"
	"time"
)

type Commits struct{ Latest []string }
type Readme struct{ HTML string }

func TestKernel(t *testing.T) {
	reg := &Registry{}
	commits := Register[Commits](reg, "commits")
	readme := Register[Readme](reg, "readme")
	latest := RegisterGlobal[[]string](reg, "commits.latest")

	proj := func(b *Builder) error { // derived index: first commit of every repo, sorted by repo
		v := b.View()
		var out []string
		for _, id := range v.Repos() {
			if c, ok := Get(v, id, commits); ok && len(c.Latest) > 0 {
				out = append(out, string(id)+":"+c.Latest[0])
			}
		}
		PutGlobal(b, latest, out)
		return nil
	}
	p := NewPublisher(reg, []Projector{proj}, nil)
	wake, cancel := p.Subscribe()
	defer cancel()

	s1, err := p.Apply(func(b *Builder) error {
		Put(b, "a", commits, Commits{Latest: []string{"c1"}})
		Put(b, "a", readme, Readme{HTML: "<p>x</p>"})
		Put(b, "b", commits, Commits{Latest: []string{"c9"}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("no wake")
	}
	s2, _ := p.Apply(func(b *Builder) error { Put(b, "a", commits, Commits{Latest: []string{"c2"}}); return nil })
	// old snapshot is unchanged (immutability), unchanged slot is shared, membership slice is shared
	if c, _ := Get(s1, "a", commits); c.Latest[0] != "c1" {
		t.Fatal("s1 mutated")
	}
	r1, _ := Get(s1, "a", readme)
	r2, _ := Get(s2, "a", readme)
	if r1 != r2 {
		t.Fatal("readme must be carried over")
	}
	if &s1.names[0] != &s2.names[0] {
		t.Fatal("names should be shared when membership unchanged")
	}
	if s2.Rev("a") != 2 || s2.Rev("b") != 1 {
		t.Fatalf("rev a=%d b=%d", s2.Rev("a"), s2.Rev("b"))
	}
	g, _ := GetGlobal(s2, latest)
	if len(g) != 2 || g[0] != "a:c2" || s2.Version() != 2 || len(s2.Changed().Repos) != 1 {
		t.Fatalf("%v %+v", g, s2.Changed())
	}
	if cs, ok := p.ChangesSince(1); !ok || len(cs) != 1 || cs[0].Version != 2 {
		t.Fatalf("%v %v", cs, ok)
	}
	// concurrent readers during writes (run with -race)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					s := p.Current()
					for _, id := range s.Repos() {
						Get(s, id, commits)
					}
				}
			}
		})
	}
	for i := 0; i < 200; i++ {
		p.Apply(func(b *Builder) error { Put(b, "b", commits, Commits{Latest: []string{"x"}}); return nil })
	}
	close(stop)
	wg.Wait()
}
