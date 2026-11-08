// Package docstore keeps what an admin edits in the portal's pages (core): the layouts, the announcements. Each document is a JSON
// text under a name in the store (SQLite, the one place data lives), served from memory, and written through to the store before a
// save returns, so a process that dies loses nothing it acknowledged. A save names the revision (the content digest) it read: one
// based on an older read is refused. A document that no longer parses keeps its last good value, with the problem.
package docstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/cantara/source-code-portal/internal/store"
)

var (
	// ErrConflict is a save or removal based on a revision that is no longer the stored one.
	ErrConflict = errors.New("the document changed since it was read")
	// ErrReadOnly is a save or removal without a store.
	ErrReadOnly = errors.New("no store to save in")
)

// Backend is where documents are kept: the store.
type Backend interface {
	Document(ctx context.Context, name string) (body []byte, revision string, ok bool, err error)
	PutDocument(ctx context.Context, name string, body []byte, revision, was string) error
	DeleteDocument(ctx context.Context, name, was string) error
}

// State is what a document holds.
type State[T any] struct {
	Value    T      // the last good value, else the fallback
	Revision string // the content digest; empty when nothing is stored
	Stored   bool   // Value is the stored one, not the fallback
	Problem  string // why the stored text is not served; empty when it is
}

// Doc is one document.
type Doc[T any] struct {
	db       Backend
	name     string
	fallback T
	parse    func([]byte) (T, error)

	mu  sync.Mutex // one save at a time, and cur
	cur State[T]
}

// Open reads the document called name with parse; without one it holds fallback. A nil db is a document that only serves the
// fallback and refuses writes.
func Open[T any](ctx context.Context, db Backend, name string, fallback T, parse func([]byte) (T, error), log *slog.Logger) *Doc[T] {
	d := &Doc[T]{db: db, name: name, fallback: fallback, parse: parse, cur: State[T]{Value: fallback}}
	if db == nil {
		return d
	}
	b, rev, ok, err := db.Document(ctx, name)
	switch {
	case err != nil:
		log.Error("document not read; the built-in value is served", "document", name, "err", err)
		d.cur.Problem = err.Error()
	case ok:
		d.set(b, rev)
		if d.cur.Problem != "" {
			log.Warn("document not served", "document", name, "err", d.cur.Problem)
		}
	}
	return d
}

// Writable reports whether the document can be saved.
func (d *Doc[T]) Writable() bool { return d.db != nil }

// Get returns what the document holds.
func (d *Doc[T]) Get() State[T] {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cur
}

// Save stores b, which parse accepts, when revision is the stored one (empty: nothing stored yet) and returns the new revision. The
// write is on disk when it returns.
func (d *Doc[T]) Save(ctx context.Context, b []byte, revision string) (string, error) {
	if d.db == nil {
		return "", ErrReadOnly
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rev := Digest(b)
	if err := d.db.PutDocument(ctx, d.name, b, rev, revision); err != nil {
		return "", conflict(err)
	}
	d.set(b, rev)
	return rev, nil
}

// Remove deletes the document when revision is the stored one, so the fallback is served again.
func (d *Doc[T]) Remove(ctx context.Context, revision string) error {
	if d.db == nil {
		return ErrReadOnly
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.db.DeleteDocument(ctx, d.name, revision); err != nil {
		return conflict(err)
	}
	d.cur = State[T]{Value: d.fallback}
	return nil
}

func conflict(err error) error {
	if errors.Is(err, store.ErrRevision) {
		return ErrConflict
	}
	return err
}

// set records the stored text b: its value when it parses, else the last good value with the problem. The revision is the stored
// one either way, so an admin can save over a broken document. Callers hold d.mu (or are Open).
func (d *Doc[T]) set(b []byte, revision string) {
	d.cur.Revision = revision
	v, err := d.parse(b)
	if err != nil {
		d.cur.Problem = err.Error()
		return
	}
	d.cur.Value, d.cur.Stored, d.cur.Problem = v, true, ""
}

// Digest is a document's revision: the first 128 bits of its SHA-256, in hex.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// Import copies `<dir>/<name>.json` into the store for each name that has such a file and no stored document: what administrators
// kept in files (layout.dir) before documents lived in the store. Once imported the store is the truth and the files are left alone.
// It returns the names it imported.
func Import(ctx context.Context, db Backend, dir string, names []string) ([]string, error) {
	if db == nil || dir == "" {
		return nil, nil
	}
	var done []string
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n+".json")) //nolint:gosec // G304: the operator's own directory
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return done, err
		}
		if _, _, ok, err := db.Document(ctx, n); err != nil || ok {
			if err != nil {
				return done, err
			}
			continue
		}
		if err := db.PutDocument(ctx, n, b, Digest(b), ""); err != nil && !errors.Is(err, store.ErrRevision) {
			return done, err
		}
		done = append(done, n)
	}
	return done, nil
}
