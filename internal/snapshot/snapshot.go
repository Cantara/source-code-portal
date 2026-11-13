// Package snapshot holds the immutable serving state of SCP: the catalogue, the facets features hang on its repositories and the
// indexes they derive. Sync jobs contribute to a Builder; the Publisher applies their updates in batches and swaps in a new Snapshot
// behind an atomic pointer. Request handlers call Current once per request and read only that value.
package snapshot

import (
	"strings"
	"time"
)

// RepoID is the identity of a repository: the numeric GitHub id survives renames and transfers.
type RepoID struct {
	Source   string // configured source id
	GitHubID int64
}

// RepoKey is the address of a repository in URLs: lower-case "owner/name".
type RepoKey string

// KeyOf returns the address of owner/name.
func KeyOf(owner, name string) RepoKey { return RepoKey(strings.ToLower(owner + "/" + name)) }

// Outranks reports whether source a owns an address that sources a and b both list: the one whose id sorts first. The rule has no
// state and does not depend on which listing came first, so the store, the engine and the snapshot agree whatever the order of their
// listings, and an address does not change hands again and again (design/store-model.md).
func Outranks(a, b string) bool { return a < b }

// Owner returns the owner segment of the address.
func (k RepoKey) Owner() string { o, _, _ := strings.Cut(string(k), "/"); return o }

// Repo is a catalogued repository with the facts from its GitHub listing.
type Repo struct {
	ID            RepoID    `json:"id"`
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	HTMLURL       string    `json:"htmlUrl"`
	Homepage      string    `json:"homepage,omitempty"`
	DefaultBranch string    `json:"defaultBranch"`
	License       string    `json:"license,omitempty"` // SPDX id
	Visibility    string    `json:"visibility"`
	Topics        []string  `json:"topics,omitempty"`
	Archived      bool      `json:"archived,omitempty"`
	Fork          bool      `json:"fork,omitempty"`
	Stars         int       `json:"stars"`
	OpenIssues    int       `json:"openIssues"` // open issues and pull requests, as GitHub counts them
	PushedAt      time.Time `json:"pushedAt"`
}

// Key returns the repository's address.
func (r Repo) Key() RepoKey { return KeyOf(r.Owner, r.Name) }

// Public reports whether everyone may see the repository: it is neither private nor internal.
func (r Repo) Public() bool { return r.Visibility != "private" && r.Visibility != "internal" }

// Group is a configured group with its resolved members. A retired member (archived, deprecated: Publisher.Retire) is not one of the
// product's Members: it is in Retired, which the Archive reads; everything that presents the group reads Members.
type Group struct {
	ID             string
	Name           string
	Description    string
	DefaultRepo    RepoKey // resolved address; empty when DefaultMissing
	DefaultMissing bool
	Members        []RepoKey // sorted; the live members
	Retired        []RepoKey // sorted; the retired members
}

// SourceStatus reports the last listing of one configured source.
type SourceStatus struct {
	ID    string
	Fresh Freshness
	Repos int
}

// Problem is a finding in a repository-authored file.
type Problem struct {
	Pointer string `json:"pointer"` // JSON pointer into the document; "" for the whole document
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RepoEntry is everything the snapshot holds about one repository: its listing and the facets features contribute. Entries are
// shared by pointer between snapshot versions and never mutated after publish; Builder.UpdateRepo replaces them.
type RepoEntry struct {
	Repo   Repo
	Fresh  Freshness        // of the listing that last confirmed the repository
	facets map[string]facet // by name; copied on write
}

// Snapshot is the serving state. It is immutable after publish.
type Snapshot struct {
	// Version identifies the content: the sum of the digests of the published items (Builder.Mark), 53 bits. Equal items give
	// equal versions, on every instance and after a restart; it is compared for equality only.
	Version uint64
	BuiltAt time.Time
	// Catalogued is true once a catalogue is loaded, from the store at start or from a complete listing.
	Catalogued bool
	Repos      map[RepoKey]*RepoEntry
	Aliases    map[RepoKey]RepoKey // old address -> current address
	Groups     []Group             // config order
	Sources    []SourceStatus      // sorted by id
	groupsOf   map[RepoKey][]string
	indexers   []Indexer             // what the features derive from every repository, run at publish
	retire     func(*RepoEntry) bool // a group member it says yes to is retired (Publisher.Retire); nil: none
	indexes    map[string]any        // index name -> what its indexer built
	public     *Snapshot             // this view without its private and internal repositories; s itself when there are none
	lean       *Snapshot             // this view without the hidden forks; s itself when there are none
	fat        *Snapshot             // this view with the hidden forks; s itself when it has them or there are none
	hidden     map[RepoKey]bool      // the forks no source shows and no group pins (Builder.SetHidden)
	marks      map[string]mark
	sum        uint64
	digests    map[RepoKey]uint64 // per repository: the sum of its items' content digests
}

// mark is the digest of one published item and the repository it belongs to ("" for a source's items).
type mark struct {
	repo    RepoKey
	digest  uint64
	content uint64 // what the item holds, without when it was checked
}

// Public returns the view of s without its private and internal repositories: s itself when it has none. It keeps s's choice of forks.
func (s *Snapshot) Public() *Snapshot {
	if s.public == nil {
		return s
	}
	return s.public
}

// View returns the view a request reads: with or without the private and internal repositories (private), with or without the forks
// no source shows and no group pins (forks). A snapshot without private repositories or hidden forks returns itself for the choice
// that changes nothing.
func (s *Snapshot) View(private, forks bool) *Snapshot {
	v := s
	if !forks && s.lean != nil {
		v = s.lean
	}
	if !private {
		v = v.Public()
	}
	return v
}

// Hidden reports whether key is a fork the default views leave out.
func (s *Snapshot) Hidden(key RepoKey) bool { return s.hidden[key] }

// HiddenForks is how many forks the default views leave out of this view's kind (private or public): the same in the view that
// shows them and in the one that does not, so a page can offer "Show forks (N)" either way.
func (s *Snapshot) HiddenForks() int {
	if s.fat == nil || s.lean == nil {
		return 0
	}
	return len(s.fat.Repos) - len(s.lean.Repos)
}

// GroupHiddenForks is HiddenForks within one group: the members the group has with the forks shown and has not without them.
func (s *Snapshot) GroupHiddenForks(id string) int {
	if s.fat == nil || s.lean == nil {
		return 0
	}
	all, _ := s.fat.Group(id)
	lean, _ := s.lean.Group(id)
	return len(all.Members) - len(lean.Members)
}

// Digest returns the content digest of the repository at key: the sum of its items' content digests (Builder.Mark), which changes
// when what any of its items holds changes and not when an item is only checked again; 0 when it has no items. It is what a derived
// index per repository is keyed by; the listing's fields are not in it.
func (s *Snapshot) Digest(key RepoKey) uint64 { return s.digests[key] }

// Repo returns the entry at key, following an alias. moved is true when key is an old address.
func (s *Snapshot) Repo(key RepoKey) (e *RepoEntry, moved bool) {
	if e, ok := s.Repos[key]; ok {
		return e, false
	}
	if cur, ok := s.Aliases[key]; ok {
		return s.Repos[cur], true
	}
	return nil, false
}

// Group returns the group with id.
func (s *Snapshot) Group(id string) (Group, bool) {
	for _, g := range s.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// GroupsOf returns the ids of the groups that contain key, as a live or a retired member, in config order.
func (s *Snapshot) GroupsOf(key RepoKey) []string { return s.groupsOf[key] }
