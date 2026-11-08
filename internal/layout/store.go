package layout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/cantara/source-code-portal/internal/docstore"
)

var (
	// ErrConflict is a save or reset based on a revision that is no longer the stored one.
	ErrConflict = docstore.ErrConflict
	// ErrReadOnly is a save or reset without a store.
	ErrReadOnly = docstore.ErrReadOnly
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

// Store is the layout store: one document per page in the store (internal/docstore), served from memory.
type Store struct {
	db    docstore.Backend // nil: built-in layouts only
	log   *slog.Logger
	files map[string]*docstore.Doc[Layout]
}

// Open loads every page's layout from db (nil: none, the store serves the built-in layouts and refuses writes).
func Open(ctx context.Context, db docstore.Backend, log *slog.Logger) *Store {
	s := &Store{db: db, log: log, files: map[string]*docstore.Doc[Layout]{}}
	for _, p := range pages {
		s.files[p] = docstore.Open(ctx, db, p, Default(p), Parse, log.With("layout", p))
	}
	return s
}

// Names are the documents' names: one per page, as the files layout.dir held were named.
func Names() []string { return slices.Clone(pages) }

// Writable reports whether layouts can be saved.
func (s *Store) Writable() bool { return s.db != nil }

// Get returns what the store holds for page; the built-in layout of an unknown page.
func (s *Store) Get(page string) Page {
	f, ok := s.files[page]
	if !ok {
		return Page{Layout: Default(page)}
	}
	st := f.Get()
	return Page{Layout: st.Value, Revision: st.Revision, Stored: st.Stored, Problem: st.Problem}
}

// Problems returns, per page, why its document is not served.
func (s *Store) Problems() []string {
	var out []string
	for _, p := range pages {
		if pr := s.files[p].Get().Problem; pr != "" {
			out = append(out, fmt.Sprintf("layout %s: %s; serving the last good layout", p, pr))
		}
	}
	return out
}

// Save stores l as page's layout when revision is the stored one (empty: none yet) and returns the new revision.
func (s *Store) Save(ctx context.Context, page string, l Layout, revision string) (string, error) {
	if s.db == nil {
		return "", ErrReadOnly
	}
	if err := l.Validate(); err != nil {
		return "", Invalid{err}
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return "", err
	}
	return s.files[page].Save(ctx, append(b, '\n'), revision)
}

// Reset removes page's layout when revision is the stored one, so the built-in layout renders again.
func (s *Store) Reset(ctx context.Context, page, revision string) error {
	return s.files[page].Remove(ctx, revision)
}
