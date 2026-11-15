package layout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/cantara/source-code-portal/internal/docstore"
)

// Invalid is a layout the element catalogue refuses.
type Invalid struct{ error }

func (e Invalid) Unwrap() error { return e.error }

// Page is what the store holds for a page.
type Page struct {
	Layout   Layout // the layout in force: an admin's change, else the configuration's, else the built-in one
	Revision string // the stored change's content digest; empty when nothing is stored
	Stored   bool   // Layout is an admin's change, kept only in the store
	// Configured is true when the configuration's `layouts:` has the page: a reset returns to it, else to the built-in one.
	Configured bool
	Problem    string // why the stored change is not served; empty when it is
}

// Store is the pages' layouts: the configuration's `layouts:` (else the built-in layout) as the base and an admin's change per page,
// one document per page in the store (internal/docstore, design/config-baseline.md), served from memory.
type Store struct {
	log        *slog.Logger
	configured map[string]bool
	pages      map[string]*docstore.Overlay[Layout, Layout]
}

// Open loads every page's change from db (nil: none, the store serves the base and refuses writes) over base, the configuration's
// layouts by page.
func Open(ctx context.Context, db docstore.Backend, base map[string]Layout, log *slog.Logger) *Store {
	s := &Store{log: log, configured: map[string]bool{}, pages: map[string]*docstore.Overlay[Layout, Layout]{}}
	ops := docstore.Ops[Layout, Layout]{Merge: merge, Diff: diff, Count: count, Parse: parseChange}
	for _, p := range pages {
		b, ok := base[p]
		if !ok {
			b = Default(p)
		}
		s.configured[p] = ok
		s.pages[p] = docstore.OpenOverlay(ctx, db, p, b, ops, log.With("layout", p))
	}
	return s
}

// A page's stored change is a whole layout; the empty layout (no elements, which no page may have) is no change.

func merge(base, change Layout) Layout {
	if change.Elements == nil {
		return base
	}
	return change
}

func diff(base, l Layout) Layout {
	if docstore.Same(base, l) {
		return Layout{}
	}
	return l
}

func count(change Layout) int {
	if change.Elements == nil {
		return 0
	}
	return 1
}

// parseChange reads a stored change: a layout Parse accepts, or no change (the empty layout as stored).
func parseChange(b []byte) (Layout, error) {
	var c bytes.Buffer
	if json.Compact(&c, b) == nil && (c.String() == `{"elements":null}` || c.String() == `{}`) {
		return Layout{}, nil
	}
	return Parse(b)
}

// Names are the documents' names: one per page, as the files layout.dir held were named.
func Names() []string { return slices.Clone(pages) }

// Writable reports whether layouts can be saved.
func (s *Store) Writable() bool { return s.pages[pages[0]].Writable() }

// Get returns what the store holds for page; the built-in layout of an unknown page.
func (s *Store) Get(page string) Page {
	o, ok := s.pages[page]
	if !ok {
		return Page{Layout: Default(page)}
	}
	st := o.State()
	return Page{Layout: o.Value(), Revision: st.Revision, Stored: o.Changes() > 0, Configured: s.configured[page], Problem: st.Problem}
}

// Problems returns, per page, why its stored change is not served.
func (s *Store) Problems() []string {
	var out []string
	for _, p := range pages {
		if pr := s.pages[p].State().Problem; pr != "" {
			out = append(out, fmt.Sprintf("layout %s: %s; serving the last good layout", p, pr))
		}
	}
	return out
}

// Changes is how many pages have a layout kept only in the store, not in the configuration.
func (s *Store) Changes() int {
	n := 0
	for _, p := range pages {
		n += s.pages[p].Changes()
	}
	return n
}

// Save stores l as page's layout when revision is the stored one (empty: none yet) and returns the new revision; the configuration's
// layout stores no change.
func (s *Store) Save(ctx context.Context, page string, l Layout, revision string) (string, error) {
	if !s.Writable() {
		return "", docstore.ErrReadOnly
	}
	if err := l.Validate(); err != nil {
		return "", Invalid{err}
	}
	return s.pages[page].Save(ctx, l, revision)
}

// Reset drops page's change when revision is the stored one, so the configuration's layout, else the built-in one, renders again.
func (s *Store) Reset(ctx context.Context, page, revision string) error {
	if !s.Writable() {
		return docstore.ErrReadOnly
	}
	_, err := s.pages[page].Save(ctx, s.pages[page].Base(), revision)
	return err
}
