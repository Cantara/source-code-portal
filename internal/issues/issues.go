// Package issues keeps every repository's open issues and says for each where it stands (design/issues.md, P-69).
package issues

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/actor"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

// Kind is the job kind that reads a repository's open issues.
const Kind = "issues"

const (
	ttl       = 15 * time.Minute // webhooks notify the job; an unchanged page is a free 304
	settle    = time.Minute      // anyone may comment on a public repository's issue: a stream of comments costs one read a minute
	maxTitle  = 200
	maxName   = 100
	maxPeople = 3
)

// Events are the webhook deliveries that re-read a repository's issues (webhook.Normalise passes them by their names).
// Not issue_comment: anyone may comment, and a comment changes an issue's idle time and count, not where it stands.
// ConfirmAfter is when a run caused by an event reads again: GitHub's listing can be a few seconds behind the event.
const ConfirmAfter = 30 * time.Second

var Events = []sync.Event{"issues", sync.EventOpen} //nolint:gochecknoglobals // a constant list

// Label is an issue's label.
type Label = untrusted.Label

// Issue is an open issue as the listing gives it, cleaned.
type Issue struct {
	Number    int       `json:"n"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	Bot       bool      `json:"bot,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Labels    []Label   `json:"labels,omitempty"`
	Assignees []string  `json:"assignees,omitempty"` // the first maxPeople
	Assigned  int       `json:"assigned,omitempty"`  // every assignee counted
	Comments  int       `json:"comments,omitempty"`
	Milestone string    `json:"milestone,omitempty"`
	Type      string    `json:"type,omitempty"` // GitHub's issue type
}

// Set is a repository's open issues, the most recently updated first.
type Set struct {
	Total  int     `json:"total"`           // open issues on GitHub when Exact, else at least len(Issues)
	Exact  bool    `json:"exact,omitempty"` // every page was read
	Issues []Issue `json:"issues"`
}

// State is where an issue stands, the first that holds.
type State string

const (
	StateAssigned   State = "assigned"   // someone has it
	StateTriage     State = "triage"     // no label, milestone or issue type: nobody has looked at it
	StateUnassigned State = "unassigned" // looked at, nobody took it
)

// States are the states in the order the lists tell them: what waits first.
var States = []State{StateTriage, StateUnassigned, StateAssigned} //nolint:gochecknoglobals // a constant list

// StateOf is where i stands.
func StateOf(i *Issue) State {
	switch {
	case i.Assigned > 0:
		return StateAssigned
	case len(i.Labels) == 0 && i.Milestone == "" && i.Type == "":
		return StateTriage
	}
	return StateUnassigned
}

// IssueKind is what an issue asks for.
type IssueKind string

const (
	KindBug      IssueKind = "bug"
	KindFeature  IssueKind = "feature"
	KindQuestion IssueKind = "question"
	KindOther    IssueKind = "other"
)

// Kinds are the kinds in the order the lists tell them.
var Kinds = []IssueKind{KindBug, KindFeature, KindQuestion, KindOther} //nolint:gochecknoglobals // a constant list

// KindOf is GitHub's issue type when it names a kind, else the first label that does, else other.
func KindOf(i *Issue) IssueKind {
	if k := kindOfWord(i.Type); k != KindOther {
		return k
	}
	for _, l := range i.Labels {
		if k := kindOfWord(l.Name); k != KindOther {
			return k
		}
	}
	return KindOther
}

// kindOfWord reads a type or label name: "bug", "type: bug", "kind/feature", "enhancement", "feature request".
func kindOfWord(s string) IssueKind {
	w := strings.ToLower(s)
	if _, after, ok := strings.Cut(w, ":"); ok {
		w = after
	}
	if i := strings.LastIndexByte(w, '/'); i >= 0 {
		w = w[i+1:]
	}
	switch strings.TrimSpace(w) {
	case "bug", "defect", "regression", "crash", "bug report", "bugfix":
		return KindBug
	case "feature", "enhancement", "improvement", "feature request", "new feature":
		return KindFeature
	case "question", "support", "help":
		return KindQuestion
	}
	return KindOther
}

const facet snapshot.Facet[*Set] = "issues" // nil when not read yet

// Of is a repository's open issues (nil when not read yet) with their freshness.
func Of(e *snapshot.RepoEntry) (*Set, snapshot.Freshness) { return facet.Get(e) }

// Options are what the job reads of the catalogue.
type Options struct {
	// Repo is the catalogued repository at a key: an archived one is read once.
	Repo func(snapshot.RepoKey) (snapshot.Repo, bool)
}

// Issues is the issues feature.
type Issues struct {
	sources map[string]Fetcher
	o       Options
}

// New returns the feature for one Fetcher per source id.
func New(sources map[string]Fetcher, o Options) *Issues { return &Issues{sources: sources, o: o} }

// Spec is the per-repository issues job.
func (f *Issues) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, On: Events, Settle: settle, Run: f.run, Publish: publish}
}

func (f *Issues) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	src := f.sources[j.Source]
	if src == nil {
		return sync.Result{}, errors.New("issues: unknown source " + j.Source)
	}
	if r, ok := f.o.Repo(j.Repo); ok && r.Archived && j.Prev.State == snapshot.StateOK {
		return sync.Result{Unchanged: true}, nil // an archived repository's issues cannot change
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	listed, exact, err := src.Open(ctx, owner, name)
	if err != nil {
		return sync.Result{}, err
	}
	payload, err := json.Marshal(build(listed, exact))
	if err != nil {
		return sync.Result{}, err
	}
	res := sync.Result{Payload: payload, Unchanged: j.Prev.State == snapshot.StateOK && string(j.Prev.Payload) == string(payload)}
	if j.Event && res.Unchanged {
		res.Again = ConfirmAfter // the event found nothing new: GitHub's listing may be seconds behind it. One that found a change needs no second read
	}
	return res, nil
}

// build cleans the listing into a repository's issues.
func build(listed []Listed, exact bool) *Set {
	s := &Set{Total: len(listed), Exact: exact, Issues: make([]Issue, 0, len(listed))}
	for _, l := range listed {
		i := Issue{Number: l.Number, Title: untrusted.Text(l.Title, maxTitle), URL: l.URL, Author: untrusted.Text(l.Author, maxName),
			Bot: l.AuthorBot || actor.Bot(l.Author), Created: l.Created.UTC(), Updated: l.Updated.UTC(), Assigned: l.Assigned,
			Comments: l.Comments, Milestone: untrusted.Text(l.Milestone, maxName), Type: untrusted.Text(l.Type, untrusted.MaxLabel)}
		for _, lb := range l.Labels {
			i.Labels = append(i.Labels, untrusted.NewLabel(lb.Name, lb.Color))
		}
		for _, a := range l.Assignees[:min(len(l.Assignees), maxPeople)] {
			i.Assignees = append(i.Assignees, untrusted.Text(a, maxName))
		}
		s.Issues = append(s.Issues, i)
	}
	return s
}

func publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	var s *Set
	if it.State == snapshot.StateOK {
		s = &Set{}
		if json.Unmarshal(it.Payload, s) != nil {
			s = nil
		}
	}
	if s != nil { // automation told again at every publish, so a change of the rule reaches the stored ones at a warm start
		for i := range s.Issues {
			s.Issues[i].Bot = s.Issues[i].Bot || actor.Bot(s.Issues[i].Author)
		}
	}
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { facet.Set(e, s, fresh) })
}
