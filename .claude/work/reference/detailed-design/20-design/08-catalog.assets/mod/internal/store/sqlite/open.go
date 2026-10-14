package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/store"
)

// OpenReport tells cmd/scp what Open did (logged at startup, shown by /status).
type OpenReport struct {
	Created       bool // a new database file was created
	Migrated      MigrateResult
	SchemaVersion int
	SQLiteVersion string
	Rebuilt       string // non-empty: the previous file was quarantined, reason below
	QuarantinedAs string
	Pruned        map[string]int64
	Duration      time.Duration
}

// Rebuild reasons (label values of scp_store_rebuilds_total).
const (
	ReasonCorrupt         = "corrupt"
	ReasonNotADatabase    = "not_a_database"
	ReasonSchemaNewer     = "schema_newer"
	ReasonMigrationFailed = "migration_failed"
	ReasonSchemaDrift     = "schema_drift"
)

type rebuildable struct {
	reason string
	err    error
}

func (r *rebuildable) Error() string { return r.reason + ": " + r.err.Error() }
func (r *rebuildable) Unwrap() error { return r.err }

// Open opens (creating, migrating, or quarantining and recreating) the database and returns a ready Store. It must be
// called from cmd/scp step 2, before any feature is constructed. It does not start goroutines: Run does.
func Open(ctx context.Context, cfg Config, d Deps) (*Store, OpenReport, error) {
	t0 := time.Now()
	d.defaults()
	if errs := cfg.Validate("store"); len(errs) > 0 {
		return nil, OpenReport{}, errors.Join(errs...)
	}
	drv := d.drv
	if drv.name == "" {
		drv = defaultDriver()
	}
	drv.strict()
	ms := d.migrations
	if ms == nil {
		var err error
		if ms, err = loadMigrations(migrationsFS, "migrations"); err != nil {
			return nil, OpenReport{}, err
		}
	}
	m := newMetrics(d.Meter)
	var rep OpenReport
	s, rep, err := openOnce(ctx, cfg, d, drv, ms, m)
	var rb *rebuildable
	if errors.As(err, &rb) {
		d.Log.Error("database unusable: quarantining and starting cold", "reason", rb.reason, "err", rb.err, "path", cfg.Path)
		q, qerr := quarantine(cfg.Path, cfg.QuarantineKeep)
		if qerr != nil {
			return nil, rep, fmt.Errorf("quarantine failed: %w", qerr)
		}
		m.rebuilds.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", rb.reason)))
		s, rep, err = openOnce(ctx, cfg, d, drv, ms, m)
		if err != nil {
			return nil, rep, fmt.Errorf("fresh database also failed to open (disk fault?): %w", err)
		}
		rep.Rebuilt, rep.QuarantinedAs = rb.reason, q
		s.rebuilt = rb.reason
	}
	if err != nil {
		return nil, rep, err
	}
	rep.Duration = time.Since(t0)
	return s, rep, nil
}

func openOnce(ctx context.Context, cfg Config, d Deps, drv driver, ms []migration, m *metrics) (*Store, OpenReport, error) {
	var rep OpenReport
	_, statErr := os.Stat(cfg.Path)
	rep.Created = errors.Is(statErr, os.ErrNotExist)
	if rep.Created {
		if err := removeOrphanSidecars(cfg.Path); err != nil { // a -wal next to a NEW database would be replayed into it
			return nil, rep, err
		}
	}
	rw, err := sql.Open(drv.name, buildDSN(cfg.Path, roleRW, cfg))
	if err != nil {
		return nil, rep, err
	}
	rw.SetMaxOpenConns(1)
	rw.SetMaxIdleConns(1)
	rw.SetConnMaxLifetime(0)
	rw.SetConnMaxIdleTime(0)
	closeAll := func(dbs ...*sql.DB) {
		for _, db := range dbs {
			if db != nil {
				_ = db.Close()
			}
		}
	}
	if err := rw.PingContext(ctx); err != nil {
		closeAll(rw)
		if c, ok := drv.code(err); ok && c == sqliteNotADB {
			return nil, rep, &rebuildable{ReasonNotADatabase, err}
		}
		return nil, rep, fmt.Errorf("open %s: %w", cfg.Path, err)
	}
	var ver string
	if err := rw.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&ver); err != nil {
		closeAll(rw)
		return nil, rep, err
	}
	rep.SQLiteVersion = ver
	if !versionAtLeast(ver, 3, 51, 3) {
		closeAll(rw)
		return nil, rep, fmt.Errorf("%w: have %s", ErrVersionGuard, ver)
	}
	var aid, uv, objects int
	if err := rw.QueryRowContext(ctx, `SELECT (SELECT application_id FROM pragma_application_id), (SELECT user_version FROM pragma_user_version), (SELECT count(*) FROM sqlite_schema)`).Scan(&aid, &uv, &objects); err != nil {
		closeAll(rw)
		if c, ok := drv.code(err); ok && (c == sqliteNotADB || c == sqliteCorrupt) {
			return nil, rep, &rebuildable{ReasonCorrupt, err}
		}
		return nil, rep, err
	}
	switch {
	case aid != 0 && aid != appID, aid == 0 && objects > 0:
		closeAll(rw)
		return nil, rep, fmt.Errorf("%w: application_id=%#x with %d objects", ErrForeignDatabase, aid, objects)
	case uv > len(ms):
		closeAll(rw)
		return nil, rep, &rebuildable{ReasonSchemaNewer, fmt.Errorf("file is v%d, build knows v%d", uv, len(ms))}
	}
	if objects > 0 {
		if err := check(ctx, drv, rw, "quick_check"); err != nil {
			closeAll(rw)
			return nil, rep, &rebuildable{ReasonCorrupt, err}
		}
	}
	res, err := migrate(ctx, rw, ms)
	rep.Migrated = res
	if err != nil {
		closeAll(rw)
		return nil, rep, &rebuildable{ReasonMigrationFailed, err}
	}
	rep.SchemaVersion = res.To
	if objects > 0 { // a database that was valid and current must match the schema this build expects
		want, err := expectedFingerprint(ctx, drv, ms, cfg)
		if err != nil {
			closeAll(rw)
			return nil, rep, err
		}
		got, err := schemaFingerprint(ctx, rw)
		if err != nil {
			closeAll(rw)
			return nil, rep, err
		}
		if got != want {
			closeAll(rw)
			return nil, rep, &rebuildable{ReasonSchemaDrift, fmt.Errorf("schema fingerprint %s, expected %s", got[:12], want[:12])}
		}
	}
	ro, err := sql.Open(drv.name, buildDSN(cfg.Path, roleRO, cfg))
	if err != nil {
		closeAll(rw)
		return nil, rep, err
	}
	ro.SetMaxOpenConns(cfg.ReadConns)
	ro.SetMaxIdleConns(cfg.ReadConns)
	bk, err := sql.Open(drv.name, buildDSN(cfg.Path, roleBK, cfg))
	if err != nil {
		closeAll(rw, ro)
		return nil, rep, err
	}
	bk.SetMaxOpenConns(1)
	bk.SetMaxIdleConns(1)
	s := &Store{cfg: cfg, drv: drv, path: cfg.Path, log: d.Log.With("component", "store"), m: m, tracer: d.Tracer, rw: rw, ro: ro, bk: bk,
		schemaV: res.To, sqliteV: ver, buildID: d.BuildID, caps: map[string]int64{}, cacheBytes: map[string]int64{},
		touched: map[touchKey]uint64{}}
	for ns, c := range d.CacheNamespaces {
		s.caps[ns] = c
	}
	if err := s.initAfterOpen(ctx, d, &rep); err != nil {
		closeAll(rw, ro, bk)
		return nil, rep, err
	}
	s.registerGauges(d.Meter)
	s.log.Info("store open", "path", filepath.Base(cfg.Path), "sqlite", ver, "schema", res.To, "migrated", res.Applied, "created", rep.Created)
	return s, rep, nil
}

func (s *Store) initAfterOpen(ctx context.Context, d Deps, rep *OpenReport) error {
	now := time.Now().Unix()
	rep.Pruned = map[string]int64{}
	err := s.write(ctx, "open", func(tx *sql.Tx) (func(), error) {
		for _, kv := range [][2]string{{"last_open_at", strconv.FormatInt(now, 10)}, {"build_id", d.BuildID}} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, kv[0], kv[1]); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO meta(key,value) VALUES('created_at', ?)`, strconv.FormatInt(now, 10)); err != nil {
			return nil, err
		}
		if len(d.KnownKinds) > 0 {
			ph := strings.TrimSuffix(strings.Repeat("?,", len(d.KnownKinds)), ",")
			args := make([]any, len(d.KnownKinds))
			for i, k := range d.KnownKinds {
				args[i] = k
			}
			res, err := tx.ExecContext(ctx, `DELETE FROM item WHERE kind NOT IN (`+ph+`)`, args...)
			if err != nil {
				return nil, err
			}
			rep.Pruned["item"], _ = res.RowsAffected()
		}
		if len(s.caps) > 0 {
			nss := make([]string, 0, len(s.caps))
			for ns := range s.caps {
				nss = append(nss, ns)
			}
			sort.Strings(nss)
			args := make([]any, len(nss))
			for i, ns := range nss {
				args[i] = ns
			}
			res, err := tx.ExecContext(ctx, `DELETE FROM cache WHERE ns NOT IN (`+strings.TrimSuffix(strings.Repeat("?,", len(nss)), ",")+`)`, args...)
			if err != nil {
				return nil, err
			}
			rep.Pruned["cache"], _ = res.RowsAffected()
		}
		return nil, nil
	})
	if err != nil {
		return err
	}
	for t, n := range rep.Pruned {
		if n > 0 {
			s.m.pruned.Add(ctx, n, metric.WithAttributes(attribute.String("table", t)))
		}
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT ns, coalesce(sum(size),0) FROM cache GROUP BY ns`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ns string
		var n int64
		if err := rows.Scan(&ns, &n); err != nil {
			rows.Close()
			return err
		}
		s.cacheBytes[ns] = n
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var mx sql.NullInt64
	if err := s.ro.QueryRowContext(ctx, `SELECT max(use_seq) FROM cache`).Scan(&mx); err != nil {
		return err
	}
	s.seq.Store(uint64(mx.Int64))
	if _, err := s.rw.ExecContext(ctx, `PRAGMA optimize=0x10002`); err != nil {
		return err
	}
	return nil
}

// check reads the (up to 100) rows of PRAGMA quick_check/integrity_check; the first row "ok" means healthy.
func check(ctx context.Context, drv driver, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, pragma string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA "+pragma)
	if err != nil {
		if c, ok := drv.code(err); ok && (c == sqliteCorrupt || c == sqliteNotADB) {
			return &store.CorruptError{Messages: []string{err.Error()}}
		}
		return err
	}
	defer rows.Close()
	var msgs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return err
		}
		if len(msgs) < 5 {
			msgs = append(msgs, trunc(s, 300))
		}
	}
	if err := rows.Err(); err != nil {
		if c, ok := drv.code(err); ok && c == sqliteCorrupt {
			return &store.CorruptError{Messages: append(msgs, err.Error())}
		}
		return err
	}
	if len(msgs) == 1 && msgs[0] == "ok" {
		return nil
	}
	return &store.CorruptError{Messages: msgs}
}

func versionAtLeast(v string, maj, min, patch int) bool {
	parts := strings.SplitN(v, ".", 4)
	if len(parts) < 3 {
		return false
	}
	n := [3]int{}
	for i := 0; i < 3; i++ {
		x, err := strconv.Atoi(parts[i])
		if err != nil {
			return false
		}
		n[i] = x
	}
	return n[0] > maj || (n[0] == maj && (n[1] > min || (n[1] == min && n[2] >= patch)))
}

// quarantine renames the sidecars first (-shm, -wal) and the database last, so a crash in between never leaves a WAL beside a
// database it does not belong to; then keeps only the newest `keep` quarantined sets.
func quarantine(path string, keep int) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	base := path + ".quarantine-" + stamp
	for n := 2; ; n++ { // never overwrite an existing set (two rebuilds in one second)
		if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
			break
		}
		base = path + ".quarantine-" + stamp + "-" + strconv.Itoa(n)
	}
	for _, suf := range []string{"-shm", "-wal", ""} {
		if err := os.Rename(path+suf, base+suf); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	pruneQuarantine(path, keep)
	return base, nil
}

func pruneQuarantine(path string, keep int) {
	dir, prefix := filepath.Dir(path), filepath.Base(path)+".quarantine-"
	ents, _ := os.ReadDir(dir)
	sets := map[string]struct{}{}
	for _, e := range ents {
		if n := e.Name(); strings.HasPrefix(n, prefix) {
			sets[strings.TrimSuffix(strings.TrimSuffix(n, "-wal"), "-shm")] = struct{}{}
		}
	}
	names := make([]string, 0, len(sets))
	for n := range sets {
		names = append(names, n)
	}
	sort.Strings(names) // the UTC stamp sorts chronologically
	for len(names) > keep {
		for _, suf := range []string{"-shm", "-wal", ""} {
			_ = os.Remove(filepath.Join(dir, names[0]+suf))
		}
		names = names[1:]
	}
}

func removeOrphanSidecars(path string) error {
	for _, suf := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suf); err == nil {
			if _, err := quarantine(path, 100); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}
