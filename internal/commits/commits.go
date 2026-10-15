// Package commits keeps the latest default-branch commits of every catalogued repository and serves the merged commit feeds.
package commits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

// Kind is the job kind of the per-repository refresh.
const Kind = "commits"

const (
	perRepo       = 30
	ttl           = 2 * time.Hour // safety net: pushes notify the job
	maxSubject    = 200           // runes
	maxAuthor     = 100           // runes
	futureClamp   = 24 * time.Hour
	maxParents    = 8 // an octopus merge beyond this keeps its first eight
	payloadSchema = 2
)

var (
	shaPattern   = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	loginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
)

// Commits is the commits feature.
type Commits struct {
	sources map[string]Fetcher
}

// New returns the feature for one Fetcher per source id.
func New(sources map[string]Fetcher) *Commits { return &Commits{sources: sources} }

type payload struct {
	Schema  int      `json:"schema"`
	Commits []Commit `json:"commits"`
}

// Spec is the per-repository refresh job.
func (c *Commits) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, On: []sync.Event{sync.EventPush}, Run: c.run, Publish: publish}
}

func (c *Commits) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	f := c.sources[j.Source]
	if f == nil {
		return sync.Result{}, errors.New("commits: unknown source " + j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	got, err := f.Commits(ctx, owner, name, perRepo)
	state := snapshot.StateOK
	var e *github.Error
	switch {
	case errors.As(err, &e) && e.Code == "empty":
		state, got = snapshot.StateNoContent, nil
	case github.IsNotFound(err):
		state, got = snapshot.StateNotFound, nil
	case err != nil:
		return sync.Result{}, err
	}
	prev := decode(j.Prev.Payload)
	b, err := json.Marshal(payload{Schema: payloadSchema, Commits: normalise(got, prev, time.Now())})
	if err != nil {
		return sync.Result{}, err
	}
	return sync.Result{State: state, Payload: b, Unchanged: j.Prev.State == state && bytes.Equal(b, j.Prev.Payload)}, nil
}

func decode(b []byte) []Commit {
	var p payload
	if len(b) == 0 || json.Unmarshal(b, &p) != nil || p.Schema != payloadSchema {
		return nil
	}
	return p.Commits
}

// normalise cleans GitHub's commits: valid SHAs only (parents too), one-line subject, bounded names, plausible dates, newest first, no duplicates.
// A date more than a day ahead is clamped to the time the commit was first seen, which prev remembers.
func normalise(in []CommitInfo, prev []Commit, now time.Time) []Commit {
	firstSeen := map[string]time.Time{}
	for _, c := range prev {
		firstSeen[c.SHA] = c.CommittedAt
	}
	seen := map[string]bool{}
	out := make([]Commit, 0, len(in))
	for _, c := range in {
		sha := strings.ToLower(c.SHA)
		if !shaPattern.MatchString(sha) || seen[sha] || c.Date.IsZero() {
			continue
		}
		seen[sha] = true
		at := c.Date.UTC().Truncate(time.Second)
		if at.After(now.Add(futureClamp)) {
			at = now.UTC().Truncate(time.Second)
			if t, ok := firstSeen[sha]; ok {
				at = t
			}
		}
		login := c.AuthorLogin
		if !loginPattern.MatchString(login) {
			login = ""
		}
		url := c.HTMLURL
		if !strings.HasPrefix(url, "https://") {
			url = ""
		}
		out = append(out, Commit{SHA: sha, Subject: untrusted.Text(untrusted.FirstLine(c.Message), maxSubject),
			AuthorName: untrusted.Text(c.AuthorName, maxAuthor), AuthorLogin: login, URL: url, CommittedAt: at, Parents: parents(c.Parents)})
	}
	slices.SortStableFunc(out, func(a, b Commit) int {
		if c := b.CommittedAt.Compare(a.CommittedAt); c != 0 {
			return c
		}
		return strings.Compare(b.SHA, a.SHA)
	})
	return out
}

// parents keeps the valid parent SHAs, lower-case, in order, without duplicates, at most maxParents; never nil.
func parents(in []string) []string {
	out := []string{}
	for _, p := range in {
		if p = strings.ToLower(p); shaPattern.MatchString(p) && !slices.Contains(out, p) && len(out) < maxParents {
			out = append(out, p)
		}
	}
	return out
}

func publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	cs := decode(it.Payload)
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { facet.Set(e, cs, fresh) })
}
