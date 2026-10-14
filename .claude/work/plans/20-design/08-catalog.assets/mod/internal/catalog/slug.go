package catalog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
)

// MaxSlugBytes bounds a portal slug: the kernel's scm.ParseSlug limit. A 100-byte GitHub name plus a prefix can exceed it (too_long).
const MaxSlugBytes = 100

var (
	slugRe     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	prefixRe   = regexp.MustCompile(`^[A-Za-z0-9._-]{0,32}$`)
	instanceRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,63}$`)
	groupRe    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// reservedSlugs may not be a slug (ambiguous path segments in /media/{repo}/..., /badges/{repo}/..., 10-architecture 11.3), compared case-insensitively.
var reservedSlugs = map[string]bool{"ext": true, "groups": true, "avatars": true}

// reservedSuffixes are the URL suffixes that the web layer never serves the SPA for (10-architecture 11.2, "Reserved prefixes").
var reservedSuffixes = map[string]bool{"md": true, "adoc": true, "txt": true, "xml": true, "svg": true, "json": true, "yaml": true}

// SlugError is the typed error of MakeSlug.
type SlugError struct {
	Name string
	Code string // invalid, too_long, reserved, dot_segment
	Msg  string
}

func (e *SlugError) Error() string { return fmt.Sprintf("slug for %q: %s", e.Name, e.Msg) }

// MakeSlug derives the portal slug of a repository: prefix + name, where a trailing ".md"-like extension has its dot
// replaced by '-' (so /repos/{slug} is never mistaken for a raw document URL). Pure and deterministic; the same input always gives the same slug.
func MakeSlug(prefix, name string) (scm.Slug, error) {
	s := prefix + name
	switch {
	case name == "" || !slugRe.MatchString(s):
		return "", &SlugError{name, "invalid", "a slug contains only letters, digits, '.', '_' and '-'"}
	case s == "." || s == "..":
		return "", &SlugError{name, "dot_segment", "'.' and '..' are not slugs"}
	case len(s) > MaxSlugBytes:
		return "", &SlugError{name, "too_long", fmt.Sprintf("longer than %d bytes", MaxSlugBytes)}
	}
	s = strings.ToLower(s) // canonical slugs are lower-case (scm.ParseSlug); uniqueness is therefore case-insensitive by construction
	if reservedSlugs[s] || scm.IsReservedSlug(s) {
		return "", &SlugError{name, "reserved", "reserved word; set catalog.slug_prefix_by_source for this source"}
	}
	if i := strings.LastIndexByte(s, '.'); i > 0 && reservedSuffixes[s[i+1:]] {
		s = s[:i] + "-" + s[i+1:]
	}
	slug, err := scm.ParseSlug(s) // the kernel grammar is the final authority
	if err != nil {
		return "", &SlugError{name, "invalid", err.Error()}
	}
	return slug, nil
}

// FoldSlug is the uniqueness key of a slug (GitHub names are case-insensitive).
func FoldSlug(s scm.Slug) string { return strings.ToLower(string(s)) }

// ValidGroupID reports whether id is a legal group id (lower-case, digits, single hyphens, at most 63 bytes).
func ValidGroupID(id scm.GroupID) bool { return len(id) <= 63 && groupRe.MatchString(string(id)) }
