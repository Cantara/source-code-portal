package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/store"
)

// ErrBackupIsSource: Backup was asked to write onto the live database file.
var ErrBackupIsSource = errors.New("sqlite: backup target is the live database")

// Check runs PRAGMA quick_check (full=false) or integrity_check (full=true) on the dedicated read-only bk connection, so it
// never queues behind a job's write and never needs the writer.
func (s *Store) Check(ctx context.Context, full bool) error {
	if err := s.gate("Check", false); err != nil {
		return err
	}
	s.bkMu.Lock()
	defer s.bkMu.Unlock()
	p := "quick_check"
	if full {
		p = "integrity_check"
	}
	err := check(ctx, s.drv, s.bk, p)
	if err != nil {
		var ce *store.CorruptError
		if errors.As(err, &ce) {
			s.markDegraded("Check", err)
		}
		return err
	}
	return nil
}

// Backup writes a consistent copy with VACUUM INTO on the read-only bk connection (never the writer, never query_only),
// verifies the copy, and renames it into place atomically. A stale tmp file from a crashed backup is removed first.
func (s *Store) Backup(ctx context.Context, path string) error {
	if err := s.gate("Backup", false); err != nil {
		return err
	}
	return s.backup(ctx, path)
}

func (s *Store) backup(ctx context.Context, path string) (err error) {
	abs, aerr := filepath.Abs(path)
	if aerr != nil {
		return aerr
	}
	if fi, e := os.Stat(abs); e == nil {
		if src, e2 := os.Stat(s.path); e2 == nil && os.SameFile(fi, src) {
			return ErrBackupIsSource
		}
	}
	if filepath.Clean(abs) == filepath.Clean(mustAbs(s.path)) {
		return ErrBackupIsSource
	}
	s.bkMu.Lock()
	defer s.bkMu.Unlock()
	t0 := time.Now()
	tmp := abs + ".tmp"
	defer func() {
		res := "ok"
		if err != nil {
			res = "error"
			msg := err.Error()
			s.lastBackupErr.Store(&msg)
			_ = os.Remove(tmp)
		}
		s.m.backups.Add(ctx, 1, metric.WithAttributes(attribute.String("result", res)))
		s.m.backupSeconds.Record(ctx, time.Since(t0).Seconds())
	}()
	if err = os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err = s.bk.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		return fmt.Errorf("backup: vacuum into: %w", s.fail("Backup", err))
	}
	if err = verifyFile(ctx, s.drv, tmp, s.schemaV); err != nil {
		return fmt.Errorf("backup: verify: %w", err)
	}
	if err = os.Rename(tmp, abs); err != nil {
		return err
	}
	syncDir(filepath.Dir(abs))
	s.lastBackup.Store(time.Now().Unix())
	s.lastBackupErr.Store(nil)
	return nil
}

func mustAbs(p string) string { a, _ := filepath.Abs(p); return a }

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil { // a directory, never the database file
		_ = d.Sync()
		_ = d.Close()
	}
}

// verifyFile opens a database file read-only and immutable, checks application_id, user_version and quick_check.
func verifyFile(ctx context.Context, drv driver, path string, wantVersion int) error {
	c := DefaultConfig()
	dsn := buildDSN(path, roleBK, c) + "&immutable=1"
	db, err := sql.Open(drv.name, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var aid, uv int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT application_id FROM pragma_application_id), (SELECT user_version FROM pragma_user_version)`).Scan(&aid, &uv); err != nil {
		return err
	}
	if aid != appID {
		return fmt.Errorf("%w: application_id=%#x", ErrForeignDatabase, aid)
	}
	if wantVersion > 0 && uv != wantVersion {
		return fmt.Errorf("user_version %d, want %d", uv, wantVersion)
	} else if wantVersion == 0 && uv > appSchemaMax() {
		return fmt.Errorf("%w: v%d", ErrSchemaNewer, uv)
	}
	return check(ctx, drv, db, "quick_check")
}

func appSchemaMax() int {
	ms, _ := loadMigrations(migrationsFS, "migrations")
	return len(ms)
}

// CheckFile verifies a database file offline (scp store check). It never writes.
func CheckFile(ctx context.Context, path string, full bool) error {
	if err := verifyFile(ctx, defaultDriver(), path, 0); err != nil {
		return err
	}
	if !full {
		return nil
	}
	db, err := sql.Open(defaultDriver().name, buildDSN(path, roleBK, DefaultConfig())+"&immutable=1")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return check(ctx, defaultDriver(), db, "integrity_check")
}

// Restore replaces the database at dbPath with a verified backup (scp store restore). The service must be stopped: the
// function takes an EXCLUSIVE lock for a moment and refuses when it cannot. The old file set is quarantined, not deleted.
func Restore(ctx context.Context, dbPath, backupPath string, keep int) error {
	if err := verifyFile(ctx, defaultDriver(), backupPath, 0); err != nil {
		return fmt.Errorf("restore: backup does not verify: %w", err)
	}
	if _, err := os.Stat(dbPath); err == nil {
		cfg := DefaultConfig()
		cfg.BusyTimeout = 100 * time.Millisecond
		// In WAL mode BEGIN EXCLUSIVE only excludes other writers; locking_mode=EXCLUSIVE additionally needs the shared-memory
		// lock, which fails with BUSY while any other connection (the running service, an operator's sqlite3) has the file open.
		db, err := sql.Open(defaultDriver().name, buildDSN(dbPath, roleRW, cfg)+"&_pragma=locking_mode(EXCLUSIVE)")
		if err != nil {
			return err
		}
		db.SetMaxOpenConns(1)
		_, lerr := db.ExecContext(ctx, `BEGIN IMMEDIATE`)
		if lerr == nil {
			_, _ = db.ExecContext(ctx, `ROLLBACK`)
		}
		_ = db.Close()
		if lerr != nil {
			return fmt.Errorf("restore: database is in use (stop the service first): %w", lerr)
		}
		if _, err := quarantine(dbPath, keep); err != nil {
			return err
		}
	} else if err := removeOrphanSidecars(dbPath); err != nil {
		return err
	}
	tmp := dbPath + ".restore.tmp"
	if err := copyFile(backupPath, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		return err
	}
	syncDir(filepath.Dir(dbPath))
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) // the backup file, not the live database
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Optimize runs PRAGMA optimize (the daily scheduler task `store.optimize` of cmd/scp calls it).
func (s *Store) Optimize(ctx context.Context) error {
	return s.write(ctx, "Optimize", func(tx *sql.Tx) (func(), error) {
		_, err := tx.ExecContext(ctx, `PRAGMA optimize`)
		return nil, err
	})
}

// Info implements store.InfoProvider.
func (s *Store) Info() store.Info {
	in := store.Info{Driver: "modernc.org/sqlite", Path: s.path, SchemaVersion: s.schemaV, SQLiteVersion: s.sqliteV, Rebuilt: s.rebuilt,
		Health: s.Health(), CacheBytes: map[string]int64{}}
	if fi, err := os.Stat(s.path); err == nil {
		in.DBBytes = fi.Size()
	}
	if fi, err := os.Stat(s.path + "-wal"); err == nil {
		in.WALBytes = fi.Size()
	}
	s.cacheMu.Lock()
	for k, v := range s.cacheBytes {
		in.CacheBytes[k] = v
	}
	s.cacheMu.Unlock()
	if t := s.lastBackup.Load(); t > 0 {
		in.LastBackup = time.Unix(t, 0).UTC()
	}
	if e := s.lastBackupErr.Load(); e != nil {
		in.LastBackupErr = *e
	}
	return in
}

var _ store.InfoProvider = (*Store)(nil)
var _ store.Optimizer = (*Store)(nil)
