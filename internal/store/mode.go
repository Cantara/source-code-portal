package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
)

// Mode is what a store is for. A store belongs to one mode for good: the shared site's (scp serve), or one person's (scp personal,
// with the sealed credential of that person). The mode is written into the store (meta, key "mode") when it is first opened as one,
// and a store of the other mode is refused before anything is read, migrated or written: a personal store can never be served as a
// site, and a site's store can never be opened by scp personal, whatever the configuration or the directory says.
type Mode string

// The two modes.
const (
	Shared   Mode = "shared"
	Personal Mode = "personal"
)

const modeKey = "mode"

// ModeError says the store at Path belongs to another mode than the one that asked for it.
type ModeError struct {
	Path       string
	Have, Want Mode
	Why        string
}

func (e *ModeError) Error() string {
	return fmt.Sprintf("store %s is %s's and cannot be opened by the %s mode: %s", e.Path, e.Have, e.Want, e.Why)
}

// OpenAs is Open for a mode: the store's mode must be mode, or none yet (a store from before the mode was written is taken when
// what it holds fits: a shared site's store holds no sealed credential; a personal one does, or is empty). Every part of the program
// that opens the store for use goes through it; Open, which claims nothing, is for tests and tools.
func OpenAs(ctx context.Context, path string, mode Mode, log *slog.Logger) (*DB, error) {
	if mode != Shared && mode != Personal {
		return nil, fmt.Errorf("store: %q is not a mode", mode)
	}
	return openFor(ctx, path, mode, log)
}

// claim decides whether a store, as it was found, may be opened for want; claim is true when its mode has still to be written.
func claim(path string, have Mode, credentials, catalogue bool, want Mode) (claim bool, err error) {
	switch {
	case have == want:
		return false, nil
	case have != "":
		return false, &ModeError{Path: path, Have: have, Want: want, Why: "it was made for that mode; give this one a store of its own"}
	case want == Shared && credentials:
		return false, &ModeError{Path: path, Have: Personal, Want: want, Why: "it holds a sealed credential, which only scp personal writes"}
	case want == Personal && catalogue && !credentials:
		return false, &ModeError{Path: path, Have: Shared, Want: want, Why: "it holds a catalogue and no credential of scp personal: it looks like a shared site's"}
	}
	return true, nil
}

// peek looks at the file at path read-only: a file that is not there yet is empty.
func peek(ctx context.Context, path string) (have Mode, credentials, catalogue bool, err error) {
	if _, serr := os.Stat(path); errors.Is(serr, fs.ErrNotExist) {
		return "", false, false, nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", false, false, err
	}
	defer func() { _ = db.Close() }()
	return inspect(ctx, db)
}

// inspect reads what decides a store's mode, touching nothing; a table that is not there yet (a new file, an old schema) is empty.
func inspect(ctx context.Context, db *sql.DB) (have Mode, credentials, catalogue bool, err error) {
	has := func(table string) (bool, error) {
		var n int
		err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&n)
		return n > 0, err
	}
	holds := func(table string) (bool, error) {
		if ok, err := has(table); err != nil || !ok {
			return false, err
		}
		var found bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM "+table+")").Scan(&found)
		return found, err
	}
	if ok, err := has("meta"); err != nil {
		return "", false, false, err
	} else if ok {
		var v sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", modeKey).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", false, false, err
		}
		have = Mode(v.String)
	}
	if credentials, err = holds("credential"); err != nil {
		return "", false, false, err
	}
	for _, t := range []string{"repo", "item"} {
		if found, err := holds(t); err != nil {
			return "", false, false, err
		} else if found {
			catalogue = true
		}
	}
	return have, credentials, catalogue, nil
}
