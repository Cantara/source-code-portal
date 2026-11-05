package issues

import (
	"maps"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Ref is an open issue of a repository with where it stands.
type Ref struct {
	Repo  snapshot.RepoKey
	Issue *Issue
	State State
	Kind  IssueKind
}

// feeds are every open issue of a snapshot, what waits first (compareWaiting): of the fleet, per group and per repository.
type feeds struct {
	all     []Ref
	byGroup map[string][]Ref
	byRepo  map[snapshot.RepoKey][]Ref
}

const feedIndex snapshot.Index[feeds] = "issues"

// Feeds is the indexer of the merged issues, for snapshot.NewPublisher.
func Feeds() snapshot.Indexer {
	return feedIndex.Build(func(s *snapshot.Snapshot) feeds {
		f := feeds{byGroup: make(map[string][]Ref, len(s.Groups)), byRepo: make(map[snapshot.RepoKey][]Ref, len(s.Repos))}
		for k, e := range s.Repos {
			set, _ := Of(e)
			if set == nil {
				continue
			}
			refs := make([]Ref, 0, len(set.Issues))
			for i := range set.Issues {
				is := &set.Issues[i]
				refs = append(refs, Ref{Repo: k, Issue: is, State: StateOf(is), Kind: KindOf(is)})
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

// compareWaiting orders what waits first: people's before automation's, then by state (needing triage, unassigned, assigned),
// then the longest idle, then address and number.
func compareWaiting(a, b Ref) int {
	if a.Issue.Bot != b.Issue.Bot {
		if a.Issue.Bot {
			return 1
		}
		return -1
	}
	if c := slices.Index(States, a.State) - slices.Index(States, b.State); c != 0 {
		return c
	}
	if c := a.Issue.Updated.Compare(b.Issue.Updated); c != 0 {
		return c
	}
	return compareAddress(a, b)
}

func compareNewest(a, b Ref) int {
	if c := b.Issue.Created.Compare(a.Issue.Created); c != 0 {
		return c
	}
	return compareAddress(a, b)
}

func compareUpdated(a, b Ref) int {
	if c := b.Issue.Updated.Compare(a.Issue.Updated); c != 0 {
		return c
	}
	return compareAddress(a, b)
}

func compareAddress(a, b Ref) int {
	if c := strings.Compare(string(a.Repo), string(b.Repo)); c != 0 {
		return c
	}
	return a.Issue.Number - b.Issue.Number
}

// By selects whose issues count.
type By uint8

const (
	ByAll        By = iota
	ByPeople        // people's only
	ByAutomation    // automation's only
)

func (b By) keeps(r Ref) bool { return b == ByAll || (b == ByPeople) != r.Issue.Bot }

// Counts are the open issues of refs that by keeps.
func Counts(refs []Ref, by By) oapi.IssueCounts {
	var c oapi.IssueCounts
	for _, r := range refs {
		if !by.keeps(r) {
			continue
		}
		c.Open++
		if r.Issue.Bot {
			c.Automation++
		} else {
			c.People++
			if r.State == StateTriage && (c.OldestTriage == nil || r.Issue.Created.Before(*c.OldestTriage)) {
				t := r.Issue.Created
				c.OldestTriage = &t
			}
		}
		switch r.State {
		case StateTriage:
			c.States.Triage++
		case StateUnassigned:
			c.States.Unassigned++
		case StateAssigned:
			c.States.Assigned++
		}
		switch r.Kind {
		case KindBug:
			c.Kinds.Bug++
		case KindFeature:
			c.Kinds.Feature++
		case KindQuestion:
			c.Kinds.Question++
		default:
			c.Kinds.Other++
		}
	}
	return c
}

// OfGroup is a group's open issues in s, what waits first.
func OfGroup(s *snapshot.Snapshot, id string) []Ref { return feedIndex.Get(s).byGroup[id] }

// OfRepo is a repository's open issues in s, what waits first.
func OfRepo(s *snapshot.Snapshot, k snapshot.RepoKey) []Ref { return feedIndex.Get(s).byRepo[k] }

// All is every open issue of s, what waits first.
func All(s *snapshot.Snapshot) []Ref { return feedIndex.Get(s).all }

// IssueOf is an issue in its API form.
func IssueOf(r Ref) oapi.Issue {
	is := r.Issue
	out := oapi.Issue{Repo: string(r.Repo), Number: is.Number, Title: is.Title, Url: is.URL, Author: is.Author, Bot: is.Bot,
		CreatedAt: is.Created, UpdatedAt: is.Updated, Labels: make([]oapi.IssueLabel, 0, len(is.Labels)),
		Assignees: slices.Clone(is.Assignees), Assigned: is.Assigned, Comments: is.Comments,
		State: oapi.IssueState(r.State), Kind: oapi.IssueKind(r.Kind)}
	if out.Assignees == nil {
		out.Assignees = []string{}
	}
	for _, l := range is.Labels {
		out.Labels = append(out.Labels, oapi.IssueLabel{Name: l.Name, Color: l.Color})
	}
	if is.Milestone != "" {
		out.Milestone = new(is.Milestone)
	}
	if is.Type != "" {
		out.Type = new(is.Type)
	}
	return out
}
