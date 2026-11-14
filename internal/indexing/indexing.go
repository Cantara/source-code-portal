// Package indexing keeps what an admin keeps out of search engines (design/indexing.md, IX1): groups (products) and repositories,
// stored as the document `indexing`. An excluded page is left out of the sitemap and answers with `X-Robots-Tag: noindex, nofollow`
// (never a robots.txt rule: a search engine must be able to fetch a page to read its noindex). The portal's own pages show everything
// as before; this is only what is offered to search engines.
package indexing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Name is the stored document.
const Name = "indexing"

const (
	maxBytes   = 128 << 10 // the document or a request body
	maxEntries = 2000      // groups and repositories each
)

var (
	groupID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	repoKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9._-]+$`)
)

// Excluded is the stored document: the groups and the repositories (owner/name, lower case) kept out of search engines.
type Excluded struct {
	Groups []string `json:"groups"`
	Repos  []string `json:"repos"`
}

// Parse decodes and checks the stored document: at most 128 KiB of one JSON object, unknown fields refused, ids and addresses in
// their forms, sorted and without repeats.
func Parse(b []byte) (Excluded, error) {
	if len(b) > maxBytes {
		return Excluded{}, fmt.Errorf("larger than %d KiB", maxBytes>>10)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var e Excluded
	if err := dec.Decode(&e); err != nil {
		return Excluded{}, fmt.Errorf("not an exclusion list: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Excluded{}, errors.New("not an exclusion list: more than one JSON value")
	}
	return clean(e)
}

// clean checks e and returns it sorted, without repeats, and with empty lists (never null).
func clean(e Excluded) (Excluded, error) {
	var err error
	if e.Groups, err = tidy("groups", e.Groups, groupID, 63); err != nil {
		return Excluded{}, err
	}
	if e.Repos, err = tidy("repos", e.Repos, repoKey, 200); err != nil {
		return Excluded{}, err
	}
	return e, nil
}

func tidy(field string, in []string, form *regexp.Regexp, max int) ([]string, error) {
	if len(in) > maxEntries {
		return nil, fmt.Errorf("%s: at most %d entries", field, maxEntries)
	}
	out := make([]string, 0, len(in))
	for i, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if len(s) > max || !form.MatchString(s) {
			return nil, fmt.Errorf("%s[%d]: %q is not an address of that kind", field, i, s)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// Set is what an admin kept out of search engines.
type Set struct {
	file *docstore.Doc[Excluded]
}

// Open reads the stored document from db (nil: nothing is excluded, and nothing can be saved).
func Open(ctx context.Context, db docstore.Backend, log *slog.Logger) *Set {
	return &Set{file: docstore.Open(ctx, db, Name, Excluded{Groups: []string{}, Repos: []string{}}, Parse, log.With("store", Name))}
}

// Problems is why the stored document is not served, for the admin status.
func (s *Set) Problems() []string {
	if p := s.file.Get().Problem; p != "" {
		return []string{Name + ": " + p + "; nothing is excluded until it is saved again"}
	}
	return nil
}

// Revision identifies the stored content: a view built from it is built again when it changes.
func (s *Set) Revision() string { return s.file.Get().Revision }

// Group reports whether the group id is kept out of search engines.
func (s *Set) Group(id string) bool {
	return slices.Contains(s.file.Get().Value.Groups, id)
}

// Repo reports whether the repository at key, in the view v, is kept out: named itself, or a member of groups that are all kept out
// (a repository in no group is not touched by a group).
func (s *Set) Repo(v *snapshot.Snapshot, key snapshot.RepoKey) bool {
	st := s.file.Get().Value
	if e, _ := v.Repo(key); e != nil {
		key = e.Repo.Key()
	}
	if slices.Contains(st.Repos, string(key)) {
		return true
	}
	ids := v.GroupsOf(key)
	return len(ids) > 0 && !slices.ContainsFunc(ids, func(id string) bool { return !slices.Contains(st.Groups, id) })
}

// Path reports whether the page at the address path, in the view v, is one kept out: a repository's page, document or Markdown, or a
// group's page.
func (s *Set) Path(v *snapshot.Snapshot, path string) bool {
	rest, ok := strings.CutPrefix(path, "/repos/")
	if ok {
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) < 2 {
			return false
		}
		return s.Repo(v, snapshot.KeyOf(parts[0], parts[1]))
	}
	if rest, ok = strings.CutPrefix(path, "/groups/"); ok {
		id, _, _ := strings.Cut(rest, "/")
		return s.Group(id)
	}
	return false
}
