// Package filestore keeps what an admin edits in the portal's pages as JSON files in one directory (core): the layouts, the
// announcements. Each file is served from memory, written through a temporary file renamed into place, guarded by a content-digest
// revision so a save based on an older read is refused, and read again when it changed on disk (by hand, by configuration
// management, by another instance sharing the directory). A file that no longer parses keeps its last good value with the problem.
package filestore

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
	"time"
)

// ReloadEvery is how often Watch looks for files changed on disk.
const ReloadEvery = 5 * time.Second

var (
	// ErrConflict is a save or removal based on a revision that is no longer the stored one.
	ErrConflict = errors.New("the file changed since it was read")
	// ErrReadOnly is a save or removal without a directory.
	ErrReadOnly = errors.New("no directory to save in")
)

// State is what a file holds.
type State[T any] struct {
	Value    T      // the last good value, else the fallback
	Revision string // the file's content digest; empty when there is no file
	Stored   bool   // Value is the file's, not the fallback
	Problem  string // why the file is not served; empty when it is
}

// File is one JSON file of the store.
type File[T any] struct {
	dir, name string
	fallback  T
	parse     func([]byte) (T, error)
	log       *slog.Logger
	stat      func(string) (fs.FileInfo, error)

	mu      sync.Mutex // one write at a time, and the fields below
	cur     State[T]
	mod     time.Time
	size    int64
	present bool
	unread  bool // the last stat or read failed: the next reload reads the file again, however it looks
}

// Open reads `<dir>/<name>.json` with parse; without a file (or without dir) it holds fallback. An empty dir is a store that only
// serves the fallback and refuses writes.
func Open[T any](dir, name string, fallback T, parse func([]byte) (T, error), log *slog.Logger) *File[T] {
	f := &File[T]{dir: dir, name: name, fallback: fallback, parse: parse, log: log, stat: os.Stat, cur: State[T]{Value: fallback}}
	f.Reload()
	return f
}

// Writable reports whether the file can be saved.
func (f *File[T]) Writable() bool { return f.dir != "" }

// Get returns what the file holds.
func (f *File[T]) Get() State[T] {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur
}

// Save writes b, which parse accepts, when revision is the stored one (empty: no file yet), and returns the new revision.
func (f *File[T]) Save(b []byte, revision string) (string, error) {
	if f.dir == "" {
		return "", ErrReadOnly
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur, err := f.revisionOnDisk(); err != nil {
		return "", err
	} else if cur != revision {
		return "", ErrConflict
	}
	if err := writeAtomic(f.path(), b); err != nil {
		return "", err
	}
	info, _ := f.stat(f.path())
	f.setLocked(b, info)
	return Digest(b), nil
}

// Remove deletes the file when revision is the stored one, so the fallback is served again.
func (f *File[T]) Remove(revision string) error {
	if f.dir == "" {
		return ErrReadOnly
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur, err := f.revisionOnDisk(); err != nil {
		return err
	} else if cur != revision {
		return ErrConflict
	}
	if err := os.Remove(f.path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f.cur, f.present = State[T]{Value: f.fallback}, false
	return nil
}

// Reload reads the file when it appeared, changed or went away since it was last read.
func (f *File[T]) Reload() {
	if f.dir == "" {
		return
	}
	info, err := f.stat(f.path())
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if f.present {
			f.cur, f.present = State[T]{Value: f.fallback}, false
			f.log.Info("file removed; the built-in value is served", "file", f.path())
		}
		return
	case err != nil:
		f.cur.Problem, f.unread = err.Error(), true
		return
	case f.present && !f.unread && info.ModTime().Equal(f.mod) && info.Size() == f.size:
		return
	}
	b, err := os.ReadFile(f.path())
	if err != nil {
		f.cur.Problem, f.unread = err.Error(), true
		return
	}
	f.setLocked(b, info) // the file as it was before it was read: a write in between is seen at the next reload
	if f.cur.Problem != "" {
		f.log.Warn("file not served", "file", f.path(), "err", f.cur.Problem)
	}
}

func (f *File[T]) path() string { return filepath.Join(f.dir, f.name+".json") }

// revisionOnDisk is the digest of the file as it is now, empty when there is none: another instance may have written it.
func (f *File[T]) revisionOnDisk() (string, error) {
	b, err := os.ReadFile(f.path())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	}
	return Digest(b), nil
}

// setLocked records the file's content b, read when the file was as info says: its value when it parses, else the last good value with the problem. The revision is
// b's either way, so an admin can save over a broken file.
func (f *File[T]) setLocked(b []byte, info fs.FileInfo) {
	f.present, f.unread = true, false
	if info != nil {
		f.mod, f.size = info.ModTime(), info.Size()
	}
	f.cur.Revision = Digest(b)
	v, err := f.parse(b)
	if err != nil {
		f.cur.Problem = err.Error()
		return
	}
	f.cur.Value, f.cur.Stored, f.cur.Problem = v, true, ""
}

// Reloader is a file Watch reads again.
type Reloader interface{ Reload() }

// Watch reloads the files every ReloadEvery until ctx ends.
func Watch(ctx context.Context, files ...Reloader) {
	t := time.NewTicker(ReloadEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, f := range files {
				f.Reload()
			}
		}
	}
}

// Digest is a file's revision: the first 128 bits of its SHA-256, in hex.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// writeAtomic writes b to path through a temporary file in the same directory, synced and renamed, so a reader never sees half.
func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".scp-*")
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
	if err := os.Chmod(f.Name(), 0o644); err != nil { //nolint:gosec // G302: what the pages show is not secret
		return err
	}
	return os.Rename(f.Name(), path)
}
