package scm_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
)

func bigSpec(n, groups int) scm.MembershipSpec {
	var sp scm.MembershipSpec
	for i := 0; i < n; i++ {
		vis := scm.VisPublic
		if i%10 == 0 {
			vis = scm.VisPrivate
		}
		slug := scm.Slug(fmt.Sprintf("repo-%05d", i))
		sp.Repos = append(sp.Repos, scm.MemberRepo{Slug: slug, Ref: scm.RepoRef{Source: "acme", ID: scm.RepoID(i + 1), Owner: "acme", Name: string(slug)}, Visibility: vis})
	}
	for g := 0; g < groups; g++ {
		gs := scm.GroupSpec{Meta: scm.GroupMeta{ID: scm.GroupID(fmt.Sprintf("group-%02d", g))}}
		for i := g; i < n; i += groups / 2 { // every repository is in about two groups
			gs.Members = append(gs.Members, sp.Repos[i].Slug)
		}
		gs.Default = gs.Members[0]
		sp.Groups = append(sp.Groups, gs)
	}
	return sp
}

func BenchmarkNewMembership5000(b *testing.B) {
	sp := bigSpec(5000, 50)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := scm.NewMembership(sp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMembershipReads(b *testing.B) {
	m, _ := scm.NewMembership(bigSpec(5000, 50))
	ref := scm.RepoRef{Source: "acme", Owner: "ACME", Name: "Repo-02500"}
	b.ReportAllocs()
	for b.Loop() {
		s, _ := m.SlugFor(ref)
		_ = m.GroupsOf(s)
		_ = m.VisibleReposOf("group-03", scm.ViewPublic)
	}
}

// TestMembershipHeap reports (and bounds) the retained heap of a 5,000-repository, 50-group membership.
func TestMembershipHeap(t *testing.T) {
	sp := bigSpec(5000, 50)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	m, err := scm.NewMembership(sp)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(m)
	used := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("membership of 5000 repositories in 50 groups retains about %d KiB", used/1024)
	if used > 8<<20 {
		t.Errorf("membership retains %d bytes, budget 8 MiB", used)
	}
}
