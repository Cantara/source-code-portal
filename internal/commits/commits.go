// Package commits keeps the latest default-branch commits of every catalogued repository and serves the merged commit feeds.
package commits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
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
	maxMain       = 500              // default-branch commits kept
	maxBranch     = 100              // commits read per other branch
	maxBranches   = 10               // other branches kept, the newest heads
	fullEvery     = 24 * time.Hour   // a full read drops what a force push rewrote
	cutAgain      = 10 * time.Minute // a read cut by a failure runs again, in full, after this
	ttl           = 2 * time.Hour    // safety net: pushes notify the job
	maxSubject    = 200              // runes
	maxAuthor     = 100              // runes
	maxBody       = 4096             // runes
	maxLabel      = 100              // runes of a branch or tag name
	futureClamp   = 24 * time.Hour
	maxParents    = 8 // an octopus merge beyond this keeps its first eight
	payloadSchema = 3
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

// payload is the stored history of a repository.
type payload struct {
	Schema int       `json:"schema"`
	Main   []Commit  `json:"main"`   // the default branch, newest first
	Side   []Commit  `json:"side"`   // commits only on other branches, newest first
	Labels []Label   `json:"labels"` // branches and tags at commits of the history
	Full   time.Time `json:"full"`   // the last read that reused nothing
}

// Spec is the per-repository refresh job.
func (c *Commits) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, On: []sync.Event{sync.EventPush}, Run: c.run, Publish: publish}
}

// automationBranch reports whether a branch is a bot's update branch: those come and go with every dependency update.
func automationBranch(name string) bool {
	for _, p := range []string{"renovate/", "dependabot/", "snyk-fix-", "snyk-upgrade-"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func (c *Commits) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	f := c.sources[j.Source]
	if f == nil {
		return sync.Result{}, errors.New("commits: unknown source " + j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	now := time.Now()
	prev := decode(j.Prev.Payload)
	full := prev == nil || now.Sub(prev.Full) > fullEvery
	if prev == nil {
		prev = &payload{}
	}
	out := payload{Schema: payloadSchema, Full: prev.Full, Labels: []Label{}}
	if full {
		out.Full = now.UTC().Truncate(time.Second)
	}
	known := map[string]bool{}
	if !full {
		for _, c := range prev.Main {
			known[c.SHA] = true
		}
	}
	read, err := line(ctx, f, owner, name, "", known, maxMain)
	state, cut := snapshot.StateOK, false
	var e *github.Error
	switch {
	case errors.As(err, &e) && e.Code == "empty":
		state, read = snapshot.StateNoContent, nil
	case github.IsNotFound(err):
		state, read = snapshot.StateNotFound, nil
	case err != nil && (len(read) == 0 || !keeps(err)):
		return sync.Result{}, err
	case err != nil:
		cut = true // the pages read before the failure are kept with the history held
	}
	old := slices.Concat(prev.Main, prev.Side)
	main := normalise(read, old, now)
	if !full || cut {
		main = union(main, prev.Main)
	}
	out.Main = main[:min(len(main), maxMain)]
	if state == snapshot.StateOK {
		var sideCut bool
		if out.Side, out.Labels, sideCut, err = sides(ctx, f, owner, name, out.Main, prev, full, now); err != nil {
			return sync.Result{}, err
		}
		cut = cut || sideCut
	}
	res := sync.Result{State: state}
	if cut {
		// What a failure cut is read again soon and in full, so no gap stays between what was read and what was held.
		out.Full, res.Again = time.Time{}, cutAgain
	}
	if res.Payload, err = json.Marshal(out); err != nil {
		return sync.Result{}, err
	}
	res.Unchanged = j.Prev.State == state && bytes.Equal(res.Payload, j.Prev.Payload)
	return res, nil
}

// keeps reports whether a read that failed with err keeps what it read: every failure but a pause of the source (a rate limit, its
// credentials) and the run's own end, which the engine handles and which leave the stored history as it was.
func keeps(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var e *github.Error
	return !errors.As(err, &e) || e.Kind != github.RateLimited && e.Kind != github.Auth
}

// line reads the commits reachable from ref, page by page, up to limit new ones. It stops early once a page reaches a commit in known
// and every parent of the new commits is new or known: the rest of the line is held already. On a failure it returns the commits of
// the pages read before it with the error.
func line(ctx context.Context, f Fetcher, owner, name, ref string, known map[string]bool, limit int) ([]CommitInfo, error) {
	var out []CommitInfo
	seen := map[string]bool{}
	for page := 1; len(out) < limit; page++ {
		cs, more, err := f.Commits(ctx, owner, name, ref, page)
		if err != nil {
			return out, err
		}
		overlap := false
		for _, c := range cs {
			sha := strings.ToLower(c.SHA)
			switch {
			case known[sha]:
				overlap = true
			case !seen[sha]:
				seen[sha] = true
				out = append(out, c)
			}
		}
		if !more || overlap && closed(out, known, seen) {
			break
		}
	}
	return out[:min(len(out), limit)], nil
}

// closed reports whether every parent of cs is in known or seen.
func closed(cs []CommitInfo, known, seen map[string]bool) bool {
	for _, c := range cs {
		for _, p := range c.Parents {
			if p = strings.ToLower(p); !known[p] && !seen[p] {
				return false
			}
		}
	}
	return true
}

// union is a and the commits of b not in a, newest first.
func union(a, b []Commit) []Commit {
	in := map[string]bool{}
	for _, c := range a {
		in[c.SHA] = true
	}
	out := slices.Clone(a)
	for _, c := range b {
		if !in[c.SHA] {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, newestFirst)
	return out
}

func newestFirst(a, b Commit) int {
	if c := b.CommittedAt.Compare(a.CommittedAt); c != 0 {
		return c
	}
	return strings.Compare(b.SHA, a.SHA)
}

// sides reads the other branches and the tags: the commits of the branches whose heads are not on the default branch (the newest
// maxBranches heads, bots' update branches left out), and a label for every branch and tag at a commit of the history. A failure
// that keeps (keeps) never costs the default branch: without the branches the held side commits stay, a branch that fails keeps
// what was read of it, and cut says so.
func sides(ctx context.Context, f Fetcher, owner, name string, main []Commit, prev *payload, full bool, now time.Time) ([]Commit, []Label, bool, error) {
	branches, err := f.Branches(ctx, owner, name)
	if err != nil {
		if !keeps(err) {
			return nil, nil, false, err
		}
		return held(prev.Side, main), heldLabels(prev.Labels, main, prev.Side), true, nil
	}
	cut := false
	tags, err := f.Tags(ctx, owner, name)
	if err != nil {
		if !keeps(err) {
			return nil, nil, false, err
		}
		tags, cut = nil, true
	}
	onMain := map[string]bool{}
	for _, c := range main {
		onMain[c.SHA] = true
	}
	held := map[string]Commit{}
	if !full {
		for _, c := range prev.Side {
			held[c.SHA] = c
		}
	}
	type head struct {
		name, sha string
		at        time.Time
	}
	var heads []head
	reads := 0
	for _, b := range branches {
		sha := strings.ToLower(b.SHA)
		if onMain[sha] || automationBranch(b.Name) || !shaPattern.MatchString(sha) {
			continue
		}
		if _, ok := held[sha]; !ok {
			if reads == maxBranches {
				continue
			}
			reads++
			known := maps.Clone(onMain)
			for s := range held {
				known[s] = true
			}
			read, err := line(ctx, f, owner, name, b.Name, known, maxBranch)
			if err != nil {
				if !keeps(err) {
					return nil, nil, false, err
				}
				cut = true
			}
			for _, c := range normalise(read, prev.Side, now) {
				held[c.SHA] = c
			}
		}
		if c, ok := held[sha]; ok {
			heads = append(heads, head{b.Name, sha, c.CommittedAt})
		}
	}
	slices.SortStableFunc(heads, func(a, b head) int { return b.at.Compare(a.at) })
	heads = heads[:min(len(heads), maxBranches)]
	// The side commits are those reachable from the kept heads without reaching the default branch.
	var side []Commit
	seen := map[string]bool{}
	for _, h := range heads {
		for stack := []string{h.sha}; len(stack) > 0; {
			sha := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			c, ok := held[sha]
			if !ok || seen[sha] {
				continue
			}
			seen[sha] = true
			side = append(side, c)
			stack = append(stack, c.Parents...)
		}
	}
	slices.SortStableFunc(side, newestFirst)
	labels := []Label{}
	kept := map[string]bool{}
	for _, h := range heads {
		kept[h.name] = true
	}
	for _, b := range branches {
		if sha := strings.ToLower(b.SHA); onMain[sha] || kept[b.Name] {
			labels = append(labels, Label{Name: untrusted.Text(b.Name, maxLabel), Kind: "branch", SHA: sha})
		}
	}
	for _, t := range tags {
		if sha := strings.ToLower(t.SHA); onMain[sha] || seen[sha] {
			labels = append(labels, Label{Name: untrusted.Text(t.Name, maxLabel), Kind: "tag", SHA: sha})
		}
	}
	return side, labels, cut, nil
}

// held is the side commits of the last read that are not on main now.
func held(side, main []Commit) []Commit {
	on := map[string]bool{}
	for _, c := range main {
		on[c.SHA] = true
	}
	out := []Commit{}
	for _, c := range side {
		if !on[c.SHA] {
			out = append(out, c)
		}
	}
	return out
}

// heldLabels is the labels of the last read that still point at a commit of main or side.
func heldLabels(labels []Label, main, side []Commit) []Label {
	at := map[string]bool{}
	for _, c := range slices.Concat(main, side) {
		at[c.SHA] = true
	}
	out := []Label{}
	for _, l := range labels {
		if at[l.SHA] {
			out = append(out, l)
		}
	}
	return out
}

func decode(b []byte) *payload {
	var p payload
	if len(b) == 0 || json.Unmarshal(b, &p) != nil || p.Schema != payloadSchema {
		return nil
	}
	return &p
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
		out = append(out, Commit{SHA: sha, Subject: untrusted.Text(untrusted.FirstLine(c.Message), maxSubject), Body: body(c.Message),
			AuthorName: untrusted.Text(c.AuthorName, maxAuthor), AuthorLogin: login, CommitterName: untrusted.Text(c.CommitterName, maxAuthor),
			URL: url, CommittedAt: at, Parents: parents(c.Parents)})
	}
	slices.SortStableFunc(out, newestFirst)
	return out
}

// body is the message after its subject line, cleaned and bounded.
func body(msg string) string {
	msg = strings.TrimLeft(msg, "\r\n\t ")
	_, rest, _ := strings.Cut(msg, "\n")
	rest = strings.TrimSpace(untrusted.Strip(rest))
	r := []rune(rest)
	if len(r) <= maxBody {
		return rest
	}
	// The trailers (Co-authored-by and the like) close the message and say who made it: a long body keeps them.
	tail := trailers(rest)
	if tail == "" {
		return string(r[:maxBody-1]) + "…"
	}
	return string(r[:maxBody-len([]rune(tail))-3]) + "…\n\n" + tail
}

// maxTrailers bounds the closing paragraph a cut body keeps.
const maxTrailers = 1024 // runes

// trailers is the last paragraph of a message when it is made of trailers ("Key: value" lines, an AI tool's footer among them), at
// most maxTrailers runes; else "".
func trailers(msg string) string {
	i := strings.LastIndex(msg, "\n\n")
	if i < 0 {
		return ""
	}
	last := strings.TrimSpace(msg[i:])
	if last == "" || len([]rune(last)) > maxTrailers {
		return ""
	}
	for line := range strings.Lines(last) {
		if strings.Contains(strings.ToLower(line), "generated with") {
			continue
		}
		if key, _, ok := strings.Cut(strings.TrimSpace(line), ": "); !ok || key == "" || strings.ContainsAny(key, " \t") {
			return ""
		}
	}
	return last
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
	p := decode(it.Payload)
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) {
		var h *History
		if p != nil {
			h = newHistory(p.Main, p.Side, p.Labels, e.Repo.DefaultBranch)
		}
		facet.Set(e, h, fresh)
	})
}
