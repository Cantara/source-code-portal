package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// appID is PRAGMA application_id: "SCP1".
const appID = 1396790321

var (
	// ErrSchemaNewer: the file was written by a newer build (application rollback). Remedy: quarantine and cold start.
	ErrSchemaNewer = errors.New("sqlite: database schema is newer than this build")
	// ErrForeignDatabase: a SQLite file that is not ours sits at the path. Never renamed or overwritten.
	ErrForeignDatabase = errors.New("sqlite: path holds a foreign SQLite database (application_id mismatch)")
	// ErrVersionGuard: the linked SQLite is older than 3.51.3 (WAL-reset corruption bug in 3.7.0..3.51.2).
	ErrVersionGuard = errors.New("sqlite: SQLite older than 3.51.3 refused (WAL-reset corruption bug)")
)

type migration struct {
	Version int
	Name    string
	SQL     string
	FKOff   bool // "-- scp:foreign_keys=off" on the first line: table-rebuild migration (12-step procedure)
}

var migName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// loadMigrations reads NNNN_name.sql from fsys, requires a contiguous sequence from 0001, and returns them sorted.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range ents {
		m := migName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration file %q does not match NNNN_name.sql", e.Name())
		}
		b, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		v, _ := strconv.Atoi(m[1])
		ms = append(ms, migration{Version: v, Name: m[2], SQL: string(b), FKOff: strings.HasPrefix(string(b), "-- scp:foreign_keys=off")})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Version < ms[j].Version })
	for i, m := range ms {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrations must be contiguous from 0001: found %04d at position %d", m.Version, i+1)
		}
	}
	return ms, nil
}

// MigrateResult describes what the runner did.
type MigrateResult struct{ From, To, Applied int }

// migrate applies pending migrations, one transaction each (the DSN's _txlock=immediate makes it BEGIN IMMEDIATE), the
// script and PRAGMA user_version together, so a failed migration leaves user_version and the schema untouched.
func migrate(ctx context.Context, db *sql.DB, ms []migration) (MigrateResult, error) {
	var cur int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&cur); err != nil {
		return MigrateResult{}, err
	}
	res := MigrateResult{From: cur, To: cur}
	if cur > len(ms) {
		return res, fmt.Errorf("%w: file is v%d, build knows v%d", ErrSchemaNewer, cur, len(ms))
	}
	for _, m := range ms[cur:] {
		if err := applyOne(ctx, db, m); err != nil {
			return res, fmt.Errorf("migration %04d_%s: %w", m.Version, m.Name, err)
		}
		res.To, res.Applied = m.Version, res.Applied+1
	}
	return res, nil
}

func applyOne(ctx context.Context, db *sql.DB, m migration) error {
	conn, err := db.Conn(ctx) // one connection for the whole sequence: PRAGMA foreign_keys is per connection
	if err != nil {
		return err
	}
	defer conn.Close()
	if m.FKOff {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return err
		}
		defer conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if m.FKOff {
		rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		viol := rows.Next()
		rows.Close()
		if viol {
			return errors.New("foreign_key_check reported violations")
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.Version)); err != nil {
		return err
	}
	return tx.Commit()
}

// schemaFingerprint hashes the normalised schema (type, name, table, sql of every non-internal object, sorted).
func schemaFingerprint(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT type, name, tbl_name, coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY type, name`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	h := sha256.New()
	for rows.Next() {
		var t, n, tn, s string
		if err := rows.Scan(&t, &n, &tn, &s); err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", t, n, tn, strings.Join(strings.Fields(s), " "))
	}
	return hex.EncodeToString(h.Sum(nil)), rows.Err()
}

// expectedFingerprint applies the migrations to a private in-memory database (about 1 ms).
func expectedFingerprint(ctx context.Context, drv driver, ms []migration, c Config) (string, error) {
	db, err := sql.Open(drv.name, "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := migrate(ctx, db, ms); err != nil {
		return "", err
	}
	return schemaFingerprint(ctx, db)
}
