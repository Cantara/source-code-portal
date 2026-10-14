package snap

import (
	"container/heap"
	"fmt"
	"maps"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type Commit struct {
	SHA     string
	Author  string
	Message string
	At      time.Time
}

type RepoEntry struct {
	Name        string
	Description string
	Groups      []string
	ReadmeRaw   string
	ReadmeHTML  string
	Commits     []Commit // newest first
	Status      [5]struct {
		State string
		At    time.Time
	}
	FetchedAt time.Time
	ETag      string
}

type CommitRef struct {
	Repo string
	C    *Commit
}

type Snapshot struct {
	Version       uint64
	Repos         map[string]*RepoEntry
	Names         []string            // sorted
	ByGroup       map[string][]string // group -> sorted repo names
	LatestGlobal  []CommitRef
	LatestByGroup map[string][]CommitRef
}

const (
	nRepos  = 1000
	nGroups = 60
	topN    = 100
)

func makeRepo(i int, r *rand.Rand, base time.Time) *RepoEntry {
	e := &RepoEntry{
		Name:        fmt.Sprintf("repo-%04d", i),
		Description: strings.Repeat("d", 80),
		ReadmeRaw:   strings.Repeat("r", 20_000),
		ReadmeHTML:  strings.Repeat("h", 25_000),
		FetchedAt:   base,
		ETag:        `W/"` + strings.Repeat("e", 32) + `"`,
	}
	// 1-2 groups
	g := r.IntN(nGroups)
	e.Groups = []string{fmt.Sprintf("group-%02d", g)}
	if r.IntN(2) == 0 {
		e.Groups = append(e.Groups, fmt.Sprintf("group-%02d", (g+1+r.IntN(nGroups-1))%nGroups))
	}
	e.Commits = make([]Commit, 30)
	t := base.Add(-time.Duration(r.IntN(100000)) * time.Minute)
	for j := range e.Commits {
		t = t.Add(-time.Duration(1+r.IntN(5000)) * time.Minute)
		e.Commits[j] = Commit{SHA: strings.Repeat("a", 40), Author: "someone", Message: strings.Repeat("m", 70), At: t}
	}
	return e
}

type headItem struct {
	repo string
	list []Commit
	i    int
}
type mh []*headItem

func (h mh) Len() int           { return len(h) }
func (h mh) Less(a, b int) bool { return h[a].list[h[a].i].At.After(h[b].list[h[b].i].At) }
func (h mh) Swap(a, b int)      { h[a], h[b] = h[b], h[a] }
func (h *mh) Push(x any)        { *h = append(*h, x.(*headItem)) }
func (h *mh) Pop() any          { o := *h; n := len(o); x := o[n-1]; *h = o[:n-1]; return x }

// latestN: k-way merge over per-repo newest-first commit lists.
func latestN(repos map[string]*RepoEntry, names []string, n int) []CommitRef {
	h := make(mh, 0, len(names))
	for _, nm := range names {
		if e := repos[nm]; e != nil && len(e.Commits) > 0 {
			h = append(h, &headItem{repo: nm, list: e.Commits})
		}
	}
	heap.Init(&h)
	out := make([]CommitRef, 0, n)
	for len(out) < n && h.Len() > 0 {
		it := h[0]
		out = append(out, CommitRef{Repo: it.repo, C: &it.list[it.i]})
		it.i++
		if it.i >= len(it.list) {
			heap.Pop(&h)
		} else {
			heap.Fix(&h, 0)
		}
	}
	return out
}

func fullBuild(version uint64, repos map[string]*RepoEntry) *Snapshot {
	s := &Snapshot{Version: version, Repos: repos, ByGroup: map[string][]string{}, LatestByGroup: map[string][]CommitRef{}}
	s.Names = slices.Sorted(maps.Keys(repos))
	for _, nm := range s.Names {
		for _, g := range repos[nm].Groups {
			s.ByGroup[g] = append(s.ByGroup[g], nm)
		}
	}
	s.LatestGlobal = latestN(repos, s.Names, topN)
	for g, members := range s.ByGroup {
		s.LatestByGroup[g] = latestN(repos, members, 50)
	}
	return s
}

// cowApply: structural sharing. Membership unchanged (same repo set), so Names/ByGroup are shared.
// Only the repo map is copied, and indexes touching changed repos are recomputed.
func cowApply(prev *Snapshot, changed []*RepoEntry) *Snapshot {
	s := &Snapshot{Version: prev.Version + 1, Names: prev.Names, ByGroup: prev.ByGroup}
	s.Repos = maps.Clone(prev.Repos)
	affected := map[string]struct{}{}
	for _, e := range changed {
		s.Repos[e.Name] = e
		for _, g := range e.Groups {
			affected[g] = struct{}{}
		}
	}
	s.LatestGlobal = latestN(s.Repos, s.Names, topN)
	s.LatestByGroup = maps.Clone(prev.LatestByGroup)
	for g := range affected {
		s.LatestByGroup[g] = latestN(s.Repos, s.ByGroup[g], 50)
	}
	return s
}

func corpus() map[string]*RepoEntry {
	r := rand.New(rand.NewPCG(1, 2))
	base := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	m := make(map[string]*RepoEntry, nRepos)
	for i := 0; i < nRepos; i++ {
		e := makeRepo(i, r, base)
		m[e.Name] = e
	}
	return m
}

func BenchmarkFullRebuild(b *testing.B) {
	repos := corpus()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fullBuild(uint64(i), repos)
	}
}

func BenchmarkCoW1(b *testing.B) {
	repos := corpus()
	prev := fullBuild(0, repos)
	r := rand.New(rand.NewPCG(3, 4))
	base := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	changed := makeRepo(5, r, base)
	changed.Groups = prev.Repos["repo-0005"].Groups
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cowApply(prev, []*RepoEntry{changed})
	}
}

func BenchmarkCoW50(b *testing.B) {
	repos := corpus()
	prev := fullBuild(0, repos)
	r := rand.New(rand.NewPCG(3, 4))
	base := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	var ch []*RepoEntry
	for i := 0; i < 50; i++ {
		e := makeRepo(i*7, r, base)
		e.Groups = prev.Repos[e.Name].Groups
		ch = append(ch, e)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cowApply(prev, ch)
	}
}

func BenchmarkMapCloneOnly(b *testing.B) {
	repos := corpus()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = maps.Clone(repos)
	}
}

// Readers hammering Load()+lookup while a writer swaps a CoW snapshot every 1ms.
func BenchmarkReadersDuringSwaps(b *testing.B) {
	repos := corpus()
	var ptr atomic.Pointer[Snapshot]
	ptr.Store(fullBuild(0, repos))
	stop := make(chan struct{})
	go func() {
		r := rand.New(rand.NewPCG(9, 9))
		base := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
		t := time.NewTicker(time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				prev := ptr.Load()
				e := makeRepo(r.IntN(nRepos), r, base)
				e.Groups = prev.Repos[e.Name].Groups
				ptr.Store(cowApply(prev, []*RepoEntry{e}))
			}
		}
	}()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewPCG(uint64(rand.Int()), 5))
		for pb.Next() {
			s := ptr.Load()
			e := s.Repos[s.Names[r.IntN(len(s.Names))]]
			_ = len(e.Commits)
			_ = s.LatestGlobal[0]
		}
	})
	close(stop)
}

func heapMB() float64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

func TestMemory(t *testing.T) {
	base := heapMB()
	repos := corpus()
	s1 := fullBuild(1, repos)
	one := heapMB()
	// second snapshot sharing entries (CoW of 1 repo)
	r := rand.New(rand.NewPCG(3, 4))
	ch := makeRepo(5, r, time.Now())
	ch.Groups = s1.Repos["repo-0005"].Groups
	s2 := cowApply(s1, []*RepoEntry{ch})
	two := heapMB()
	// full independent deep rebuild snapshot (fresh index, shared entries)
	s3 := fullBuild(3, s2.Repos)
	three := heapMB()
	t.Logf("baseline=%.1fMB  one snapshot (1000 repos, 20KB raw+25KB html README, 30 commits)=%.1fMB  +CoW snapshot=%.2fMB  +full-rebuild-index snapshot sharing entries=%.2fMB",
		base, one-base, two-one, three-two)
	runtime.KeepAlive(s1)
	runtime.KeepAlive(s2)
	runtime.KeepAlive(s3)
}
