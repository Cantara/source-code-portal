// Package pulls keeps every repository's open pull requests and says for each whose turn it is (design/pulls.md, P-68).
package pulls

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/actor"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

// Kind is the job kind that reads a repository's open pull requests.
const Kind = "pulls"

const (
	ttl       = 15 * time.Minute // webhooks notify the job; the listing is a free 304 when nothing changed
	fullEvery = time.Hour        // checks change without touching the listing: every pull request is asked again this often
	eventGap  = time.Minute      // anyone may open a pull request or review one on a public repository: one read a minute at most
	// ConfirmAfter is when a run caused by an event reads again: GitHub's listing can be a few seconds behind the event.
	ConfirmAfter = 30 * time.Second
	failAgain    = 10 * time.Minute // a failed GraphQL query is asked again this soon
	maxTitle     = 200
)

// pendingSteps are the waits before a pull request whose checks run or whose mergeability GitHub is still computing is asked again:
// a required check that never reports cannot keep a repository at a query every 5 minutes.
var pendingSteps = []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, time.Hour} //nolint:gochecknoglobals // a constant table

// Events are the webhook deliveries that re-read a repository's pull requests (webhook.Normalise passes them by their names).
// Not check_suite or status: they fire on every CI update of every commit; running checks are asked again on their own steps.
var Events = []sync.Event{sync.EventPush, sync.EventOpen, "pull_request", "pull_request_review"} //nolint:gochecknoglobals // a constant list

// Label is a pull request's label.
type Label = untrusted.Label

// Stats is what a pull request changes.
type Stats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Files     int `json:"files"`
}

// Pull is an open pull request: the REST listing's fields, and GraphQL's from when it was asked (Asked zero: never).
type Pull struct {
	Number    int       `json:"n"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	Bot       bool      `json:"bot,omitempty"`
	Draft     bool      `json:"draft,omitempty"`
	Fork      bool      `json:"fork,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Base      string    `json:"base"`
	Head      string    `json:"head"`
	HeadSHA   string    `json:"sha"`
	Labels    []Label   `json:"labels,omitempty"`
	Requested int       `json:"requested,omitempty"`
	Reviewers []string  `json:"reviewers,omitempty"` // the first maxReviewers people asked, by login (teams are only counted)
	Review    string    `json:"review,omitempty"`
	Approved  bool      `json:"approved,omitempty"`
	Changes   bool      `json:"changes,omitempty"`
	Checks    string    `json:"checks,omitempty"`
	Mergeable string    `json:"mergeable,omitempty"`
	Stats     *Stats    `json:"stats,omitempty"`
	Asked     time.Time `json:"asked,omitzero"`
}

// Set is a repository's open pull requests, the most recently updated first.
type Set struct {
	Total     int       `json:"total"` // open on GitHub: len(Pulls) unless there are more than 100
	Pulls     []Pull    `json:"pulls"`
	Full      time.Time `json:"full,omitzero"`      // when every pull request was last asked
	Pending   int       `json:"pending,omitempty"`  // the step of pendingSteps the waiting ones are at
	PendingAt time.Time `json:"pendingAt,omitzero"` // when the waiting ones were last asked
}

// State is whose turn a pull request is, the first that holds.
type State string

const (
	StateDraft     State = "draft"     // the author: still a draft
	StateConflicts State = "conflicts" // the author: it does not merge
	StateFailing   State = "failing"   // the author: its checks fail
	StateChanges   State = "changes"   // the author: a reviewer asked for changes
	StateReview    State = "review"    // reviewers: a review is required, or none approved yet
	StateChecks    State = "checks"    // CI: its checks run
	StateReady     State = "ready"     // a maintainer: approved or no review required, checks pass
	StateUnknown   State = "unknown"   // not asked yet
)

// States are the states in the order whose turn is told.
var States = []State{StateDraft, StateConflicts, StateFailing, StateChanges, StateReview, StateChecks, StateReady, StateUnknown} //nolint:gochecknoglobals // a constant list

// Waits is the party a state waits for.
func (s State) Waits() string {
	switch s {
	case StateDraft, StateConflicts, StateFailing, StateChanges:
		return "author"
	case StateReview:
		return "reviewers"
	case StateChecks:
		return "ci"
	case StateReady:
		return "maintainer"
	}
	return "unknown"
}

// StateOf is whose turn p is.
func StateOf(p *Pull) State {
	switch {
	case p.Draft:
		return StateDraft
	case p.Asked.IsZero():
		return StateUnknown
	case p.Mergeable == "CONFLICTING":
		return StateConflicts
	case p.Checks == "FAILURE" || p.Checks == "ERROR":
		return StateFailing
	case p.Review == "CHANGES_REQUESTED" || p.Review == "" && p.Changes:
		return StateChanges
	case p.Review == "REVIEW_REQUIRED" || p.Review == "" && !p.Approved:
		return StateReview
	case p.Checks == "PENDING" || p.Checks == "EXPECTED":
		return StateChecks
	}
	return StateReady
}

// waiting reports whether GitHub has not settled p yet: its checks run or its mergeability is being computed.
func waiting(p *Pull) bool {
	return !p.Draft && (p.Checks == "PENDING" || p.Checks == "EXPECTED" || p.Mergeable == "UNKNOWN")
}

const facet snapshot.Facet[*Set] = "pulls" // nil when not read yet

// Of is a repository's open pull requests (nil when not read yet) with their freshness.
func Of(e *snapshot.RepoEntry) (*Set, snapshot.Freshness) { return facet.Get(e) }

// Options are what the job reads of the catalogue.
type Options struct {
	// Repo is the catalogued repository at a key: an archived one is read once.
	Repo func(snapshot.RepoKey) (snapshot.Repo, bool)
	Now  func() time.Time
}

// Pulls is the pull requests feature.
type Pulls struct {
	sources map[string]Fetcher
	o       Options
}

// New returns the feature for one Fetcher per source id.
func New(sources map[string]Fetcher, o Options) *Pulls {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Pulls{sources: sources, o: o}
}

// Spec is the per-repository pull requests job.
func (p *Pulls) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, On: Events, Settle: eventGap, Run: p.run, Publish: publish}
}

func (p *Pulls) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	src := p.sources[j.Source]
	if src == nil {
		return sync.Result{}, errors.New("pulls: unknown source " + j.Source)
	}
	var prev *Set
	if j.Prev.State == snapshot.StateOK {
		prev = &Set{}
		if json.Unmarshal(j.Prev.Payload, prev) != nil {
			prev = nil
		}
	}
	if r, ok := p.o.Repo(j.Repo); ok && r.Archived && prev != nil {
		return sync.Result{Unchanged: true}, nil // an archived repository's pull requests cannot change
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	listed, err := src.Open(ctx, owner, name)
	if err != nil {
		return sync.Result{}, err
	}
	now := p.o.Now()
	next, ask := merge(prev, listed, now)
	var again time.Duration
	if len(ask) > 0 || len(listed) == maxAsk {
		got, total, err := src.Details(ctx, owner, name, ask, len(listed) == maxAsk)
		if err != nil {
			// GraphQL is its own budget: a failed query keeps what was known and asks again soon, never pausing the source.
			if prev != nil {
				next.Full, next.Pending, next.PendingAt = prev.Full, prev.Pending, prev.PendingAt
			}
			again = failAgain
		} else {
			apply(next, got, ask, total, now)
		}
	}
	if d := settle(next, ask, now); d > 0 && (again == 0 || d < again) {
		again = d
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return sync.Result{}, err
	}
	unchanged := j.Prev.State == snapshot.StateOK && string(j.Prev.Payload) == string(payload)
	if j.Event && unchanged && (again == 0 || ConfirmAfter < again) {
		again = ConfirmAfter // the event found nothing new: GitHub's listing may be seconds behind it. One that found a change needs no second read
	}
	return sync.Result{Payload: payload, Again: again,
		Unchanged: unchanged}, nil
}

// merge builds the repository's pull requests from the listing, carrying GraphQL's fields over from prev, and returns the numbers
// to ask: new or changed ones (another update time or head), every one when the last full read is an hour old, and the ones GitHub
// had not settled when their step of pendingSteps is due.
func merge(prev *Set, listed []Listed, now time.Time) (*Set, []int) {
	old := map[int]*Pull{}
	full := true
	if prev != nil {
		for i := range prev.Pulls {
			old[prev.Pulls[i].Number] = &prev.Pulls[i]
		}
		full = now.Sub(prev.Full) >= fullEvery
	}
	pendingDue := prev == nil || now.Sub(prev.PendingAt) >= pendingSteps[min(prev.Pending, len(pendingSteps)-1)]
	next := &Set{Total: len(listed), Pulls: make([]Pull, 0, len(listed))}
	if prev != nil {
		next.Full, next.Pending, next.PendingAt = prev.Full, prev.Pending, prev.PendingAt
	}
	var ask []int
	for _, l := range listed {
		p := Pull{Number: l.Number, Title: untrusted.Text(l.Title, maxTitle), URL: l.URL, Author: untrusted.Text(l.Author, 100),
			Bot: l.AuthorBot || actor.Bot(l.Author) || actor.Branch(l.Head), Draft: l.Draft, Fork: l.Fork, Created: l.Created.UTC(), Updated: l.Updated.UTC(),
			Base: untrusted.Text(l.Base, 100), Head: untrusted.Text(l.Head, 100), HeadSHA: l.HeadSHA, Requested: l.Requested,
			Reviewers: cleanLogins(l.Reviewers)}
		for _, lb := range l.Labels {
			p.Labels = append(p.Labels, untrusted.NewLabel(lb.Name, lb.Color))
		}
		o := old[p.Number]
		if o != nil {
			p.Review, p.Approved, p.Changes, p.Checks, p.Mergeable, p.Stats, p.Asked = o.Review, o.Approved, o.Changes, o.Checks, o.Mergeable, o.Stats, o.Asked
		}
		switch {
		case full, o == nil, o.Asked.IsZero(), !o.Updated.Equal(p.Updated), o.HeadSHA != p.HeadSHA, waiting(&p) && pendingDue:
			ask = append(ask, p.Number)
		}
		next.Pulls = append(next.Pulls, p)
	}
	if full {
		next.Full = now
	}
	return next, ask
}

// apply puts GraphQL's answers into r; total, when known, is the open pull requests on GitHub.
func apply(r *Set, got map[int]Detail, asked []int, total int, now time.Time) {
	if total >= 0 {
		r.Total = max(total, len(r.Pulls))
	}
	for i := range r.Pulls {
		p := &r.Pulls[i]
		if !slices.Contains(asked, p.Number) {
			continue
		}
		d, ok := got[p.Number]
		if !ok {
			continue // closed between the listing and the query
		}
		p.Review, p.Approved, p.Changes, p.Checks, p.Mergeable, p.Asked = d.Review, d.Approved, d.Changes, d.Checks, d.Mergeable, now
		s := d.Stats
		p.Stats = &s
	}
}

// settle moves the backoff of the pull requests GitHub had not settled and returns when to run again for them (0: none waits).
func settle(r *Set, asked []int, now time.Time) time.Duration {
	waits, askedWaiting := false, false
	for i := range r.Pulls {
		if waiting(&r.Pulls[i]) {
			waits = true
			askedWaiting = askedWaiting || slices.Contains(asked, r.Pulls[i].Number)
		}
	}
	if !waits {
		r.Pending, r.PendingAt = 0, time.Time{}
		return 0
	}
	if askedWaiting {
		if !r.PendingAt.IsZero() {
			r.Pending = min(r.Pending+1, len(pendingSteps)-1)
		}
		r.PendingAt = now
	}
	return max(r.PendingAt.Add(pendingSteps[r.Pending]).Sub(now), time.Minute)
}

func publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	var r *Set
	if it.State == snapshot.StateOK {
		r = &Set{}
		if json.Unmarshal(it.Payload, r) != nil {
			r = nil
		}
	}
	if r != nil { // automation told again at every publish, so a change of the rule reaches the stored ones at a warm start
		for i := range r.Pulls {
			p := &r.Pulls[i]
			p.Bot = p.Bot || actor.Bot(p.Author) || actor.Branch(p.Head)
		}
	}
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { facet.Set(e, r, fresh) })
}

// cleanLogins cleans the logins of the people asked to review, as an author's is.
func cleanLogins(in []string) []string {
	var out []string
	for _, l := range in {
		if l = untrusted.Text(l, 64); l != "" {
			out = append(out, l)
		}
	}
	return out
}
