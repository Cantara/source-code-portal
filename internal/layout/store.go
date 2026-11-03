package layout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// reloadEvery is how often the store looks for files changed by hand, by configuration management or by another instance.
const reloadEvery = 5 * time.Second

var (
	// ErrConflict is a save or reset based on a revision that is no longer the stored one.
	ErrConflict = errors.New("the layout changed since it was read")
	// ErrReadOnly is a save or reset without a layout directory.
	ErrReadOnly = errors.New("no layout.dir: the built-in layouts only")
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

type entry struct {
	page    Page
	mod     time.Time
	size    int64
	present bool
}

// Store is the file-system layout store: `<dir>/<page>.json`, each written to a temporary file and renamed into place. It serves
// from memory and reloads a file that changed.
type Store struct {
	dir  string // empty: built-in layouts only
	log  *slog.Logger
	mu   sync.Mutex // one write at a time, and the entries
	cur  map[string]entry
	stat func(string) (fs.FileInfo, error)
}

// Open loads every page's layout from dir (empty: none, the store serves the built-in layouts and refuses writes).
func Open(dir string, log *slog.Logger) *Store {
	s := &Store{dir: dir, log: log, cur: map[string]entry{}, stat: os.Stat}
	for _, p := range pages {
		s.cur[p] = entry{page: Page{Layout: Default(p)}}
		s.reload(p)
	}
	return s
}

// Writable reports whether layouts can be saved.
func (s *Store) Writable() bool { return s.dir != "" }

// Get returns what the store holds for page; the built-in layout of an unknown page.
func (s *Store) Get(page string) Page {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.cur[page]; ok {
		return e.page
	}
	return Page{Layout: Default(page)}
}

// Problems returns, per page, why its file is not served.
func (s *Store) Problems() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, p := range pages {
		if pr := s.cur[p].page.Problem; pr != "" {
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
	t := time.NewTicker(reloadEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, p := range pages {
				s.reload(p)
			}
		}
	}
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
	b = append(b, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, err := s.revisionOnDisk(page); err != nil {
		return "", err
	} else if cur != revision {
		return "", ErrConflict
	}
	if err := writeAtomic(s.path(page), b); err != nil {
		return "", err
	}
	s.setLocked(page, b)
	return digest(b), nil
}

// Reset removes page's layout when revision is the stored one, so the built-in layout renders again.
func (s *Store) Reset(page, revision string) error {
	if s.dir == "" {
		return ErrReadOnly
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, err := s.revisionOnDisk(page); err != nil {
		return err
	} else if cur != revision {
		return ErrConflict
	}
	if err := os.Remove(s.path(page)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	s.cur[page] = entry{page: Page{Layout: Default(page)}}
	return nil
}

func (s *Store) path(page string) string { return filepath.Join(s.dir, page+".json") }

// revisionOnDisk is the digest of page's file as it is now, empty when there is none: another instance may have written it.
func (s *Store) revisionOnDisk(page string) (string, error) {
	b, err := os.ReadFile(s.path(page))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	}
	return digest(b), nil
}

// reload reads page's file when it appeared, changed or went away since it was last read.
func (s *Store) reload(page string) {
	if s.dir == "" {
		return
	}
	info, err := s.stat(s.path(page))
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.cur[page]
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if old.present {
			s.cur[page] = entry{page: Page{Layout: Default(page)}}
			s.log.Info("layout removed; the built-in layout renders", "page", page)
		}
		return
	case err != nil:
		old.page.Problem = err.Error()
		s.cur[page] = old
		return
	case old.present && info.ModTime().Equal(old.mod) && info.Size() == old.size:
		return
	}
	b, err := os.ReadFile(s.path(page))
	if err != nil {
		old.page.Problem = err.Error()
		s.cur[page] = old
		return
	}
	s.setLocked(page, b)
	if e := s.cur[page]; e.page.Problem != "" {
		s.log.Warn("layout not served", "page", page, "file", s.path(page), "err", e.page.Problem)
	}
}

// setLocked records page's file content b: its layout when it parses, else the last good layout with the problem. The file's
// revision is b's either way, so an admin can save over a broken file.
func (s *Store) setLocked(page string, b []byte) {
	e := s.cur[page]
	e.present = true
	if info, err := s.stat(s.path(page)); err == nil {
		e.mod, e.size = info.ModTime(), info.Size()
	}
	e.page.Revision = digest(b)
	l, err := Parse(b)
	if err != nil {
		e.page.Problem = err.Error()
	} else {
		e.page.Layout, e.page.Stored, e.page.Problem = l, true, ""
	}
	s.cur[page] = e
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// writeAtomic writes b to path through a temporary file in the same directory, synced and renamed, so a reader never sees half.
func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".layout-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // gone after the rename
	if _, err := f.Write(b); err != nil {
		f.Close() //nolint:gosec // the write failed already
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close() //nolint:gosec // the sync failed already
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil { //nolint:gosec // G302: a layout is not secret
		return err
	}
	return os.Rename(f.Name(), path)
}
