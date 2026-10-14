package store

import (
	"fmt"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
)

// RepoKey is the durable identity of a repository: (source, owner, name). GitHub's numeric id is carried by Repo.GitHubID
// so a rename keeps its rows (section 4.5 of the design).
type RepoKey struct{ Source, Owner, Name string }

// KeyOf is the single conversion from the kernel identity (the other is scm.RepoRef.Key / ParseRepoKey for job keys).
//
// It MUST use the kernel's folded parts: scm.RepoRef.Key() ASCII-lower-cases owner and name (GitHub resolves both
// case-insensitively and every job key carries the folded tail), so a RepoKey built from the raw display case would never
// match the repository a job addresses (verification V-01).
func KeyOf(r scm.RepoRef) RepoKey {
	p := r.KeyParts() // source, folded owner, folded name
	return RepoKey{Source: p[0], Owner: p[1], Name: p[2]}
}

// String is "source/owner/name": identical to scm.RepoRef.Key() and to the tail of every per-repository job.Key.
func (k RepoKey) String() string { return k.Source + "/" + k.Owner + "/" + k.Name }
func (k RepoKey) IsZero() bool   { return k == RepoKey{} }

// ItemKey addresses one item row. Kind is owned by features and equals the job.Spec.Kind whose plan the row carries
// (design section 4.1.3).
type ItemKey struct {
	Repo RepoKey
	Kind string
}

// InvalidError is returned for a malformed argument; it never reaches SQLite.
type InvalidError struct{ Field, Reason string }

func (e *InvalidError) Error() string   { return fmt.Sprintf("store: invalid %s: %s", e.Field, e.Reason) }
func (e *InvalidError) Is(t error) bool { return t == ErrInvalid }

func invalid(field, reason string) error { return &InvalidError{field, reason} }

func validSegment(field, s string) error {
	switch {
	case s == "":
		return invalid(field, "empty")
	case len(s) > 200:
		return invalid(field, "longer than 200 bytes")
	case strings.ContainsAny(s, "/\x00"):
		return invalid(field, "contains '/' or NUL (job keys split on '/')")
	}
	return nil
}

// Validate checks the key as the contract demands: three non-empty segments without '/' or NUL, and owner and name already
// ASCII-folded (no 'A'-'Z'), so two spellings of one repository can never become two rows (UNIQUE is case-sensitive).
func (k RepoKey) Validate() error {
	if err := validSegment("repo.source", k.Source); err != nil {
		return err
	}
	if err := validSegment("repo.owner", k.Owner); err != nil {
		return err
	}
	if err := validSegment("repo.name", k.Name); err != nil {
		return err
	}
	if hasUpperASCII(k.Source) || hasUpperASCII(k.Owner) || hasUpperASCII(k.Name) {
		return invalid("repo", "must be ASCII-lower-case (use store.KeyOf, which folds like scm.RepoRef.Key)")
	}
	return nil
}

func hasUpperASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}

// ValidKind: lower-case dotted identifier, 3..64 bytes: "commits.refresh", "docs.refresh", "manifests.facts".
func ValidKind(kind string) error {
	if len(kind) < 3 || len(kind) > 64 {
		return invalid("kind", "length must be 3..64")
	}
	dot := false
	for i := 0; i < len(kind); i++ {
		c := kind[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-':
		case c == '.' && i > 0 && i < len(kind)-1 && kind[i-1] != '.':
			dot = true
		default:
			return invalid("kind", "must match [a-z0-9_-]+(\\.[a-z0-9_-]+)+")
		}
	}
	if !dot {
		return invalid("kind", "needs at least one '.'")
	}
	return nil
}

func (k ItemKey) Validate() error {
	if err := k.Repo.Validate(); err != nil {
		return err
	}
	return ValidKind(k.Kind)
}
