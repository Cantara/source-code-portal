package store

import (
	"errors"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/scm"
)

// MaxPayload bounds Item.Payload exactly like the real package (03-readmodel-store: 16 MiB).
const MaxPayload = 16 << 20

type RepoKey struct{ Source, Owner, Name string }

func KeyOf(r scm.RepoRef) RepoKey { return RepoKey{string(r.Source), r.Owner, r.Name} }

type ItemKey struct {
	Repo RepoKey
	Kind string
}
type Item struct {
	Key                     ItemKey
	State                   freshness.State
	FetchedAt, CheckedAt    time.Time
	Attempts                int
	ETag, ContentSHA, Error string
	Payload                 []byte
}
type Failure struct {
	Code, Message string
	At            time.Time
}

var ErrNoRepo = errors.New("store: no such repository")
