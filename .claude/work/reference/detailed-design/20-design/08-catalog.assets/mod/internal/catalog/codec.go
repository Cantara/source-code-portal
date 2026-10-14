package catalog

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// repoMeta is the JSON in store.Repo.Meta. schema is bumped only for incompatible changes; unknown fields are ignored on read.
type repoMeta struct {
	Schema        int       `json:"schema"`
	Owner         string    `json:"owner"` // display case; the store key carries the folded form (store.KeyOf)
	Name          string    `json:"name"`  // display case: regexp matchers are case-sensitive and the UI shows it
	Description   string    `json:"description,omitempty"`
	DefaultBranch string    `json:"default_branch,omitempty"` // GitHub's, as listed
	Topics        []string  `json:"topics,omitempty"`
	Visibility    string    `json:"visibility"` // public, internal, private
	Archived      bool      `json:"archived,omitempty"`
	Fork          bool      `json:"fork,omitempty"`
	HTMLURL       string    `json:"html_url,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitzero"`
	PushedAt      time.Time `json:"pushed_at,omitzero"`
	Aliases       []string  `json:"aliases,omitempty"`
	FirstSeen     time.Time `json:"first_seen"`
	Misses        int       `json:"misses,omitempty"`
}

func visString(v scm.Visibility) string {
	switch v {
	case scm.VisPublic:
		return "public"
	case scm.VisInternal:
		return "internal"
	case scm.VisPrivate:
		return "private"
	}
	return "unknown"
}

func parseVis(s string) (scm.Visibility, bool) {
	switch s {
	case "public":
		return scm.VisPublic, true
	case "internal":
		return scm.VisInternal, true
	case "private":
		return scm.VisPrivate, true
	}
	return 0, false
}

// EncodeRow converts one tracked repository to its store row (UTC, deterministic JSON).
func EncodeRow(t Tracked) (store.Repo, error) {
	m := repoMeta{Schema: 1, Owner: t.Repo.Ref.Owner, Name: t.Repo.Ref.Name, Description: t.Repo.Description, DefaultBranch: t.Repo.DefaultBranch, Topics: t.Repo.Topics,
		Visibility: visString(t.Repo.Visibility), Archived: t.Repo.Archived, Fork: t.Repo.Fork, HTMLURL: t.Repo.HTMLURL,
		CreatedAt: t.Repo.CreatedAt.UTC(), PushedAt: t.Repo.PushedAt.UTC(), FirstSeen: t.FirstSeen.UTC(), Misses: t.Misses}
	for _, a := range t.Aliases {
		m.Aliases = append(m.Aliases, string(a))
	}
	b, err := json.Marshal(m)
	if err != nil {
		return store.Repo{}, err
	}
	r := t.Repo.Ref
	return store.Repo{Key: store.KeyOf(r), GitHubID: int64(r.ID), Slug: string(t.Repo.Slug), Meta: b, SeenAt: t.SeenAt.UTC()}, nil
}

// DecodeRow is the inverse of EncodeRow. A row it cannot decode (unknown schema, bad visibility, bad slug) is an error the caller
// logs and skips: the next reconcile re-adds the repository, so a damaged row never blocks start.
func DecodeRow(r store.Repo) (Tracked, error) {
	var m repoMeta
	if err := json.Unmarshal(r.Meta, &m); err != nil {
		return Tracked{}, fmt.Errorf("repo %s/%s: meta: %w", r.Key.Owner, r.Key.Name, err)
	}
	if m.Schema != 1 {
		return Tracked{}, fmt.Errorf("repo %s/%s: unsupported meta schema %d", r.Key.Owner, r.Key.Name, m.Schema)
	}
	vis, ok := parseVis(m.Visibility)
	if !ok {
		return Tracked{}, fmt.Errorf("repo %s/%s: unknown visibility %q", r.Key.Owner, r.Key.Name, m.Visibility)
	}
	if r.GitHubID == 0 {
		return Tracked{}, fmt.Errorf("repo %s/%s: bad id", r.Key.Owner, r.Key.Name)
	}
	if _, err := scm.ParseSlug(r.Slug); err != nil {
		return Tracked{}, fmt.Errorf("repo %s/%s: %w", r.Key.Owner, r.Key.Name, err)
	}
	owner, name := r.Key.Owner, r.Key.Name
	if m.Owner != "" || m.Name != "" { // the display case must be the folded key's own spelling, or the row is damaged
		if !strings.EqualFold(m.Owner, owner) || !strings.EqualFold(m.Name, name) {
			return Tracked{}, fmt.Errorf("repo %s/%s: meta names %s/%s do not match the key", r.Key.Owner, r.Key.Name, m.Owner, m.Name)
		}
		owner, name = m.Owner, m.Name
	}
	t := Tracked{FirstSeen: m.FirstSeen, SeenAt: r.SeenAt, Misses: m.Misses}
	t.Repo = scm.Repository{
		Ref:  scm.RepoRef{Source: scm.SourceID(r.Key.Source), ID: scm.RepoID(r.GitHubID), Owner: owner, Name: name},
		Slug: scm.Slug(r.Slug), Description: m.Description, DefaultBranch: m.DefaultBranch, Topics: m.Topics, Visibility: vis,
		Archived: m.Archived, Fork: m.Fork, HTMLURL: m.HTMLURL, CreatedAt: m.CreatedAt, PushedAt: m.PushedAt,
	}
	for _, a := range m.Aliases {
		if as, err := scm.ParseSlug(a); err == nil { // stored bytes are untrusted: an alias that is not a legal slug is dropped, not served
			t.Aliases = append(t.Aliases, as)
		}
	}
	clean, ok := normaliseListed(t.Repo)
	if !ok {
		return Tracked{}, fmt.Errorf("repo %s/%s: stored values fail validation", r.Key.Owner, r.Key.Name)
	}
	clean.Slug = t.Repo.Slug
	t.Repo = clean
	return t, nil
}

// statsPayload is the payload of the store item catalog.stats (explicit tags: the kernel struct is not a wire format).
type statsPayload struct {
	Schema      int         `json:"schema"`
	OpenIssues  int         `json:"open_issues"`
	Stars       int         `json:"stars"`
	License     string      `json:"license,omitempty"`
	Release     *releaseDTO `json:"release,omitempty"`
	HeadSHA     string      `json:"head_sha,omitempty"`
	HeadTreeOID string      `json:"head_tree_oid,omitempty"`
	PushedAt    time.Time   `json:"pushed_at,omitzero"`
}
type releaseDTO struct {
	Tag         string    `json:"tag"`
	Name        string    `json:"name,omitempty"`
	URL         string    `json:"url,omitempty"`
	Prerelease  bool      `json:"prerelease,omitempty"`
	PublishedAt time.Time `json:"published_at,omitzero"`
}

func encodeStats(s scm.RepoStats) ([]byte, error) {
	p := statsPayload{Schema: 1, OpenIssues: s.OpenIssues, Stars: s.Stars, License: s.License, HeadSHA: s.HeadSHA, HeadTreeOID: s.HeadTreeOID, PushedAt: s.PushedAt.UTC()}
	if r := s.Release; r != nil {
		p.Release = &releaseDTO{r.Tag, r.Name, r.URL, r.Prerelease, r.PublishedAt.UTC()}
	}
	return json.Marshal(p)
}

func decodeStats(b []byte) (scm.RepoStats, error) {
	var p statsPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return scm.RepoStats{}, err
	}
	if p.Schema != 1 {
		return scm.RepoStats{}, fmt.Errorf("unsupported stats schema %d", p.Schema)
	}
	s := scm.RepoStats{OpenIssues: p.OpenIssues, Stars: p.Stars, License: p.License, HeadSHA: p.HeadSHA, HeadTreeOID: p.HeadTreeOID, PushedAt: p.PushedAt}
	if r := p.Release; r != nil {
		s.Release = &scm.Release{Tag: r.Tag, Name: r.Name, URL: r.URL, Prerelease: r.Prerelease, PublishedAt: r.PublishedAt}
	}
	return s, nil
}

// sameStats compares two stats values (Release is a pointer).
func sameStats(a, b scm.RepoStats) bool {
	if (a.Release == nil) != (b.Release == nil) {
		return false
	}
	if a.Release != nil {
		x, y := *a.Release, *b.Release
		if x.Tag != y.Tag || x.Name != y.Name || x.URL != y.URL || x.Prerelease != y.Prerelease || !x.PublishedAt.Equal(y.PublishedAt) {
			return false
		}
	}
	return a.OpenIssues == b.OpenIssues && a.Stars == b.Stars && a.License == b.License && a.HeadSHA == b.HeadSHA &&
		a.HeadTreeOID == b.HeadTreeOID && a.PushedAt.Equal(b.PushedAt)
}
