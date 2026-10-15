package commits

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Commit is one default-branch commit.
type Commit struct {
	SHA         string    `json:"sha"`
	Subject     string    `json:"subject"` // first line of the message
	AuthorName  string    `json:"authorName"`
	AuthorLogin string    `json:"authorLogin,omitempty"`
	URL         string    `json:"url"`
	CommittedAt time.Time `json:"committedAt"`
	Parents     []string  `json:"parents"` // first parent first
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
	facet     snapshot.Facet[[]Commit] = "commits" // newest first
	feedIndex snapshot.Index[feeds]    = "commits"
)

// Of returns the commits of e, newest first, with their freshness.
func Of(e *snapshot.RepoEntry) ([]Commit, snapshot.Freshness) { return facet.Get(e) }

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
}

// Detail adds the commits and their freshness to a repository's detail.
func Detail(e *snapshot.RepoEntry, d *oapi.RepoDetail) {
	cs, fresh := Of(e)
	d.CommitsFreshness = oapi.FreshnessOf(fresh, time.Now())
	key := e.Repo.Key()
	for i := range cs {
		d.Commits = append(d.Commits, commitOf(key, &cs[i]))
	}
}
