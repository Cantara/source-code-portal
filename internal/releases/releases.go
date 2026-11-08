// Package releases keeps every repository's recent releases (design/releases.md, P-82): the 20 newest published ones as a conditional
// REST listing, read when a release is published and every 6 hours as the net. The latest release (the newest stable one) that the badges, the
// repository summary and the search show is the first of this list, so a repository is read once.
package releases

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/actor"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

// Kind is the job kind that reads a repository's releases.
const Kind = "releases"

const (
	// Keep is how many of a repository's newest releases are kept.
	Keep = 20
	// ttl is the safety net: a release is published by a person, rarely, and the webhook is the way it is noticed (P-80), with the App's
	// delivery log read every two minutes for one that was missed. A shorter TTL makes a cold start read every repository again before it
	// has read the last one.
	ttl = 6 * time.Hour
	// pollTTL is the cadence for a source nothing tells about a new release (no webhook, no delivery log): conditional requests, a 304
	// costs nothing against GitHub's limits.
	pollTTL = 30 * time.Minute
	// ConfirmAfter is when a run caused by an event reads again: GitHub's listing can be a few seconds behind the event.
	ConfirmAfter = 30 * time.Second
	maxName      = 200
)

// Events are the deliveries that re-read a repository's releases: a published release is a tag event (webhook.Normalise).
var Events = []sync.Event{sync.EventTag} //nolint:gochecknoglobals // a constant list

// Release is a published release. Drafts are never kept.
type Release struct {
	Tag       string    `json:"tag"`
	Name      string    `json:"name,omitempty"`
	Summary   string    `json:"summary,omitempty"` // the first line of its notes, cleaned
	URL       string    `json:"url"`
	Published time.Time `json:"published"`
	Pre       bool      `json:"pre,omitempty"`
	Author    string    `json:"author,omitempty"`
	Bot       bool      `json:"bot,omitempty"`
	Assets    int       `json:"assets,omitempty"`
	Notes     string    `json:"notes,omitempty"` // the release's own notes (Markdown), cut at maxNotes
	NotesCut  bool      `json:"notesCut,omitempty"`
}

// Set is a repository's recent releases, the newest first.
type Set struct {
	Releases  []Release `json:"releases"`
	Changelog []Section `json:"changelog,omitempty"` // the newest versions of the repository's changelog (design/releases.md, RL5)
}

// Latest is the newest stable release: nil when there is none.
func (s *Set) Latest() *Release {
	if s == nil {
		return nil
	}
	for i := range s.Releases {
		if !s.Releases[i].Pre {
			return &s.Releases[i]
		}
	}
	return nil
}

const facet snapshot.Facet[*Set] = "releases" // nil when not read yet

// Of is a repository's recent releases (nil when not read yet) with their freshness.
func Of(e *snapshot.RepoEntry) (*Set, snapshot.Freshness) { return facet.Get(e) }

// Latest is a repository's latest stable release (nil when there is none or it is not known yet) with its freshness. A repository read
// to have no stable release is not found, as the badges say none for it.
func Latest(e *snapshot.RepoEntry) (*Release, snapshot.Freshness) {
	s, fresh := facet.Get(e)
	rel := s.Latest()
	if rel == nil && s != nil && fresh.State == snapshot.StateOK {
		fresh.State = snapshot.StateNotFound
	}
	return rel, fresh
}

// Repo adds the latest release to a repository summary.
func Repo(_ *snapshot.Snapshot, e *snapshot.RepoEntry, r *oapi.Repo) {
	if rel, _ := Latest(e); rel != nil {
		r.Release = &oapi.Release{Tag: rel.Tag, Url: rel.URL, PublishedAt: rel.Published.UTC()}
	}
}

// Fetcher reads a repository's releases, the newest first, and its changelog; none is not an error.
type Fetcher interface {
	Releases(ctx context.Context, owner, name string) ([]Release, error)
	// Changelog is the text of the changelog at the repository's root, nil when it has none.
	Changelog(ctx context.Context, owner, name string) ([]byte, error)
}

// Client is a source's GitHub client; *github.Source implements it.
type Client interface {
	REST(ctx context.Context, owner string) (*gh.Client, error)
	Tree(ctx context.Context, owner, name, ref string, recursive bool) ([]github.TreeEntry, error)
	Blob(ctx context.Context, owner, name, sha string) ([]byte, error)
}

// FromGitHub reads releases through src.
func FromGitHub(src Client) Fetcher { return rest{src} }

type rest struct{ src Client }

// Releases lists the Keep newest published releases; drafts and releases without a publication time are left out.
func (r rest) Releases(ctx context.Context, owner, name string) ([]Release, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, err
	}
	list, resp, err := c.Repositories.ListReleases(ctx, owner, name, &gh.ListOptions{PerPage: Keep})
	if err != nil {
		if err := github.Classify(resp, err); !github.IsNotFound(err) {
			return nil, err
		}
		return nil, nil
	}
	out := make([]Release, 0, len(list))
	for _, x := range list {
		if x.GetDraft() || x.GetPublishedAt().IsZero() {
			continue
		}
		out = append(out, Release{Tag: x.GetTagName(), Name: x.GetName(), Summary: firstLine(x.GetBody()), Notes: x.GetBody(), URL: x.GetHTMLURL(),
			Published: x.GetPublishedAt().UTC(), Pre: x.GetPrerelease(), Author: x.GetAuthor().GetLogin(),
			Bot: x.GetAuthor().GetType() == "Bot", Assets: len(x.Assets)})
	}
	return out, nil
}

// Changelog reads the file at the root of the default branch that holds the changelog, the first of its name in tree order.
func (r rest) Changelog(ctx context.Context, owner, name string) ([]byte, error) {
	top, err := r.src.Tree(ctx, owner, name, "HEAD", false)
	if err != nil {
		if github.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range top {
		if e.Type == "blob" && e.Size <= maxChangelog && changelogName.MatchString(e.Path) {
			b, err := r.src.Blob(ctx, owner, name, e.SHA)
			if github.IsNotFound(err) {
				return nil, nil
			}
			return b, err
		}
	}
	return nil, nil
}

// firstLine is the first line of a release's notes that says something, before it is cleaned.
func firstLine(body string) string {
	for l := range strings.Lines(body) {
		if t := strings.TrimLeft(strings.TrimSpace(l), "#*-> "); t != "" {
			return t
		}
	}
	return ""
}

// Releases is the releases feature.
type Releases struct {
	sources map[string]Fetcher
}

// New returns the feature for one Fetcher per source id.
func New(sources map[string]Fetcher) *Releases { return &Releases{sources: sources} }

// Spec is the per-repository releases job.
func (f *Releases) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, PollTTL: pollTTL, On: Events, Run: f.run, Publish: publish}
}

func (f *Releases) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	src := f.sources[j.Source]
	if src == nil {
		return sync.Result{}, errors.New("releases: unknown source " + j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	listed, err := src.Releases(ctx, owner, name)
	if err != nil {
		return sync.Result{}, err
	}
	text, err := src.Changelog(ctx, owner, name)
	if err != nil {
		return sync.Result{}, err
	}
	set := build(listed)
	set.Changelog = sections(string(text))
	payload, err := json.Marshal(set)
	if err != nil {
		return sync.Result{}, err
	}
	res := sync.Result{Payload: payload, Unchanged: j.Prev.State == snapshot.StateOK && string(j.Prev.Payload) == string(payload)}
	if j.Event && res.Unchanged {
		res.Again = ConfirmAfter // the event found nothing new: GitHub's listing may be seconds behind it
	}
	return res, nil
}

// build cleans the listing into a repository's releases, the newest first, at most Keep.
func build(listed []Release) *Set {
	s := &Set{Releases: make([]Release, 0, min(len(listed), Keep))}
	for _, l := range listed {
		l.Name, l.Summary = untrusted.Text(l.Name, maxName), untrusted.Text(l.Summary, maxName)
		l.Tag, l.Author = untrusted.Text(l.Tag, maxName), untrusted.Text(l.Author, maxName)
		l.Notes, l.NotesCut = cut(l.Notes, maxNotes)
		l.Bot = l.Bot || actor.Bot(l.Author)
		l.Published = l.Published.UTC()
		s.Releases = append(s.Releases, l)
	}
	slices.SortStableFunc(s.Releases, func(x, y Release) int { return y.Published.Compare(x.Published) })
	if len(s.Releases) > Keep {
		s.Releases = s.Releases[:Keep]
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
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { facet.Set(e, s, fresh) })
}
