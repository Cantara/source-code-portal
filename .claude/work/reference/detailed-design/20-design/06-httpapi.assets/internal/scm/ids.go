// Package scm is the provider-neutral vocabulary of the portal: value types for repositories, commits, trees,
// events and the portal identity of a repository (Slug, GroupID, Membership, ViewClass).
//
// It imports nothing from this module and carries no behaviour beyond small pure helpers. Types have no
// encoding/json or yaml tags on purpose: persisted forms are owned by the feature that persists them.
package scm

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors. Adapters wrap them; callers use errors.Is.
var (
	ErrNotFound    = errors.New("scm: not found")
	ErrForbidden   = errors.New("scm: forbidden")
	ErrUnavailable = errors.New("scm: unavailable")
	// ErrInvalid is matched (errors.Is) by every *ValidationError returned from this package.
	ErrInvalid = errors.New("scm: invalid value")
)

// ValidationError reports a rejected identifier. Value is truncated to 64 bytes so a hostile input cannot bloat logs.
type ValidationError struct {
	Type   string // "slug", "group id", "source id", "repo key", ...
	Value  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("scm: invalid %s %q: %s", e.Type, truncate(e.Value, 64), e.Reason)
}
func (e *ValidationError) Is(target error) bool { return target == ErrInvalid }

func invalid(typ, value, reason string) error {
	return &ValidationError{Type: typ, Value: value, Reason: reason}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}

// SourceID names a configured GitHub source. Grammar: ^[a-z0-9][a-z0-9-]{0,31}$.
type SourceID string

// RepoID is the GitHub numeric repository id (stable across rename and transfer). 0 means unknown.
type RepoID int64

// InstallationID is a GitHub App installation id. 0 means none.
type InstallationID int64

// Slug is the portal-visible repository id: exactly one URL path segment, unique in the catalogue.
// Canonical form is lower-case ASCII: ^[a-z0-9._-]{1,100}$, not "." or "..", not a reserved word.
type Slug string

// GroupID identifies a repository group. Grammar: ^[a-z0-9][a-z0-9-]{0,62}$ (lower case only).
type GroupID string

func (s SourceID) String() string { return string(s) }
func (s Slug) String() string     { return string(s) }
func (g GroupID) String() string  { return string(g) }

// ParseSourceID validates a source id.
func ParseSourceID(s string) (SourceID, error) {
	if len(s) == 0 || len(s) > 32 {
		return "", invalid("source id", s, "length must be 1..32")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0
		if !ok {
			return "", invalid("source id", s, "allowed: a-z 0-9 and '-' (not first)")
		}
	}
	return SourceID(s), nil
}

// IsReservedSlug reports the words a slug may not take: they are ambiguous path segments in /media/{repo}/...
// ("ext") and in the badge, group and avatar routes ("groups", "avatars").
func IsReservedSlug(s string) bool {
	switch s {
	case "ext", "groups", "avatars":
		return true
	}
	return false
}

// ParseSlug validates a canonical (already lower-case) slug.
func ParseSlug(s string) (Slug, error) {
	if len(s) == 0 || len(s) > 100 {
		return "", invalid("slug", s, "length must be 1..100")
	}
	if s == "." || s == ".." {
		return "", invalid("slug", s, "dot segments are not allowed")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return "", invalid("slug", s, "allowed: a-z 0-9 '.' '_' '-'")
		}
	}
	if IsReservedSlug(s) {
		return "", invalid("slug", s, "reserved word (set slug_prefix for the source)")
	}
	return Slug(s), nil
}

// SlugFromName derives the canonical slug of a GitHub repository name: ASCII lower-casing, then ParseSlug.
func SlugFromName(name string) (Slug, error) { return ParseSlug(lowerASCII(name)) }

// ParseGroupID validates a canonical group id.
func ParseGroupID(s string) (GroupID, error) {
	if len(s) == 0 || len(s) > 63 {
		return "", invalid("group id", s, "length must be 1..63")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0) {
			return "", invalid("group id", s, "allowed: a-z 0-9 and '-' (not first)")
		}
	}
	return GroupID(s), nil
}

// GroupIDFromName folds a mixed-case group id written in configuration (BaselineProjects, Core-All) to canonical form.
func GroupIDFromName(s string) (GroupID, error) { return ParseGroupID(lowerASCII(s)) }

func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}
