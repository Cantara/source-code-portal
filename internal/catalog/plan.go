package catalog

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

const (
	maxAliases     = 8
	missesToRemove = 2
	maxDescription = 1000 // runes
	maxTopics      = 20
)

// Tracked is a catalogued repository with the addresses it had before and how many complete listings in a row missed it.
type Tracked struct {
	Repo    snapshot.Repo      `json:"repo"`
	Aliases []snapshot.RepoKey `json:"aliases,omitempty"` // newest first
	Misses  int                `json:"misses,omitempty"`
}

// Filter is a source's admission rule.
type Filter struct {
	IncludeForks, IncludeArchived, IncludePrivate bool
	exclude                                       []matcher
}

// SourceFilter is the admission rule of a configured source.
func SourceFilter(s config.Source) (Filter, error) {
	f := Filter{IncludeForks: s.IncludeForks, IncludeArchived: s.IncludeArchived, IncludePrivate: s.IncludePrivate}
	for i, m := range s.Exclude {
		kind, value, _ := m.Kind() // one key: checked by config
		fn, err := compileMatcher(kind, value)
		if err != nil {
			return Filter{}, fmt.Errorf("github.sources (%s).exclude[%d]: %w", s.ID, i, err)
		}
		f.exclude = append(f.exclude, fn)
	}
	return f, nil
}

// Plan returns the source's catalogue after a complete listing: listed repositories that pass the filter are tracked (renames keep
// the old address as an alias), tracked ones that turn private or fail the filter are dropped, and tracked ones the listing missed
// stay until the second miss in a row. The result is sorted by address and does not depend on the listing order.
func Plan(listed []snapshot.Repo, current []Tracked, f Filter) []Tracked {
	byID := make(map[int64]Tracked, len(current))
	for _, t := range current {
		byID[t.Repo.ID.GitHubID] = t
	}
	seen := map[int64]bool{}
	listedAt := map[snapshot.RepoKey]bool{}
	var out []Tracked
	for _, r := range sortedByID(listed) {
		id := r.ID.GitHubID
		if seen[id] {
			continue
		}
		seen[id] = true
		r, ok := admit(r, f)
		if !ok || listedAt[r.Key()] {
			continue // not admitted, or the address is already taken by a lower id
		}
		listedAt[r.Key()] = true
		t := Tracked{Repo: r}
		if prev, was := byID[id]; was {
			t.Aliases = prev.Aliases
			if old := prev.Repo.Key(); old != r.Key() {
				t.Aliases = append([]snapshot.RepoKey{old}, t.Aliases...)
			}
		}
		out = append(out, t)
	}
	for _, t := range current {
		if seen[t.Repo.ID.GitHubID] || listedAt[t.Repo.Key()] {
			continue // listed (kept or dropped above), or its address now belongs to a listed repository
		}
		if t.Misses+1 < missesToRemove {
			t.Misses++
			out = append(out, t)
		}
	}
	for i := range out {
		out[i].Aliases = cleanAliases(out[i].Aliases, listedAt, out[i].Repo.Key())
	}
	slices.SortFunc(out, func(a, b Tracked) int { return cmp.Compare(a.Repo.Key(), b.Repo.Key()) })
	return out
}

func sortedByID(rs []snapshot.Repo) []snapshot.Repo {
	rs = slices.Clone(rs)
	slices.SortStableFunc(rs, func(a, b snapshot.Repo) int { return cmp.Compare(a.ID.GitHubID, b.ID.GitHubID) })
	return rs
}

// cleanAliases keeps at most maxAliases distinct old addresses that are neither current nor another listed repository's address.
func cleanAliases(as []snapshot.RepoKey, taken map[snapshot.RepoKey]bool, current snapshot.RepoKey) []snapshot.RepoKey {
	var out []snapshot.RepoKey
	for _, a := range as {
		if a != current && !taken[a] && !slices.Contains(out, a) && len(out) < maxAliases {
			out = append(out, a)
		}
	}
	return out
}

// Visible reports whether a repository of visibility passes the filter: public always, private and internal with IncludePrivate.
func (f Filter) Visible(visibility string) bool {
	return visibility == "public" || f.IncludePrivate && (visibility == "private" || visibility == "internal")
}

// admit validates and cleans a listed repository and applies the filter.
func admit(r snapshot.Repo, f Filter) (snapshot.Repo, bool) {
	switch {
	case r.ID.GitHubID == 0, !namePattern.MatchString(r.Owner), !namePattern.MatchString(r.Name),
		r.Name == "." || r.Name == "..", !f.Visible(r.Visibility),
		r.Archived && !f.IncludeArchived, r.Fork && !f.IncludeForks,
		slices.ContainsFunc(f.exclude, func(m matcher) bool { return m(&r) }):
		return r, false
	}
	r.Description = untrusted.Text(r.Description, maxDescription)
	var topics []string
	for _, t := range r.Topics {
		if topicPattern.MatchString(t) && len(topics) < maxTopics {
			topics = append(topics, t)
		}
	}
	r.Topics = topics
	r.Homepage = httpsOnly(r.Homepage)
	r.HTMLURL = httpsOnly(r.HTMLURL)
	return r, true
}

func httpsOnly(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return s
}
