package pulls

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Ref is an open pull request of a repository with its state.
type Ref struct {
	Repo  snapshot.RepoKey
	Pull  *Pull
	State State
}

// feeds are every open pull request of a snapshot, whose turn first (compareWaiting): of the fleet, per group and per repository.
type feeds struct {
	all     []Ref
	byGroup map[string][]Ref
	byRepo  map[snapshot.RepoKey][]Ref
}

const feedIndex snapshot.Index[feeds] = "pulls"

// Feeds is the indexer of the merged pull requests, for snapshot.NewPublisher.
func Feeds() snapshot.Indexer {
	return feedIndex.Build(func(s *snapshot.Snapshot) feeds {
		f := feeds{byGroup: make(map[string][]Ref, len(s.Groups)), byRepo: make(map[snapshot.RepoKey][]Ref, len(s.Repos))}
		for k, e := range s.Repos {
			r, _ := Of(e)
			if r == nil {
				continue
			}
			refs := make([]Ref, 0, len(r.Pulls))
			for i := range r.Pulls {
				refs = append(refs, Ref{Repo: k, Pull: &r.Pulls[i], State: StateOf(&r.Pulls[i])})
			}
			slices.SortFunc(refs, compareWaiting)
			f.byRepo[k] = refs
		}
		f.all = mergeRefs(f.byRepo, slices.Collect(maps.Keys(s.Repos)))
		for _, g := range s.Groups {
			f.byGroup[g.ID] = mergeRefs(f.byRepo, g.Members)
		}
		return f
	})
}

func mergeRefs(byRepo map[snapshot.RepoKey][]Ref, keys []snapshot.RepoKey) []Ref {
	var out []Ref
	for _, k := range keys {
		out = append(out, byRepo[k]...)
	}
	slices.SortFunc(out, compareWaiting)
	return out
}

// compareWaiting orders whose turn first: people's before automation's, those waiting for reviewers first, drafts last, then the
// longest idle, then address and number.
func compareWaiting(a, b Ref) int {
	if a.Pull.Bot != b.Pull.Bot {
		return boolOrder(a.Pull.Bot)
	}
	if ra, rb := a.State == StateReview, b.State == StateReview; ra != rb {
		return boolOrder(rb)
	}
	if da, db := a.State == StateDraft, b.State == StateDraft; da != db {
		return boolOrder(da)
	}
	if c := a.Pull.Updated.Compare(b.Pull.Updated); c != 0 {
		return c
	}
	return compareAddress(a, b)
}

func compareNewest(a, b Ref) int {
	if c := b.Pull.Created.Compare(a.Pull.Created); c != 0 {
		return c
	}
	return compareAddress(a, b)
}

func compareUpdated(a, b Ref) int {
	if c := b.Pull.Updated.Compare(a.Pull.Updated); c != 0 {
		return c
	}
	return compareAddress(a, b)
}

func compareAddress(a, b Ref) int {
	if c := strings.Compare(string(a.Repo), string(b.Repo)); c != 0 {
		return c
	}
	return a.Pull.Number - b.Pull.Number
}

// boolOrder is 1 when the first's flag puts it after the second.
func boolOrder(after bool) int {
	if after {
		return 1
	}
	return -1
}

// By selects whose pull requests count.
type By uint8

const (
	ByAll        By = iota
	ByPeople        // people's only
	ByAutomation    // automation's only
)

func (b By) keeps(r Ref) bool {
	return b == ByAll || (b == ByPeople) != r.Pull.Bot
}

// Counts are the open pull requests of refs that by keeps.
func Counts(refs []Ref, by By) oapi.PullCounts {
	var c oapi.PullCounts
	for _, r := range refs {
		if !by.keeps(r) {
			continue
		}
		c.Open++
		if r.Pull.Bot {
			c.Automation++
		} else {
			c.People++
			if r.State == StateReview && (c.OldestReview == nil || r.Pull.Updated.Before(*c.OldestReview)) {
				t := r.Pull.Updated
				c.OldestReview = &t
			}
		}
		switch r.State {
		case StateDraft:
			c.States.Draft++
		case StateConflicts:
			c.States.Conflicts++
		case StateFailing:
			c.States.Failing++
		case StateChanges:
			c.States.Changes++
		case StateReview:
			c.States.Review++
		case StateChecks:
			c.States.Checks++
		case StateReady:
			c.States.Ready++
		default:
			c.States.Unknown++
		}
	}
	return c
}

// OfGroup is a group's open pull requests in s, whose turn first; OfRepo a repository's; All the fleet's.
func OfGroup(s *snapshot.Snapshot, id string) []Ref { return feedIndex.Get(s).byGroup[id] }

// OfRepo is a repository's open pull requests in s, whose turn first.
func OfRepo(s *snapshot.Snapshot, k snapshot.RepoKey) []Ref { return feedIndex.Get(s).byRepo[k] }

// All is every open pull request of s, whose turn first.
func All(s *snapshot.Snapshot) []Ref { return feedIndex.Get(s).all }

// Open is the open pull requests of a repository (-1 when not read yet).
func Open(e *snapshot.RepoEntry) int {
	if r, _ := Of(e); r != nil {
		return r.Total
	}
	return -1
}

// Waiting is what a repository's health reads of its pull requests: how long the person's pull request that waits longest for a
// review has been idle, and how many of automation's are older than old.
func Waiting(e *snapshot.RepoEntry, old time.Duration, now time.Time) (review time.Duration, stale int) {
	r, _ := Of(e)
	if r == nil {
		return 0, 0
	}
	for i := range r.Pulls {
		p := &r.Pulls[i]
		switch {
		case p.Bot && now.Sub(p.Created) > old:
			stale++
		case !p.Bot && StateOf(p) == StateReview:
			review = max(review, now.Sub(p.Updated))
		}
	}
	return review, stale
}

// PullOf is a pull request in its API form.
func PullOf(r Ref) oapi.PullRequest {
	p := r.Pull
	out := oapi.PullRequest{Repo: string(r.Repo), Number: p.Number, Title: p.Title, Url: p.URL, Author: p.Author, Bot: p.Bot,
		Draft: p.Draft, Fork: p.Fork, CreatedAt: p.Created, UpdatedAt: p.Updated, Base: p.Base, Head: p.Head,
		Labels: make([]oapi.PullLabel, 0, len(p.Labels)), Requested: p.Requested, State: oapi.PullState(r.State),
		WaitsFor: oapi.PullRequestWaitsFor(r.State.Waits()), Checks: checksOf(p.Checks)}
	for _, l := range p.Labels {
		out.Labels = append(out.Labels, oapi.PullLabel{Name: l.Name, Color: l.Color})
	}
	if p.Stats != nil {
		files := p.Stats.Files
		out.Stats = &oapi.CommitStats{Additions: p.Stats.Additions, Deletions: p.Stats.Deletions, Files: &files}
	}
	return out
}

func checksOf(s string) oapi.PullRequestChecks {
	switch s {
	case "SUCCESS":
		return oapi.PullRequestChecksPassing
	case "FAILURE", "ERROR":
		return oapi.PullRequestChecksFailing
	case "PENDING", "EXPECTED":
		return oapi.PullRequestChecksRunning
	}
	return oapi.PullRequestChecksNone
}
