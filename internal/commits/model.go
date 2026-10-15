package commits

import (
	"container/heap"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Commit is one commit of a repository's history.
type Commit struct {
	SHA           string    `json:"sha"`
	Subject       string    `json:"subject"`        // first line of the message
	Body          string    `json:"body,omitempty"` // the rest of the message
	AuthorName    string    `json:"authorName"`
	AuthorLogin   string    `json:"authorLogin,omitempty"`
	CommitterName string    `json:"committerName,omitempty"`
	URL           string    `json:"url"`
	CommittedAt   time.Time `json:"committedAt"`
	Parents       []string  `json:"parents"` // first parent first
}

// Label is a branch or a tag at a commit of the history.
type Label struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // branch | tag
	SHA     string `json:"sha"`
	Default bool   `json:"-"` // the default branch; set at publish
}

// History is a repository's stored history: the default branch, the commits only on other branches, their labels, and the graph
// order the navigator pages through (newest first by commit time, a parent never before its child).
type History struct {
	Main   []Commit
	Side   []Commit
	Labels []Label // the default branch first, then branches, then tags
	Order  []*Commit
	pos    map[string]int     // SHA -> index in Order
	at     map[string][]Label // SHA -> its labels, the default branch first, then branches, then tags
}

// newHistory indexes a history; defaultBranch marks the default branch's label.
func newHistory(main, side []Commit, labels []Label, defaultBranch string) *History {
	h := &History{Main: main, Side: side, Labels: labels, pos: map[string]int{}, at: map[string][]Label{}}
	for i := range h.Labels {
		l := &h.Labels[i]
		l.Default = l.Kind == "branch" && l.Name == defaultBranch
	}
	slices.SortStableFunc(h.Labels, func(a, b Label) int { return rank(a) - rank(b) })
	for _, l := range h.Labels {
		h.at[l.SHA] = append(h.at[l.SHA], l)
	}
	for sha, ls := range h.at {
		slices.SortStableFunc(ls, func(a, b Label) int { return rank(a) - rank(b) })
		h.at[sha] = ls
	}
	h.Order = graphOrder(main, side)
	for i, c := range h.Order {
		h.pos[c.SHA] = i
	}
	return h
}

func rank(l Label) int {
	switch {
	case l.Default:
		return 0
	case l.Kind == "branch":
		return 1
	}
	return 2
}

// LabelsAt returns the labels of the commit sha.
func (h *History) LabelsAt(sha string) []Label { return h.at[sha] }

// Position returns the index of sha in the graph order.
func (h *History) Position(sha string) (int, bool) {
	i, ok := h.pos[sha]
	return i, ok
}

// graphOrder orders the commits newest first, a parent only after all its children (git log --date-order): a topological order by
// Kahn's algorithm that always takes the newest ready commit.
func graphOrder(main, side []Commit) []*Commit {
	by := make(map[string]*Commit, len(main)+len(side))
	for _, cs := range [][]Commit{main, side} {
		for i := range cs {
			if _, dup := by[cs[i].SHA]; !dup {
				by[cs[i].SHA] = &cs[i]
			}
		}
	}
	children := make(map[string]int, len(by))
	for _, c := range by {
		for _, p := range c.Parents {
			if _, ok := by[p]; ok {
				children[p]++
			}
		}
	}
	ready := &newest{}
	for _, c := range by {
		if children[c.SHA] == 0 {
			heap.Push(ready, c)
		}
	}
	out := make([]*Commit, 0, len(by))
	for ready.Len() > 0 {
		c := heap.Pop(ready).(*Commit) //nolint:forcetypeassert // the heap holds commits
		out = append(out, c)
		for _, p := range c.Parents {
			if pc, ok := by[p]; ok {
				if children[p]--; children[p] == 0 {
					heap.Push(ready, pc)
				}
			}
		}
	}
	return out
}

// newest is a heap of commits, the newest (then the greatest SHA) on top.
type newest []*Commit

func (h newest) Len() int { return len(h) }
func (h newest) Less(i, j int) bool {
	if !h[i].CommittedAt.Equal(h[j].CommittedAt) {
		return h[i].CommittedAt.After(h[j].CommittedAt)
	}
	return h[i].SHA > h[j].SHA
}
func (h newest) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *newest) Push(x any)   { *h = append(*h, x.(*Commit)) } //nolint:forcetypeassert // the heap holds commits
func (h *newest) Pop() any {
	old := *h
	c := old[len(old)-1]
	*h = old[:len(old)-1]
	return c
}

// Ref is a commit in a merged feed.
type Ref struct {
	Repo   snapshot.RepoKey
	Commit *Commit // points into the repository's facet
}

// feeds are the merged commits of every repository and of every group, newest first (committedAt desc, repo asc, sha desc).
type feeds struct {
	all     []Ref
	byGroup map[string][]Ref
}

const (
	facet     snapshot.Facet[*History] = "commits" // nil when there is none
	feedIndex snapshot.Index[feeds]    = "commits"
)

// Of returns the default-branch commits of e, newest first, with their freshness.
func Of(e *snapshot.RepoEntry) ([]Commit, snapshot.Freshness) {
	h, f := facet.Get(e)
	if h == nil {
		return nil, f
	}
	return h.Main, f
}

// HistoryOf returns the history of e (nil when there is none) with its freshness.
func HistoryOf(e *snapshot.RepoEntry) (*History, snapshot.Freshness) { return facet.Get(e) }

// Feeds is the indexer of the merged commit feeds, for snapshot.NewPublisher.
func Feeds() snapshot.Indexer {
	return feedIndex.Build(func(s *snapshot.Snapshot) feeds {
		f := feeds{all: merge(s, slices.Sorted(maps.Keys(s.Repos))), byGroup: make(map[string][]Ref, len(s.Groups))}
		for _, g := range s.Groups {
			f.byGroup[g.ID] = merge(s, g.Members)
		}
		return f
	})
}

// feed returns the merged commits of the group with id, or of every repository when id is empty.
func feed(s *snapshot.Snapshot, id string) ([]Ref, bool) {
	f := feedIndex.Get(s)
	if id == "" {
		return f.all, true
	}
	refs, ok := f.byGroup[id]
	return refs, ok
}

// merge merges the commits of keys, newest first.
func merge(s *snapshot.Snapshot, keys []snapshot.RepoKey) []Ref {
	var out []Ref
	for _, k := range keys {
		if e := s.Repos[k]; e != nil {
			cs, _ := Of(e)
			for i := range cs {
				out = append(out, Ref{Repo: k, Commit: &cs[i]})
			}
		}
	}
	slices.SortFunc(out, compareRefs)
	return out
}

// compareRefs orders a feed: committedAt desc, repo asc, sha desc.
func compareRefs(a, b Ref) int {
	if c := b.Commit.CommittedAt.Compare(a.Commit.CommittedAt); c != 0 {
		return c
	}
	if c := strings.Compare(string(a.Repo), string(b.Repo)); c != 0 {
		return c
	}
	return strings.Compare(b.Commit.SHA, a.Commit.SHA)
}

// commitOf converts a commit of the repository at repo to its API form.
func commitOf(repo snapshot.RepoKey, c *Commit) oapi.Commit {
	return oapi.Commit{Repo: string(repo), Sha: c.SHA, Subject: c.Subject, AuthorName: c.AuthorName, AuthorLogin: c.AuthorLogin,
		Url: c.URL, CommittedAt: c.CommittedAt.UTC(), Parents: c.Parents}
}

// LastCommit returns the time of the newest commit of e (zero when there is none) and the commits' freshness.
func LastCommit(e *snapshot.RepoEntry) (time.Time, snapshot.Freshness) {
	cs, fresh := Of(e)
	if len(cs) == 0 {
		return time.Time{}, fresh
	}
	return cs[0].CommittedAt, fresh
}

// Automated reports whether a commit was made by automation: an author named like a GitHub App (`renovate[bot]`) or a Maven release
// commit.
func Automated(c *Commit) bool {
	return strings.HasSuffix(c.AuthorLogin, "[bot]") || strings.HasSuffix(c.AuthorName, "[bot]") ||
		strings.HasPrefix(c.Subject, "[maven-release-plugin]")
}

// LastAuthored returns the time of the newest commit of e made by a person. When every read commit is automated, it returns the oldest
// read commit's time with before set: the last person's commit is older. Zero when there are no commits.
func LastAuthored(e *snapshot.RepoEntry) (at time.Time, before bool) {
	cs, _ := Of(e)
	for i := range cs {
		if !Automated(&cs[i]) {
			return cs[i].CommittedAt, false
		}
	}
	if len(cs) == 0 {
		return time.Time{}, false
	}
	return cs[len(cs)-1].CommittedAt, true
}

// dashboardCommits is how many of the latest commits the dashboard shows.
const dashboardCommits = 10

// Dashboard adds the commits to the dashboard: the latest commits, each group's last activity and the commits' freshness counts.
func Dashboard(s *snapshot.Snapshot, d *oapi.Dashboard) {
	all, _ := feed(s, "")
	for _, r := range all[:min(dashboardCommits, len(all))] {
		d.Latest = append(d.Latest, commitOf(r.Repo, r.Commit))
	}
	for i := range d.Groups {
		if refs, _ := feed(s, d.Groups[i].Id); len(refs) > 0 {
			d.Groups[i].LastActivity = oapi.TimeOrNil(refs[0].Commit.CommittedAt)
		}
	}
	now := time.Now()
	for _, e := range s.Repos {
		_, f := Of(e)
		switch f.Derive(now) {
		case snapshot.StatusFresh:
			d.Freshness.Fresh++
		case snapshot.StatusStale:
			d.Freshness.Stale++
		case snapshot.StatusError:
			d.Freshness.Error++
		default:
			d.Freshness.Unknown++
		}
	}
}

// Repo adds the time of the last commit to a repository summary.
func Repo(e *snapshot.RepoEntry, r *oapi.Repo) {
	if at, _ := LastCommit(e); !at.IsZero() {
		r.LastCommitAt = oapi.TimeOrNil(at)
	}
	if at, _ := LastAuthored(e); !at.IsZero() {
		r.LastAuthoredAt = oapi.TimeOrNil(at)
	}
}

// detailCommits is how many of the newest default-branch commits a repository's detail carries.
const detailCommits = 30

// Detail adds the newest commits and their freshness to a repository's detail.
func Detail(e *snapshot.RepoEntry, d *oapi.RepoDetail) {
	cs, fresh := Of(e)
	d.CommitsFreshness = oapi.FreshnessOf(fresh, time.Now())
	key := e.Repo.Key()
	for i := range cs[:min(len(cs), detailCommits)] {
		d.Commits = append(d.Commits, commitOf(key, &cs[i]))
	}
}
