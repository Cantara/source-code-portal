package layout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/cantara/source-code-portal/internal/filestore"
)

var (
	// ErrConflict is a save or reset based on a revision that is no longer the stored one.
	ErrConflict = filestore.ErrConflict
	// ErrReadOnly is a save or reset without a layout directory.
	ErrReadOnly = filestore.ErrReadOnly
)

// Invalid is a layout the element catalogue refuses.
type Invalid struct{ error }

func (e Invalid) Unwrap() error { return e.error }

// Page is what the store holds for a page.
type Page struct {
	Layout   Layout // the last good layout, else the built-in one
	Revision string // the file's content digest; empty when there is no file
	Stored   bool   // Layout is an admin's, not the built-in one
	Problem  string // why the file is not served; empty when it is
}

// Store is the file-system layout store: `<dir>/<page>.json` per page (internal/filestore). It serves from memory and reloads a
// file that changed.
type Store struct {
	dir   string // empty: built-in layouts only
	log   *slog.Logger
	files map[string]*filestore.File[Layout]
}

// Open loads every page's layout from dir (empty: none, the store serves the built-in layouts and refuses writes).
func Open(dir string, log *slog.Logger) *Store {
	s := &Store{dir: dir, log: log, files: map[string]*filestore.File[Layout]{}}
	for _, p := range pages {
		s.files[p] = filestore.Open(dir, p, Default(p), Parse, log.With("layout", p))
	}
	return s
}

// Writable reports whether layouts can be saved.
func (s *Store) Writable() bool { return s.dir != "" }

// Get returns what the store holds for page; the built-in layout of an unknown page.
func (s *Store) Get(page string) Page {
	f, ok := s.files[page]
	if !ok {
		return Page{Layout: Default(page)}
	}
	st := f.Get()
	return Page{Layout: st.Value, Revision: st.Revision, Stored: st.Stored, Problem: st.Problem}
}

// Problems returns, per page, why its file is not served.
func (s *Store) Problems() []string {
	var out []string
	for _, p := range pages {
		if pr := s.files[p].Get().Problem; pr != "" {
			out = append(out, fmt.Sprintf("layout %s: %s; serving the last good layout", p, pr))
		}
	}
	return out
}

// Run reloads changed files until ctx ends.
func (s *Store) Run(ctx context.Context) {
	if s.dir == "" {
		return
	}
	files := make([]filestore.Reloader, 0, len(pages))
	for _, p := range pages {
		files = append(files, s.files[p])
	}
	filestore.Watch(ctx, files...)
}

// Save stores l as page's layout when revision is the stored one (empty: no file yet) and returns the new revision.
func (s *Store) Save(page string, l Layout, revision string) (string, error) {
	if s.dir == "" {
		return "", ErrReadOnly
	}
	if err := l.Validate(); err != nil {
		return "", Invalid{err}
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return "", err
	}
	return s.files[page].Save(append(b, '\n'), revision)
}

// Reset removes page's layout when revision is the stored one, so the built-in layout renders again.
func (s *Store) Reset(page, revision string) error { return s.files[page].Remove(revision) }

// reload reads page's file again when it changed.
func (s *Store) reload(page string) { s.files[page].Reload() }
