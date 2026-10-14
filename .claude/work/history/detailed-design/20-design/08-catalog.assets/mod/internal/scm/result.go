package scm

import "time"

// Page is one page of a cursor-paginated upstream listing. Next is "" on the last page.
type Page[T any] struct {
	Items []T
	Next  string
}

// HasNext reports whether another page follows.
func (p Page[T]) HasNext() bool { return p.Next != "" }

// File is a file fetched from a repository. Truncated means the adapter cut Data at its size cap.
type File struct {
	Path, SHA string
	Data      []byte
	Truncated bool
}

// Blob is an immutable Git blob addressed by SHA.
type Blob struct {
	SHA  string
	Data []byte
}

// Validators are content identifiers, not credentials: they stay valid when tokens rotate. SHA is authoritative;
// ETag is an optimisation that is only meaningful inside one token lifetime.
type Validators struct{ SHA, ETag string }

// IsZero reports that nothing is known about the stored version.
func (v Validators) IsZero() bool { return v == Validators{} }

// Result is the outcome of a conditional fetch. NotModified means Value is the zero value and the caller keeps its
// stored value.
type Result[T any] struct {
	Value       T
	NotModified bool
	Validators  Validators
	FetchedAt   time.Time
}

// Resolve returns the value the caller should now hold.
func (r Result[T]) Resolve(stored T) T {
	if r.NotModified {
		return stored
	}
	return r.Value
}
