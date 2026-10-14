// Package scm is a STUB of unit 01 (design 01-kernel.md section 3.1): the symbols internal/manifests uses, with the specified semantics.
package scm

import (
	"errors"
	"iter"
	"strings"
	"time"
)

type SourceID string
type RepoID int64
type InstallationID int64
type Slug string
type GroupID string

type RepoRef struct {
	Source SourceID
	ID     RepoID
	Owner  string
	Name   string
}

func (r RepoRef) Key() string { return strings.Join(r.KeyParts(), "/") }

// KeyParts are the three folded parts for job.NewKey(kind, ref.KeyParts()...).
func (r RepoRef) KeyParts() []string {
	return []string{string(r.Source), strings.ToLower(r.Owner), strings.ToLower(r.Name)}
}
func ParseRepoKey(s string) (RepoRef, error) {
	p := strings.SplitN(s, "/", 3)
	if len(p) != 3 || p[0] == "" || p[1] == "" || p[2] == "" {
		return RepoRef{}, errors.New("bad repo key")
	}
	return RepoRef{Source: SourceID(p[0]), Owner: strings.ToLower(p[1]), Name: strings.ToLower(p[2])}, nil
}

type Visibility uint8

const (
	VisUnknown Visibility = iota // zero is NOT public
	VisPublic
	VisInternal
	VisPrivate
)

type ViewClass uint8

const (
	ViewPublic ViewClass = iota
	ViewAll
)

func (v Visibility) Visible(c ViewClass) bool   { return v == VisPublic || c == ViewAll }
func (r Repository) VisibleTo(c ViewClass) bool { return r.Visibility.Visible(c) }

type Classed[T any] struct{ Public, All T }

func (c Classed[T]) For(v ViewClass) T {
	if v == ViewAll {
		return c.All
	}
	return c.Public
}

type Repository struct {
	Ref           RepoRef
	Slug          Slug
	Description   string
	DefaultBranch string
	Topics        []string
	Visibility    Visibility
	Archived      bool
	Fork          bool
	HTMLURL       string
	CreatedAt     time.Time
	PushedAt      time.Time
}

type Release struct {
	Tag, Name, URL string
	Prerelease     bool
	PublishedAt    time.Time
}

type RepoStats struct {
	OpenIssues  int
	Stars       int
	License     string
	Release     *Release
	HeadSHA     string
	HeadTreeOID string
	PushedAt    time.Time
}

type EntryType uint8

const (
	EntryUnknown EntryType = iota
	EntryBlob
	EntryTree
	EntryCommit
	EntryLink
)

type TreeEntry struct {
	Path, SHA string
	Mode      string
	Size      int64
	Type      EntryType
}
type Tree struct {
	sha       string
	entries   []TreeEntry
	truncated bool
}

func NewTree(sha string, entries []TreeEntry, truncated bool) Tree {
	return Tree{sha, entries, truncated}
}
func (t Tree) SHA() string     { return t.sha }
func (t Tree) Truncated() bool { return t.truncated }
func (t Tree) Len() int        { return len(t.entries) }
func (t Tree) All() iter.Seq[TreeEntry] {
	return func(yield func(TreeEntry) bool) {
		for _, e := range t.entries {
			if !yield(e) {
				return
			}
		}
	}
}

type TreeOptions struct {
	Recursive  bool
	Path       string
	MaxEntries int
}
type Page[T any] struct {
	Items []T
	Next  string
}
type File struct {
	Path, SHA string
	Data      []byte
	Truncated bool
}
type Blob struct {
	SHA  string
	Data []byte
}
type Validators struct{ SHA, ETag string }
type Result[T any] struct {
	Value       T
	NotModified bool
	Validators  Validators
	FetchedAt   time.Time
}

// Membership stub: an index of group -> member slugs with visibility (the real type is built by NewMembership(MembershipSpec)).
type Membership struct {
	By  map[GroupID][]Slug
	Vis map[Slug]Visibility
}

// MemberRepo, GroupSpec, MembershipSpec and NewMembership mirror the real constructor (01-kernel.md section 3.1) so tests run unchanged on both.
type MemberRepo struct {
	Slug       Slug
	Ref        RepoRef
	Visibility Visibility
}
type GroupSpec struct {
	Meta    GroupMeta
	Members []Slug
	Default Slug
}
type MembershipSpec struct {
	Repos  []MemberRepo
	Groups []GroupSpec
}

func NewMembership(spec MembershipSpec) (Membership, error) {
	m := Membership{By: map[GroupID][]Slug{}, Vis: map[Slug]Visibility{}}
	for _, r := range spec.Repos {
		m.Vis[r.Slug] = r.Visibility
	}
	for _, g := range spec.Groups {
		m.By[g.Meta.ID] = append([]Slug(nil), g.Members...)
	}
	return m, nil
}

func (m Membership) ReposOf(g GroupID) []Slug { return m.VisibleReposOf(g, ViewAll) }
func (m Membership) VisibleReposOf(g GroupID, c ViewClass) []Slug {
	var out []Slug
	for _, s := range m.By[g] {
		if m.Vis == nil || m.Vis[s].Visible(c) || m.Vis[s] == VisUnknown && c == ViewAll {
			out = append(out, s)
		}
	}
	return out
}
func (m Membership) Group(g GroupID) (GroupMeta, bool) { _, ok := m.By[g]; return GroupMeta{ID: g}, ok }

type GroupMeta struct {
	ID           GroupID
	DisplayName  string
	Description  string
	Integrations map[string]map[string]string
}

type EventKind string

const (
	EventPush              EventKind = "push"
	EventRelease           EventKind = "release"
	EventCreate            EventKind = "create"
	EventRepository        EventKind = "repository"
	EventInstallation      EventKind = "installation"
	EventInstallationRepos EventKind = "installation_repositories"
	EventPing              EventKind = "ping"
	EventHeadChanged       EventKind = "head_changed"
)

type Event struct {
	Kind                     EventKind
	Delivery                 string
	Source                   SourceID
	Installation             InstallationID
	Repo                     RepoRef
	Action                   string
	Ref                      string
	Before, After            string
	Created, Deleted, Forced bool
	CommitCount              int
	ChangedPaths             []string
	PathsComplete            bool
	HeadTreeOID              string
}

var ErrNotFound, ErrForbidden, ErrUnavailable = errors.New("not found"), errors.New("forbidden"), errors.New("unavailable")

func (e Event) Branch() (string, bool) {
	b, ok := strings.CutPrefix(e.Ref, "refs/heads/")
	return b, ok && b != ""
}
func (e Event) IsPushTo(branch string) bool {
	b, ok := e.Branch()
	return e.Kind == EventPush && !e.Deleted && ok && b == branch
}
