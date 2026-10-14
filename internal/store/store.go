// Package store keeps the last good state in SQLite so a restart serves data before the first GitHub call. It is a cache:
// deleting the file costs a resync, never data. A file that is corrupt, fails to migrate or has a newer schema is moved aside and the
// store starts empty. The sync engine is the only writer.
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
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Repo is a catalogued repository: its address and identity.
type Repo struct {
	Key snapshot.RepoKey
	ID  snapshot.RepoID
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
	db  *sql.DB
	log *slog.Logger
}

// Open opens or creates the store at path. A file that cannot be used is quarantined next to path and a new one created.
func Open(ctx context.Context, path string, log *slog.Logger) (*DB, error) {
	db, err := open(ctx, path)
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
	db, err = open(ctx, path)
	if err != nil {
		return nil, err
	}
	return &DB{db: db, log: log}, nil
}

// unusableError marks a file that exists but cannot be used as a store.
type unusableError struct{ err error }

func (e unusableError) Error() string { return e.err.Error() }
func (e unusableError) Unwrap() error { return e.err }

func open(ctx context.Context, path string) (*sql.DB, error) {
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
	if err := check(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func check(ctx context.Context, db *sql.DB) error {
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return unusableError{fmt.Errorf("quick_check: %w", err)}
	}
	if res != "ok" {
		return unusableError{fmt.Errorf("quick_check: %s", res)}
	}
	if err := migrate(ctx, db); err != nil {
		return unusableError{err}
	}
	return nil
}

// migrate applies the embedded migrations NNNN_name.sql after PRAGMA user_version, each in one transaction with its version bump.
func migrate(ctx context.Context, db *sql.DB) error {
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
	rows, err := s.db.QueryContext(ctx, "SELECT key, source, github_id FROM repo ORDER BY key")
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.Key, &r.ID.Source, &r.ID.GitHubID); err != nil {
			_ = rows.Close()
			return nil, nil, err
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
		if err := replaceCatalogue(ctx, tx, c.Source, c.Catalogue); err != nil {
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

func replaceCatalogue(ctx context.Context, tx *sql.Tx, source string, repos []Repo) error {
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
		// An address taken by a repository with another identity (deleted and recreated, or a transfer between sources) moves over.
		if _, err := tx.ExecContext(ctx, "DELETE FROM repo WHERE key = ? AND NOT (source = ? AND github_id = ?)", r.Key, source, r.ID.GitHubID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO repo (key, source, github_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			r.Key, source, r.ID.GitHubID); err != nil {
			return err
		}
	}
	return nil
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
