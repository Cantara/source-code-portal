package scm

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// RepoRef identifies a repository at its source. ID survives rename and transfer; Owner and Name are the current
// GitHub names. Compare repositories with SameRepo or Key, never with ==: ID may be 0 (a ref parsed from a job key)
// and GitHub names are case-insensitive.
type RepoRef struct {
	Source SourceID
	ID     RepoID
	Owner  string
	Name   string
}

// IsZero reports the zero ref (installation-level events carry one).
func (r RepoRef) IsZero() bool { return r == RepoRef{} }

// Key is "source/owner/name" with owner and name lower-cased (GitHub resolves both case-insensitively, so the
// folded form is stable whichever casing a payload used). It is the tail of every per-repository job key
// and the join key of store rows.
func (r RepoRef) Key() string {
	return string(r.Source) + "/" + lowerASCII(r.Owner) + "/" + lowerASCII(r.Name)
}

// KeyParts is Key split into its three parts, for job.NewKey(kind, ref.KeyParts()...): scm cannot import job, and a
// key built from anything but the folded parts would not coalesce with the planner's key for the same repository.
func (r RepoRef) KeyParts() []string {
	return []string{string(r.Source), lowerASCII(r.Owner), lowerASCII(r.Name)}
}

// SameRepo reports whether two refs name the same repository: equal non-zero IDs, or equal Key().
func (r RepoRef) SameRepo(o RepoRef) bool {
	if r.ID != 0 && o.ID != 0 {
		return r.Source == o.Source && r.ID == o.ID
	}
	return r.Key() == o.Key()
}

// Validate checks that Source, Owner and Name are safe to embed in a job key and in a URL path.
func (r RepoRef) Validate() error {
	if _, err := ParseSourceID(string(r.Source)); err != nil {
		return err
	}
	return validateOwnerName(r.Owner, r.Name)
}

func validateOwnerName(owner, name string) error {
	if owner == "" || len(owner) > 100 || !isAlnum(owner[0]) {
		return invalid("repo owner", owner, "1..100 bytes, first a-z A-Z 0-9")
	}
	for i := 0; i < len(owner); i++ {
		c := owner[i]
		if !(isAlnum(c) || c == '.' || c == '_' || c == '-') {
			return invalid("repo owner", owner, "allowed: a-z A-Z 0-9 '.' '_' '-'")
		}
	}
	if name == "" || len(name) > 100 || name == "." || name == ".." {
		return invalid("repo name", name, "1..100 bytes, not '.' or '..'")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(isAlnum(c) || c == '.' || c == '_' || c == '-') {
			return invalid("repo name", name, "allowed: a-z A-Z 0-9 '.' '_' '-'")
		}
	}
	return nil
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// ParseRepoKey parses the output of RepoRef.Key. The result has ID 0 and folded owner and name.
func ParseRepoKey(s string) (RepoRef, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return RepoRef{}, invalid("repo key", s, "want source/owner/name")
	}
	src, err := ParseSourceID(parts[0])
	if err != nil {
		return RepoRef{}, err
	}
	if err := validateOwnerName(parts[1], parts[2]); err != nil {
		return RepoRef{}, err
	}
	return RepoRef{Source: src, Owner: lowerASCII(parts[1]), Name: lowerASCII(parts[2])}, nil
}

// Visibility is a repository's GitHub visibility. The zero value VisUnknown is NOT public: anything that is not
// explicitly VisPublic is hidden from ViewPublic (fail closed).
type Visibility uint8

const (
	VisUnknown Visibility = iota
	VisPublic
	VisInternal
	VisPrivate
)

func (v Visibility) String() string {
	switch v {
	case VisPublic:
		return "public"
	case VisInternal:
		return "internal"
	case VisPrivate:
		return "private"
	}
	return "unknown"
}

// ParseVisibility is the inverse of String; the empty string and unknown words are errors (never VisPublic).
func ParseVisibility(s string) (Visibility, error) {
	switch s {
	case "public":
		return VisPublic, nil
	case "internal":
		return VisInternal, nil
	case "private":
		return VisPrivate, nil
	case "unknown":
		return VisUnknown, nil
	}
	return VisUnknown, invalid("visibility", s, "want public, internal, private or unknown")
}

func (v Visibility) MarshalText() ([]byte, error) { return []byte(v.String()), nil }
func (v *Visibility) UnmarshalText(b []byte) error {
	p, err := ParseVisibility(string(b))
	if err != nil {
		return err
	}
	*v = p
	return nil
}

// ViewClass is what a principal may see. The zero value is the least privileged class.
type ViewClass uint8

const (
	ViewPublic ViewClass = iota // VisPublic repositories only
	ViewAll                     // every repository
)

// Visible reports whether a repository of this visibility is visible to the class.
func (v Visibility) Visible(c ViewClass) bool {
	if v == VisPublic {
		return true
	}
	return c == ViewAll
}

// Classed holds the two variants of a global projection. When the deployment has no non-public repositories
// the producer stores the same value in both (Same).
type Classed[T any] struct{ Public, All T }

// Same returns a Classed whose variants are the same value.
func Same[T any](v T) Classed[T] { return Classed[T]{Public: v, All: v} }

// For selects a variant. Any class other than ViewAll yields the Public variant.
func (c Classed[T]) For(v ViewClass) T {
	if v == ViewAll {
		return c.All
	}
	return c.Public
}

// Repository is the catalogue's description of one repository. Topics is shared by readers: producers Clone before Put.
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

// VisibleTo reports whether the repository may be shown to a class.
func (r Repository) VisibleTo(c ViewClass) bool { return r.Visibility.Visible(c) }

// Clone returns a copy that shares no mutable memory with r.
func (r Repository) Clone() Repository {
	r.Topics = slices.Clone(r.Topics)
	return r
}

// Release is the latest release or tag of a repository.
type Release struct {
	Tag, Name, URL string
	Prerelease     bool
	PublishedAt    time.Time
}

// RepoStats are the cheap per-repository facts of the head sweep.
type RepoStats struct {
	OpenIssues  int
	Stars       int
	License     string   // SPDX id or ""
	Release     *Release // nil: none
	HeadSHA     string   // default-branch HEAD commit
	HeadTreeOID string   // tree object id of HEAD (change detection for docs and manifests)
	PushedAt    time.Time
}

// Clone returns a deep copy.
func (s RepoStats) Clone() RepoStats {
	if s.Release != nil {
		r := *s.Release
		s.Release = &r
	}
	return s
}

func (r RepoRef) String() string { return fmt.Sprintf("%s/%s/%s", r.Source, r.Owner, r.Name) }
