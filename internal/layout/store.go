package layout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

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
	Stored   bool   // Layout is an admin's change, kept in the store
	// Configured is true when the configuration's `layouts:` has the page: a reset returns to it, else to the built-in one.
	Configured bool
	Problem    string // why the stored document is not served; empty when it is
}

// Store is the pages' layouts: the configuration's `layouts:` (the base, else the built-in layout) and an admin's change per page,
// one document per page in the store (internal/docstore, design/config-baseline.md), served from memory.
type Store struct {
	db    docstore.Backend // nil: the base only
	log   *slog.Logger
	base  map[string]Layout
	files map[string]*docstore.Doc[Layout]
}

// Open loads every page's change from db (nil: none, the store serves the base and refuses writes) over base, the configuration's
// layouts by page. A change equal to the configuration's layout is dropped, so a configuration downloaded and deployed leaves the
// store without changes of its own.
func Open(ctx context.Context, db docstore.Backend, base map[string]Layout, log *slog.Logger) *Store {
	s := &Store{db: db, log: log, base: base, files: map[string]*docstore.Doc[Layout]{}}
	for _, p := range pages {
		s.files[p] = docstore.Open(ctx, db, p, s.fallback(p), Parse, log.With("layout", p))
		if st := s.files[p].Get(); st.Stored && st.Problem == "" && s.configured(p, st.Value) {
			if err := s.files[p].Remove(ctx, st.Revision); err != nil {
				log.Error("the layout equal to the configuration's was not dropped", "page", p, "err", err)
			}
		}
	}
	return s
}

// fallback is page's layout without a change: the configuration's, else the built-in one.
func (s *Store) fallback(page string) Layout {
	if l, ok := s.base[page]; ok {
		return l
	}
	return Default(page)
}

// configured reports whether l is the configuration's layout of page.
func (s *Store) configured(page string, l Layout) bool {
	b, ok := s.base[page]
	if !ok {
		return false
	}
	x, _ := json.Marshal(b) //nolint:errchkjson // plain values
	y, _ := json.Marshal(l) //nolint:errchkjson // plain values
	return bytes.Equal(x, y)
}

// Base is the configuration's layouts by page.
func (s *Store) Base() map[string]Layout { return s.base }

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
	_, configured := s.base[page]
	return Page{Layout: st.Value, Revision: st.Revision, Stored: st.Stored, Configured: configured, Problem: st.Problem}
}

// Problems returns, per page, why its document is not served, and the pages whose layout is kept only in the store.
func (s *Store) Problems() []string {
	var out, changed []string
	for _, p := range pages {
		switch st := s.files[p].Get(); {
		case st.Problem != "":
			out = append(out, fmt.Sprintf("layout %s: %s; serving the last good layout", p, st.Problem))
		case st.Stored:
			changed = append(changed, p)
		}
	}
	if len(changed) > 0 {
		out = append(out, fmt.Sprintf("layouts: %s kept only in the store, not in the configuration; download scp.yaml from Admin to keep them",
			strings.Join(changed, ", ")))
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
	if s.configured(page, l) {
		// The configuration's layout: nothing of its own to keep.
		if st := s.files[page].Get(); st.Stored {
			return "", s.files[page].Remove(ctx, revision)
		}
		return "", nil
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return "", err
	}
	return s.files[page].Save(ctx, append(b, '\n'), revision)
}

// Reset removes page's change when revision is the stored one, so the configuration's layout, else the built-in one, renders again.
func (s *Store) Reset(ctx context.Context, page, revision string) error {
	return s.files[page].Remove(ctx, revision)
}
