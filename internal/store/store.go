// Package store keeps everything the portal holds on to in SQLite, so a restart (or a crash) serves it before the first GitHub call:
// the catalogue, the documents administrators save, sealed credentials, parked webhook events. Memory only loads it. The catalogue part
// is read again from GitHub when the file is lost; the rest is data (back the file up), and an acknowledged write of it is synced to
// disk before it returns (durably). A file that is corrupt, fails to migrate or has a newer schema is moved aside (the way back is its
// copy) and the store starts empty.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Repo is a catalogued repository: its address and identity, and its visibility as its source last listed it ("" when not known).
type Repo struct {
	Key        snapshot.RepoKey
	ID         snapshot.RepoID
	Visibility string
}

// Item is the stored state of one sync job key.
type Item struct {
	Key        string
	Kind       string
	Repo       snapshot.RepoKey // empty for source jobs
	State      snapshot.State
	FetchedAt  time.Time
	CheckedAt  time.Time
	NextDue    time.Time
	Attempts   int
	ErrCode    string
	ContentSHA string
	Payload    []byte
}

// Change is one job outcome, written in one transaction.
type Change struct {
	Item Item
	// Catalogue, when not nil, replaces the repositories of Source. Repositories that left, and repositories whose address changed,
	// are deleted with their items.
	Source    string
	Catalogue []Repo
}

// DB is the store.
type DB struct {
	db    *sql.DB
	log   *slog.Logger
	docMu sync.Mutex // one document save at a time: the revision check and the write are one step
}

// Open opens or creates the store at path. A file that cannot be used is quarantined next to path and a new one created.
func Open(ctx context.Context, path string, log *slog.Logger) (*DB, error) {
	db, err := open(ctx, path, log)
	if err == nil {
		return &DB{db: db, log: log}, nil
	}
	var unusable unusableError
	if !errors.As(err, &unusable) {
		return nil, err
	}
	moved, qerr := quarantine(path, time.Now())
	if qerr != nil {
		return nil, errors.Join(err, qerr)
	}
	log.Warn("store unusable; moved aside, starting empty", "err", err, "moved_to", moved)
	db, err = open(ctx, path, log)
	if err != nil {
		return nil, err
	}
	return &DB{db: db, log: log}, nil
}

// unusableError marks a file that exists but cannot be used as a store.
type unusableError struct{ err error }

func (e unusableError) Error() string { return e.err.Error() }
func (e unusableError) Unwrap() error { return e.err }

func open(ctx context.Context, path string, log *slog.Logger) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "synchronous(NORMAL)", "busy_timeout(5000)", "foreign_keys(1)", "temp_store(MEMORY)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; handlers never read the store
	if err := check(ctx, db, path, log); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func check(ctx context.Context, db *sql.DB, path string, log *slog.Logger) error {
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return unusableError{fmt.Errorf("quick_check: %w", err)}
	}
	if res != "ok" {
		return unusableError{fmt.Errorf("quick_check: %s", res)}
	}
	if err := migrate(ctx, db, path, log); err != nil {
		return unusableError{err}
	}
	return nil
}

// migrate applies the embedded migrations NNNN_name.sql after PRAGMA user_version, each in one transaction with its version bump. A
// store that holds data is copied first (backup): a binary older than the schema quarantines the file and starts empty, so the copy is
// the way back.
func migrate(ctx context.Context, db *sql.DB, path string, log *slog.Logger) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(names) {
		return fmt.Errorf("schema version %d is newer than this binary (%d)", current, len(names))
	}
	for i, name := range names {
		n, err := strconv.Atoi(strings.SplitN(filepath.Base(name), "_", 2)[0])
		if err != nil || n != i+1 {
			return fmt.Errorf("migration %s: want number %d", name, i+1)
		}
		if n <= current {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if current > 0 {
			backup(ctx, db, path, n, log)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// backup copies the store as it is before migration n to path.pre-NNNN (VACUUM INTO: a consistent copy of a store in use, about 50 ms
// for 27 MB), once: a copy that exists is kept. A failure is logged and the migration goes on; the migrations are additive and run in
// one transaction, and the store is a cache.
func backup(ctx context.Context, db *sql.DB, path string, n int, log *slog.Logger) {
	if path == "" {
		return
	}
	dst := fmt.Sprintf("%s.pre-%04d", path, n)
	if _, err := os.Stat(dst); err == nil {
		return
	}
	// The copy holds sealed credentials and the documents: create it private before anything is written (SQLite fills an empty file).
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: next to the store, named by us
	if err != nil {
		log.Warn("store: no copy before the migration", "migration", n, "err", err)
		return
	}
	_ = f.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		log.Warn("store: no copy before the migration", "migration", n, "err", err)
		_ = os.Remove(dst)
		return
	}
	_ = os.Chmod(dst, 0o600)
	log.Info("store: copied before the migration", "migration", n, "copy", dst)
}

// quarantine renames path and its -wal and -shm files to path.quarantine-<UTC>, keeping the newest two quarantined copies.
func quarantine(path string, now time.Time) (string, error) {
	dst := path + ".quarantine-" + now.UTC().Format("20060102T150405Z")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(path+suffix, dst+suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	old, _ := filepath.Glob(path + ".quarantine-*")
	var bases []string
	for _, o := range old {
		if !strings.HasSuffix(o, "-wal") && !strings.HasSuffix(o, "-shm") {
			bases = append(bases, o)
		}
	}
	slices.Sort(bases)
	for len(bases) > 2 {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(bases[0] + suffix)
		}
		bases = bases[1:]
	}
	return dst, nil
}

// Close checkpoints the WAL and closes the store.
func (s *DB) Close() error {
	ctx := context.Background()
	_, err1 := s.db.ExecContext(ctx, "PRAGMA optimize")
	_, err2 := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(err1, err2, s.db.Close())
}

// Load returns every repository and item, for the warm start.
func (s *DB) Load(ctx context.Context) ([]Repo, []Item, error) {
	var repos []Repo
	rows, err := s.db.QueryContext(ctx, "SELECT key, source, github_id, visibility FROM repo ORDER BY key")
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.Key, &r.ID.Source, &r.ID.GitHubID, &r.Visibility); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		if r.Visibility == "unknown" {
			r.Visibility = ""
		}
		repos = append(repos, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, err
	}

	var items []Item
	rows, err = s.db.QueryContext(ctx, `SELECT key, kind, coalesce(repo_key, ''), status, coalesce(fetched_at, 0), coalesce(checked_at, 0),
		coalesce(next_due_at, 0), attempts, coalesce(error, ''), coalesce(content_sha, ''), payload FROM item ORDER BY key`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var it Item
		var fetched, checked, due int64
		if err := rows.Scan(&it.Key, &it.Kind, &it.Repo, &it.State, &fetched, &checked, &due, &it.Attempts, &it.ErrCode,
			&it.ContentSHA, &it.Payload); err != nil {
			return nil, nil, err
		}
		it.FetchedAt, it.CheckedAt, it.NextDue = fromUnix(fetched), fromUnix(checked), fromUnix(due)
		items = append(items, it)
	}
	return repos, items, rows.Err()
}

// Apply writes one job outcome. An item of a repository that is no longer catalogued is not written.
func (s *DB) Apply(ctx context.Context, c Change) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if c.Catalogue != nil {
		if err := replaceCatalogue(ctx, tx, s.log, c.Source, c.Catalogue); err != nil {
			return err
		}
	}
	it := c.Item
	var repoKey any
	if it.Repo != "" {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM repo WHERE key = ?", it.Repo).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return tx.Commit()
		}
		repoKey = string(it.Repo)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO item (key, kind, repo_key, status, fetched_at, checked_at, next_due_at, attempts, error, content_sha, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, nullif(?, ''), nullif(?, ''), ?)
		ON CONFLICT (key) DO UPDATE SET kind = excluded.kind, repo_key = excluded.repo_key, status = excluded.status,
			fetched_at = excluded.fetched_at, checked_at = excluded.checked_at, next_due_at = excluded.next_due_at,
			attempts = excluded.attempts, error = excluded.error, content_sha = excluded.content_sha, payload = excluded.payload`,
		it.Key, it.Kind, repoKey, string(it.State), toUnix(it.FetchedAt), toUnix(it.CheckedAt), toUnix(it.NextDue), it.Attempts,
		it.ErrCode, it.ContentSHA, it.Payload)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func replaceCatalogue(ctx context.Context, tx *sql.Tx, log *slog.Logger, source string, repos []Repo) error {
	ids := map[int64]snapshot.RepoKey{}
	for _, r := range repos {
		ids[r.ID.GitHubID] = r.Key
	}
	rows, err := tx.QueryContext(ctx, "SELECT key, github_id FROM repo WHERE source = ?", source)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var key string
		var id int64
		if err := rows.Scan(&key, &id); err != nil {
			_ = rows.Close()
			return err
		}
		if cur, ok := ids[id]; !ok || string(cur) != key {
			stale = append(stale, key) // left the catalogue, or renamed: items are refetched under the new address
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, key := range stale {
		if _, err := tx.ExecContext(ctx, "DELETE FROM repo WHERE key = ?", key); err != nil {
			return err
		}
	}
	for _, r := range repos {
		var holder string
		switch err := tx.QueryRowContext(ctx, "SELECT source FROM repo WHERE key = ?", r.Key).Scan(&holder); {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		case holder != source && !snapshot.Outranks(source, holder):
			// Another source owns the address: this one is not stored (and is told in the log), never swapped in. Two sources that
			// list one address used to take it from each other at every listing and delete its items each time.
			log.Warn("catalogue: address held by another source; not stored", "repo", r.Key, "source", source, "held_by", holder)
			continue
		}
		// An address held by another identity (deleted and recreated, or a source that ranks lower) moves over, with its items.
		if _, err := tx.ExecContext(ctx, "DELETE FROM repo WHERE key = ? AND NOT (source = ? AND github_id = ?)", r.Key, source, r.ID.GitHubID); err != nil {
			return err
		}
		// A listing that does not say (a follower applying an older leader's outcome) keeps what is known.
		if _, err := tx.ExecContext(ctx, `INSERT INTO repo (key, source, github_id, visibility) VALUES (?, ?, ?, ?)
			ON CONFLICT (key) DO UPDATE SET visibility = CASE WHEN excluded.visibility = 'unknown' THEN repo.visibility ELSE excluded.visibility END`,
			r.Key, source, r.ID.GitHubID, visibilityOf(r.Visibility)); err != nil {
			return err
		}
	}
	return nil
}

// visibilityOf is v as the column holds it: one of the listed values, else unknown.
func visibilityOf(v string) string {
	switch v {
	case "public", "private", "internal":
		return v
	}
	return "unknown"
}

// Meta returns the value stored under key, "" when there is none.
func (s *DB) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetMeta stores value under key.
func (s *DB) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// RetainSources deletes repositories and items of sources that are no longer configured, and items of unknown kinds.
func (s *DB) RetainSources(ctx context.Context, sources, kinds []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "DELETE FROM repo WHERE source NOT IN (SELECT value FROM json_each(?))", jsonList(sources)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM item WHERE kind NOT IN (SELECT value FROM json_each(?))", jsonList(kinds)); err != nil {
		return err
	}
	// Source jobs are keyed "<kind>/<source>".
	if _, err := tx.ExecContext(ctx, `DELETE FROM item WHERE repo_key IS NULL
		AND substr(key, instr(key, '/') + 1) NOT IN (SELECT value FROM json_each(?))`, jsonList(sources)); err != nil {
		return err
	}
	return tx.Commit()
}

func jsonList(ss []string) string {
	if ss == nil {
		ss = []string{}
	}
	b, _ := json.Marshal(ss) // a []string always marshals
	return string(b)
}

func toUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func fromUnix(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}
