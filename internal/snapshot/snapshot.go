// Package snapshot holds the immutable serving state of SCP and the value types every feature shares. Sync jobs contribute to a
// Builder; the Publisher applies their updates in batches and swaps in a new Snapshot behind an atomic pointer. Request handlers call
// Current once per request and read only that value.
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

// Group is a configured group with its resolved members.
type Group struct {
	ID             string
	Name           string
	Description    string
	DefaultRepo    RepoKey // resolved address; empty when DefaultMissing
	DefaultMissing bool
	Members        []RepoKey // sorted
}

// SourceStatus reports the last listing of one configured source.
type SourceStatus struct {
	ID    string
	Fresh Freshness
	Repos int
}

// RepoEntry is everything the snapshot holds about one repository. Entries are shared by pointer between snapshot versions and
// never mutated after publish.
type RepoEntry struct {
	Repo  Repo
	Fresh Freshness // of the listing that last confirmed the repository
}

// Snapshot is the serving state. It is immutable after publish.
type Snapshot struct {
	Version uint64
	BuiltAt time.Time
	// Catalogued is true once a catalogue is loaded, from the store at start or from a complete listing.
	Catalogued bool
	Repos      map[RepoKey]*RepoEntry
	Aliases    map[RepoKey]RepoKey // old address -> current address
	Groups     []Group             // config order
	Sources    []SourceStatus      // sorted by id
	groupsOf   map[RepoKey][]string
}

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

// GroupsOf returns the ids of the groups that contain key, in config order.
func (s *Snapshot) GroupsOf(key RepoKey) []string { return s.groupsOf[key] }
