// Package parked is the admins' list of parked repositories (design/parked.md): repositories the portal keeps reading and storing,
// webhook events included, and shows nowhere. A parked repository is in no snapshot view (snapshot.Publisher.Park); taking it off the
// list shows it again at once, with everything that happened while it was parked.
package parked

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"

	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Name is the stored document.
const Name = "parked"

const (
	maxBytes   = 128 << 10 // the document or a request body
	maxEntries = 2000
)

var repoKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9._-]+$`)

// List is the stored document: the parked repositories by address (owner/name, lower case), sorted.
type List struct {
	Repos []string `json:"repos"`
}

// Parse decodes and checks the stored document: at most 128 KiB of one JSON object, unknown fields refused, addresses in their form,
// sorted and without repeats.
func Parse(b []byte) (List, error) {
	if len(b) > maxBytes {
		return List{}, fmt.Errorf("larger than %d KiB", maxBytes>>10)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var l List
	if err := dec.Decode(&l); err != nil {
		return List{}, fmt.Errorf("not a list of parked repositories: %w", err)
	}
	return clean(l)
}

// clean checks a list and puts it in its stored form.
func clean(l List) (List, error) {
	if len(l.Repos) > maxEntries {
		return List{}, fmt.Errorf("repos: at most %d", maxEntries)
	}
	out := make([]string, 0, len(l.Repos))
	for _, r := range l.Repos {
		if !repoKey.MatchString(r) {
			return List{}, fmt.Errorf("repos: %q is not an address (owner/name, lower case)", r)
		}
		out = append(out, r)
	}
	slices.Sort(out)
	return List{Repos: slices.Compact(out)}, nil
}

// Set is what the admins parked.
type Set struct {
	file    *docstore.Doc[List]
	changed func()
}

// Open reads the stored document from db (nil: nothing is parked, and nothing can be saved). changed is called after every save, so
// the snapshot is published again without the newly parked repositories (or with the ones taken off).
func Open(ctx context.Context, db docstore.Backend, changed func(), log *slog.Logger) *Set {
	if changed == nil {
		changed = func() {}
	}
	return &Set{file: docstore.Open(ctx, db, Name, List{Repos: []string{}}, Parse, log.With("store", Name)), changed: changed}
}

// Problems is why the stored document is not served, for the admin status.
func (s *Set) Problems() []string {
	if p := s.file.Get().Problem; p != "" {
		return []string{Name + ": " + p + "; nothing is parked until it is saved again"}
	}
	return nil
}

// Has reports whether the repository at key is parked.
func (s *Set) Has(key snapshot.RepoKey) bool {
	_, ok := slices.BinarySearch(s.file.Get().Value.Repos, string(key))
	return ok
}

// Repo reports whether e is parked: what snapshot.Publisher.Park asks.
func (s *Set) Repo(e *snapshot.RepoEntry) bool { return s.Has(e.Repo.Key()) }
