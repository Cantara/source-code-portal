// Package snapshot holds the immutable serving state of SCP and the value types every feature shares. Sync jobs contribute to a
// Builder; the Publisher applies their updates in batches and swaps in a new Snapshot behind an atomic pointer. Request handlers call
// Current once per request and read only that value.
package snapshot

import (
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/safehtml"
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

// Commit is one default-branch commit.
type Commit struct {
	SHA         string    `json:"sha"`
	Subject     string    `json:"subject"` // first line of the message
	AuthorName  string    `json:"authorName"`
	AuthorLogin string    `json:"authorLogin,omitempty"`
	URL         string    `json:"url"`
	CommittedAt time.Time `json:"committedAt"`
}

// Release is a repository's latest published release.
type Release struct {
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Published time.Time `json:"published"`
}

// CommitRef is a commit in a merged feed.
type CommitRef struct {
	Repo   RepoKey
	Commit *Commit // points into the repository's entry
}

// Descriptor is the repository's own scp.yaml. It affects display only.
type Descriptor struct {
	Present     bool      `json:"present"`
	File        string    `json:"file,omitempty"` // ".github/scp.yaml" etc.
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Lifecycle   string    `json:"lifecycle,omitempty"` // "" | experimental | production | deprecated
	Owners      []string  `json:"owners,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Links       []Link    `json:"links,omitempty"`
	Problems    []Problem `json:"problems,omitempty"`
}

// Link is a titled https link.
type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Problem is a finding in a repository-authored file.
type Problem struct {
	Pointer string `json:"pointer"` // JSON pointer into the document; "" for the whole document
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Doc is one rendered document of a repository: the README or a Markdown file under docs/.
type Doc struct {
	Path        string // repository path
	README      bool
	Title       string
	Description string
	Order       int    // front matter order
	Hidden      bool   // front matter hidden: left out of the document list
	Format      string // markdown | text
	HTML        safehtml.SafeHTML
	Source      []byte    // the text after truncation, for the Markdown surface
	TOC         []Heading // h2 and h3
	BlobSHA     string
	Truncated   bool  // the source was cut at the size cap
	State       State // ok | too_large | unsupported
}

// Heading is a TOC entry.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// RepoEntry is everything the snapshot holds about one repository. Entries are shared by pointer between snapshot versions and
// never mutated after publish; Builder.UpdateRepo replaces them.
type RepoEntry struct {
	Repo            Repo
	Fresh           Freshness // of the listing that last confirmed the repository
	Commits         []Commit  // newest first
	CommitsFresh    Freshness
	Descriptor      Descriptor
	DescriptorFresh Freshness
	Docs            []Doc // README first, then by order, then path
	DocsFresh       Freshness
	Release         *Release // nil when there is none or it is not known yet (ReleaseFresh.State tells which)
	ReleaseFresh    Freshness
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
	// Latest is every repository's commits merged, newest first (committedAt desc, repo asc, sha desc).
	Latest        []CommitRef
	groupsOf      map[RepoKey][]string
	latestByGroup map[string][]CommitRef
}

// Feed returns the merged commits of the group with id, or of every repository when id is empty.
func (s *Snapshot) Feed(id string) ([]CommitRef, bool) {
	if id == "" {
		return s.Latest, true
	}
	f, ok := s.latestByGroup[id]
	return f, ok
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
