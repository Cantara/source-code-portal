package commits

import (
	"cmp"
	"container/heap"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/health"
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
	Parents       []string  `json:"parents"`           // first parent first
	Stats         *Stats    `json:"stats,omitempty"`   // nil until asked (design/diffstat.md)
	NoStats       bool      `json:"noStats,omitempty"` // asked, and GitHub had none: never asked again
}

// Stats are what a commit changed: lines added and removed, and files (-1 when GitHub does not say).
type Stats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Files     int `json:"files"`
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
	Rows   []Row              // the git graph of Order
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
	h.Rows = layout(h.Order, h.held)
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

// held reports whether sha is in the history.
func (h *History) held(sha string) bool {
	_, ok := h.pos[sha]
	return ok
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
	Person bool    // a person's work in the view (not automation, not a sweep), an agent's included; set in the feeds index only
	Origin Origin  // set in the feeds index only
	AI     string  // the assistant's family (Assistant) when Origin.AI
}

// feeds are the merged commits of every repository, of every group and of each repository, newest first (committedAt desc, repo asc,
// sha desc), each commit classified once per snapshot, and the sweeps among them.
type feeds struct {
	all     []Ref
	byGroup map[string][]Ref
	byRepo  map[snapshot.RepoKey][]Ref
	sweeps  map[sweep]bool
}

// sweep is a commit subject on a day (UTC). One that lands in more than sweepRepos repositories is a change to the fleet, not work in
// each repository: it is not counted as a person's activity.
type sweep struct {
	year, day int
	subject   string
}

const sweepRepos = 10

func sweepOf(c *Commit) sweep {
	t := c.CommittedAt.UTC()
	return sweep{t.Year(), t.YearDay(), c.Subject}
}

// sweepsOf finds the sweeps among the default-branch commits of the view.
func sweepsOf(s *snapshot.Snapshot) map[sweep]bool {
	repos := map[sweep]map[snapshot.RepoKey]bool{}
	for k, e := range s.Repos {
		cs, _ := Of(e)
		for i := range cs {
			sw := sweepOf(&cs[i])
			if repos[sw] == nil {
				repos[sw] = map[snapshot.RepoKey]bool{}
			}
			repos[sw][k] = true
		}
	}
	out := map[sweep]bool{}
	for sw, ks := range repos {
		if len(ks) > sweepRepos {
			out[sw] = true
		}
	}
	return out
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
		sweeps := sweepsOf(s)
		f := feeds{byGroup: make(map[string][]Ref, len(s.Groups)), byRepo: make(map[snapshot.RepoKey][]Ref, len(s.Repos)), sweeps: sweeps}
		for k, e := range s.Repos {
			f.byRepo[k] = refsOf(k, e, sweeps)
		}
		f.all = merge(f.byRepo, slices.Collect(maps.Keys(s.Repos)))
		for _, g := range s.Groups {
			f.byGroup[g.ID] = merge(f.byRepo, g.Members)
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

// refsOf classifies the default-branch commits of the repository k, newest first, marking a person's work.
func refsOf(k snapshot.RepoKey, e *snapshot.RepoEntry, sweeps map[sweep]bool) []Ref {
	cs, _ := Of(e)
	out := make([]Ref, 0, len(cs))
	for i := range cs {
		kind, ai := Classify(&cs[i])
		out = append(out, Ref{Repo: k, Commit: &cs[i], Origin: kind, AI: Assistant(ai),
			Person: kind != OriginAutomation && !sweeps[sweepOf(&cs[i])]})
	}
	slices.SortFunc(out, compareRefs)
	return out
}

// merge merges the classified commits of keys, newest first.
func merge(byRepo map[snapshot.RepoKey][]Ref, keys []snapshot.RepoKey) []Ref {
	var out []Ref
	for _, k := range keys {
		out = append(out, byRepo[k]...)
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

// commitOf converts a classified commit to its API form; only a commit with an AI is classified again, for the assistant's full name
// (the Ref holds its family).
func commitOf(r Ref) oapi.Commit {
	kind, ai := r.Origin, ""
	if kind.AI() {
		_, ai = Classify(r.Commit)
	}
	return apiCommit(r.Repo, r.Commit, kind, ai)
}

// apiCommit is the API form of the commit c of the repository at repo, of origin kind, naming the assistant ai.
func apiCommit(repo snapshot.RepoKey, c *Commit, kind Origin, ai string) oapi.Commit {
	out := oapi.Commit{Repo: string(repo), Sha: c.SHA, Subject: c.Subject, AuthorName: c.AuthorName, AuthorLogin: c.AuthorLogin,
		Url: c.URL, CommittedAt: c.CommittedAt.UTC(), Parents: c.Parents, Kind: oapi.CommitKind(kind.String()), Assistant: ai}
	out.Stats = statsOf(c.Stats)
	return out
}

// LastCommit returns the time of the newest commit of e (zero when there is none) and the commits' freshness.
func LastCommit(e *snapshot.RepoEntry) (time.Time, snapshot.Freshness) {
	cs, fresh := Of(e)
	if len(cs) == 0 {
		return time.Time{}, fresh
	}
	return cs[0].CommittedAt, fresh
}

// Automated reports whether a commit was made by automation (Classify): CI, dependency and release bots, not coding agents.
func Automated(c *Commit) bool {
	k, _ := Classify(c)
	return k == OriginAutomation
}

// Sweep reports whether c is part of a sweep in the view s: its subject landed in more than 10 repositories on its day.
func Sweep(s *snapshot.Snapshot, c *Commit) bool { return feedIndex.Get(s).sweeps[sweepOf(c)] }

// LastAuthored returns the time of the newest commit of the repository k in the view s that is work of the lens w (Counts). When no
// read commit is, it returns the oldest read commit's time with before set: the last such commit is older. Zero when there are no
// commits.
func LastAuthored(s *snapshot.Snapshot, k snapshot.RepoKey, w Work) (at time.Time, before bool) {
	refs := feedIndex.Get(s).byRepo[k]
	for _, r := range refs {
		if w.Counts(r.Origin, r.Person) {
			return r.Commit.CommittedAt, false
		}
	}
	if len(refs) == 0 {
		return time.Time{}, false
	}
	return refs[len(refs)-1].Commit.CommittedAt, true
}

const (
	dashboardCommits   = 10                // how many of the latest commits by people the dashboard shows
	activityDays       = health.WindowDays // the window of the activity counts and authors
	activityWeeks      = 12                // the weekly counts
	activityAuthors    = 5                 // the authors named
	activityRecent     = 3                 // the repositories people changed last
	activityAssistants = 3                 // the AI assistants named
)

// Dashboard adds the commits to the dashboard through its lens (d.Work): the latest commits of the work that counts, the activity of
// the fleet and of each group (with its members' last change of that work), each group's last activity and the commits' freshness
// counts.
func Dashboard(s *snapshot.Snapshot, d *oapi.Dashboard) {
	now := time.Now()
	w := WorkOf(d.Work, WorkEvery)
	all, _ := feed(s, "")
	for _, r := range all {
		if len(d.Latest) == dashboardCommits {
			break
		}
		if w.Counts(r.Origin, r.Person) {
			d.Latest = append(d.Latest, commitOf(r))
		}
	}
	d.Activity = activity(all, now, w)
	for i := range d.Groups {
		g := &d.Groups[i]
		refs, _ := feed(s, g.Id)
		if len(refs) > 0 {
			g.LastActivity = oapi.TimeOrNil(refs[0].Commit.CommittedAt)
		}
		g.Activity = activity(refs, now, w)
		for j := range g.Members {
			m := &g.Members[j]
			// Before: none of the commits read is of the work that counts, so the member has no last change of it.
			if at, before := LastAuthored(s, snapshot.RepoKey(m.Address), w); !at.IsZero() && !before {
				m.LastAuthoredAt = oapi.TimeOrNil(at)
			}
		}
	}
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

// activity sums a feed (newest first) at now through the lens w: people's commits by hand and with an AI, automation and sweeps in the
// last 30 days, each kind per week, the assistants, and of the work that counts the people, the last commit and the repositories it
// changed last.
func activity(refs []Ref, now time.Time, w Work) oapi.Activity {
	const day, week = 24 * time.Hour, 7 * 24 * time.Hour
	a := oapi.Activity{WeeklyHand: make([]int, activityWeeks), WeeklyAi: make([]int, activityWeeks),
		WeeklyAutomation: make([]int, activityWeeks), Authors: []oapi.Person{},
		Assistants: []string{}, Recent: []oapi.RecentRepo{}}
	window, weeks := now.Add(-activityDays*day), now.Add(-activityWeeks*week)
	type author struct {
		name, login string
		commits     int
		first       int // order of appearance: the newest commit first
	}
	authors, assistants := map[string]*author{}, map[string]*author{}
	for _, r := range refs {
		c := r.Commit
		if c.CommittedAt.Before(weeks) && a.LastAuthoredAt != nil && len(a.Recent) == activityRecent {
			break
		}
		counts := w.Counts(r.Origin, r.Person)
		if counts {
			if a.LastAuthoredAt == nil {
				a.LastAuthoredAt = oapi.TimeOrNil(c.CommittedAt)
			}
			if len(a.Recent) < activityRecent && !slices.ContainsFunc(a.Recent, func(x oapi.RecentRepo) bool { return x.Repo == string(r.Repo) }) {
				a.Recent = append(a.Recent, oapi.RecentRepo{Repo: string(r.Repo), At: c.CommittedAt.UTC()})
			}
		}
		if !c.CommittedAt.Before(weeks) {
			wk := activityWeeks - 1 - min(max(int(now.Sub(c.CommittedAt)/week), 0), activityWeeks-1)
			switch kindOf(r.Origin, r.Person) {
			case WorkAI:
				a.WeeklyAi[wk]++
			case WorkHand:
				a.WeeklyHand[wk]++
			default:
				a.WeeklyAutomation[wk]++
			}
		}
		if c.CommittedAt.Before(window) {
			continue
		}
		switch {
		case r.Origin == OriginAutomation:
			a.Automation++
		case !r.Person:
			a.Sweeps++
		default:
			if r.Origin.AI() {
				a.Ai++
				if assistants[r.AI] == nil {
					assistants[r.AI] = &author{name: r.AI, first: len(assistants)}
				}
				assistants[r.AI].commits++
			} else {
				a.Hand++
			}
			if counts || w&WorkTeam == 0 { // a lens of automation alone still names the people beside it
				key := strings.ToLower(cmp.Or(c.AuthorLogin, c.AuthorName))
				if authors[key] == nil {
					authors[key] = &author{name: cmp.Or(c.AuthorName, c.AuthorLogin), login: c.AuthorLogin, first: len(authors)}
				}
				authors[key].commits++
			}
		}
	}
	byCommits := func(x, y *author) int {
		return cmp.Or(cmp.Compare(y.commits, x.commits), cmp.Compare(x.first, y.first))
	}
	ranked := slices.SortedFunc(maps.Values(authors), byCommits)
	a.AuthorCount = len(ranked)
	for _, au := range ranked[:min(activityAuthors, len(ranked))] {
		a.Authors = append(a.Authors, oapi.Person{Name: au.name, Login: loginOf(au.login)})
	}
	tools := slices.SortedFunc(maps.Values(assistants), byCommits)
	for _, t := range tools[:min(activityAssistants, len(tools))] {
		a.Assistants = append(a.Assistants, t.name)
	}
	return a
}

// Repo adds the time of the last commit to a repository summary.
func Repo(s *snapshot.Snapshot, e *snapshot.RepoEntry, r *oapi.Repo) {
	if at, _ := LastCommit(e); !at.IsZero() {
		r.LastCommitAt = oapi.TimeOrNil(at)
	}
	if at, _ := LastAuthored(s, e.Repo.Key(), WorkTeam); !at.IsZero() {
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
		kind, ai := Classify(&cs[i])
		d.Commits = append(d.Commits, apiCommit(key, &cs[i], kind, ai))
	}
}

// loginOf is the login, absent when the commits carry none.
func loginOf(login string) *string {
	if login == "" {
		return nil
	}
	return &login
}

// statsOf is the API form of a commit's diff stats; nil when unknown, its files left out when GitHub did not say.
func statsOf(s *Stats) *oapi.CommitStats {
	if s == nil {
		return nil
	}
	out := &oapi.CommitStats{Additions: s.Additions, Deletions: s.Deletions}
	if s.Files >= 0 {
		out.Files = new(s.Files)
	}
	return out
}
