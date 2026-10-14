package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/storetest"
)

var ctx = context.Background()
var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func mkRepo(i int) store.Repo {
	return store.Repo{Key: store.RepoKey{Source: "src", Owner: "acme", Name: fmt.Sprintf("repo-%02d", i)}, GitHubID: int64(1000 + i), Slug: fmt.Sprintf("repo-%02d", i), Meta: []byte(`{}`), SeenAt: t0}
}

func populate(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.ReplaceCatalogue(ctx, []store.Repo{mkRepo(0), mkRepo(1)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		it := store.Item{Key: store.ItemKey{Repo: mkRepo(i % 2).Key, Kind: fmt.Sprintf("docs.k%d", i/2)}, State: "ok", FetchedAt: t0, CheckedAt: t0, Payload: make([]byte, 2000)}
		if err := s.PutItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPragmasPerRole(t *testing.T) {
	s, _ := open(t, testConfig(t), nil)
	defer s.Close(ctx)
	get := func(db *sql.DB, p string) string {
		var v string
		if err := db.QueryRow("PRAGMA " + p).Scan(&v); err != nil {
			t.Fatal(p, err)
		}
		return v
	}
	for name, db := range map[string]*sql.DB{"rw": s.rw, "ro": s.ro, "bk": s.bk} {
		if get(db, "temp_store") != "2" || get(db, "trusted_schema") != "0" || get(db, "busy_timeout") != "5000" {
			t.Fatalf("%s: temp_store/trusted_schema/busy_timeout", name)
		}
	}
	if get(s.rw, "journal_mode") != "wal" || get(s.rw, "synchronous") != "1" || get(s.rw, "foreign_keys") != "1" || get(s.rw, "journal_size_limit") != "67108864" {
		t.Fatal("rw pragmas")
	}
	if get(s.ro, "query_only") != "1" || get(s.bk, "query_only") != "0" {
		t.Fatal("ro must be query_only, bk must not be (VACUUM INTO fails under query_only)")
	}
	if _, err := s.ro.Exec(`INSERT INTO meta VALUES('x','y')`); err == nil {
		t.Fatal("ro wrote")
	}
	if _, err := s.bk.Exec(`INSERT INTO meta VALUES('x','y')`); err == nil {
		t.Fatal("bk wrote")
	}
	// the writer is one connection: a second concurrent transaction waits, never opens a second connection
	if s.rw.Stats().MaxOpenConnections != 1 {
		t.Fatal("rw pool")
	}
}

func TestDSNEscapingFuzzSeeds(t *testing.T) {
	for _, p := range []string{"/data/scp.db", "/tmp/with space/x y.db", "/tmp/q?mode=ro&x=1/db", "/tmp/a#b/c%20d.db", "/tmp/_pragma=journal_mode(OFF)/x.db", "/tmp/a;b.db"} {
		dsn := buildDSN(p, roleRW, DefaultConfig())
		u, err := url.Parse(dsn)
		if err != nil || u.Scheme != "file" || u.Path != p {
			t.Fatalf("%q -> %q (%v) path %q", p, dsn, err, u.Path)
		}
		q := u.Query()
		if q.Has("mode") || len(q["_pragma"]) != 7 || q.Get("_txlock") != "immediate" {
			t.Fatalf("injected parameter via path %q: %v", p, q)
		}
	}
}

func FuzzDSN(f *testing.F) {
	for _, s := range []string{"/a/b", "x?y", "z#w", "%", "a b", "../../x", "é"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		if p == "" || strings.ContainsRune(p, 0) {
			return
		}
		p = "/" + p
		dsn := buildDSN(p, roleBK, DefaultConfig())
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("%q: %v", dsn, err)
		}
		if u.Path != filepath.Clean(p) && u.Path != p {
			t.Fatalf("path changed: %q -> %q", p, u.Path)
		}
		if q := u.Query(); q.Get("mode") != "ro" || len(q["_pragma"]) != 3 {
			t.Fatalf("parameters changed for path %q: %v", p, q)
		}
	})
}

func TestQuarantineOnGarbageFile(t *testing.T) {
	c := testConfig(t)
	os.WriteFile(c.Path, []byte("this is certainly not a SQLite database file, just text, long enough to have a header"), 0o600)
	s, rep := open(t, c, nil)
	defer s.Close(ctx)
	if rep.Rebuilt != ReasonNotADatabase || !rep.Created || rep.QuarantinedAs == "" {
		t.Fatalf("%+v", rep)
	}
	if _, err := os.Stat(rep.QuarantinedAs); err != nil {
		t.Fatal(err)
	}
	if s.Info().Rebuilt != ReasonNotADatabase {
		t.Fatal("info")
	}
}

func TestQuarantineOnPageCorruption(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, nil)
	populate(t, s)
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(c.Path)
	for i := 0; i < 128; i++ {
		b[4096*6+i] ^= 0xff
	}
	os.WriteFile(c.Path, b, 0o600)
	s2, rep := open(t, c, nil)
	defer s2.Close(ctx)
	if rep.Rebuilt != ReasonCorrupt {
		t.Fatalf("%+v", rep)
	}
	if got, _ := s2.LoadCatalogue(ctx); len(got) != 0 {
		t.Fatal("cold start must be empty")
	}
}

func TestSchemaNewerQuarantines(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, nil)
	s.rw.Exec(`PRAGMA user_version = 99`)
	s.Close(ctx)
	s2, rep := open(t, c, nil)
	defer s2.Close(ctx)
	if rep.Rebuilt != ReasonSchemaNewer {
		t.Fatalf("%+v", rep)
	}
}

func TestSchemaDriftQuarantines(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, nil)
	s.rw.Exec(`CREATE INDEX stray ON item(kind)`)
	s.Close(ctx)
	s2, rep := open(t, c, nil)
	defer s2.Close(ctx)
	if rep.Rebuilt != ReasonSchemaDrift {
		t.Fatalf("%+v", rep)
	}
}

func TestForeignDatabaseIsNeverTouched(t *testing.T) {
	c := testConfig(t)
	db, _ := sql.Open("sqlite", "file:"+c.Path)
	db.Exec(`CREATE TABLE other(x)`)
	db.Close()
	_, _, err := Open(ctx, c, Deps{})
	if !errors.Is(err, ErrForeignDatabase) {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.Path); err != nil {
		t.Fatal("foreign file must stay in place")
	}
	if m, _ := filepath.Glob(c.Path + ".quarantine-*"); len(m) != 0 {
		t.Fatal("foreign database was quarantined")
	}
}

func TestMigrationFailureQuarantinesThenFatalOnFreshFailure(t *testing.T) {
	c := testConfig(t)
	good, _ := loadMigrations(migrationsFS, "migrations")
	s, _ := open(t, c, nil)
	populate(t, s)
	s.Close(ctx)
	broken := append(slices.Clone(good), migration{Version: 2, Name: "broken", SQL: "CREATE TABLE ok(a); THIS IS BROKEN"})
	// an existing database whose pending migration fails: quarantine, then the fresh file hits the same broken migration: fatal
	_, _, err := Open(ctx, c, Deps{migrations: broken})
	if err == nil || !strings.Contains(err.Error(), "fresh database also failed") {
		t.Fatalf("%v", err)
	}
	if m, _ := filepath.Glob(c.Path + ".quarantine-*"); len(m) != 1 {
		t.Fatalf("quarantine: %v", m)
	}
}

func TestMigrationRunnerIsAtomicAndAdditive(t *testing.T) {
	c := testConfig(t)
	good, _ := loadMigrations(migrationsFS, "migrations")
	s, _ := open(t, c, nil)
	s.Close(ctx)
	add := append(slices.Clone(good), migration{Version: 2, Name: "add_col", SQL: "ALTER TABLE repo ADD COLUMN note TEXT; CREATE INDEX repo_note ON repo(note);"})
	s2, rep, err := Open(ctx, c, Deps{migrations: add})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Migrated.From != 1 || rep.Migrated.To != 2 || rep.Rebuilt != "" {
		t.Fatalf("%+v", rep)
	}
	s2.Close(ctx)
	// a rollback of the binary to v1 meets schema v2: quarantine (cold start)
	s3, rep3 := open(t, c, nil)
	defer s3.Close(ctx)
	if rep3.Rebuilt != ReasonSchemaNewer {
		t.Fatalf("%+v", rep3)
	}
}

func TestFailedMigrationLeavesVersionUntouched(t *testing.T) {
	db, _ := sql.Open("sqlite", "file::memory:")
	db.SetMaxOpenConns(1)
	defer db.Close()
	good, _ := loadMigrations(migrationsFS, "migrations")
	if _, err := migrate(ctx, db, good); err != nil {
		t.Fatal(err)
	}
	bad := append(slices.Clone(good), migration{Version: 2, Name: "bad", SQL: "CREATE TABLE half(a); NOT SQL"})
	if _, err := migrate(ctx, db, bad); err == nil {
		t.Fatal("want failure")
	}
	var v, n int
	db.QueryRow(`PRAGMA user_version`).Scan(&v)
	db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='half'`).Scan(&n)
	if v != 1 || n != 0 {
		t.Fatalf("user_version=%d half=%d", v, n)
	}
}

func TestForeignKeyOffMigration(t *testing.T) {
	db, _ := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	db.SetMaxOpenConns(1)
	defer db.Close()
	good, _ := loadMigrations(migrationsFS, "migrations")
	rebuild := "-- scp:foreign_keys=off\nCREATE TABLE repo_new(id INTEGER PRIMARY KEY, source TEXT NOT NULL, owner TEXT NOT NULL, name TEXT NOT NULL, github_id INTEGER, slug TEXT NOT NULL UNIQUE, seen_at INTEGER NOT NULL, meta TEXT NOT NULL, extra TEXT, UNIQUE(source,owner,name)) STRICT;\n" +
		"INSERT INTO repo_new(id,source,owner,name,github_id,slug,seen_at,meta) SELECT id,source,owner,name,github_id,slug,seen_at,meta FROM repo;\nDROP TABLE repo;\nALTER TABLE repo_new RENAME TO repo;\n"
	ms := append(slices.Clone(good), migration{Version: 2, Name: "rebuild", SQL: rebuild, FKOff: true})
	if _, err := migrate(ctx, db, good); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO repo(source,owner,name,slug,seen_at,meta) VALUES('s','o','n','slug',1,'{}')`)
	db.Exec(`INSERT INTO item(repo_id,kind,status,fetched_at,checked_at) VALUES(1,'a.b','ok',1,1)`)
	if _, err := migrate(ctx, db, ms); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM item`).Scan(&n)
	if n != 1 {
		t.Fatal("rebuild with foreign_keys=off must not cascade-delete items")
	}
	var fk int
	db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign_keys must be restored")
	}
}

func TestVersionGuardParse(t *testing.T) {
	for v, want := range map[string]bool{"3.53.4": true, "3.51.3": true, "3.51.2": false, "3.50.7": false, "4.0.0": true, "3.52.0": true, "garbage": false, "3.9": false} {
		if versionAtLeast(v, 3, 51, 3) != want {
			t.Fatalf("%s", v)
		}
	}
}

func TestBackupVerifyAndRestore(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, nil)
	populate(t, s)
	bak := filepath.Join(filepath.Dir(c.Path), "scp.db.bak")
	if err := s.Backup(ctx, bak); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(ctx, c.Path); !errors.Is(err, ErrBackupIsSource) {
		t.Fatalf("%v", err)
	}
	if err := CheckFile(ctx, bak, true); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(bak + ".tmp"); fi != nil {
		t.Fatal("tmp left behind")
	}
	if s.Info().LastBackup.IsZero() {
		t.Fatal("info")
	}
	// writes after the backup are not in it; restore brings back the backup state
	s.PutItem(ctx, store.Item{Key: store.ItemKey{Repo: mkRepo(0).Key, Kind: "docs.after"}, State: "ok", FetchedAt: t0, CheckedAt: t0})
	if err := Restore(ctx, c.Path, bak, 2); err == nil {
		t.Fatal("restore must refuse while the database is open")
	}
	s.Close(ctx)
	if err := Restore(ctx, c.Path, bak, 2); err != nil {
		t.Fatal(err)
	}
	s2, rep := open(t, c, nil)
	defer s2.Close(ctx)
	if rep.Rebuilt != "" {
		t.Fatalf("%+v", rep)
	}
	n := 0
	s2.LoadItems(ctx, func(it store.Item) error {
		if it.Key.Kind == "docs.after" {
			t.Fatal("post-backup row present")
		}
		n++
		return nil
	})
	if n != 200 {
		t.Fatal(n)
	}
	if m, _ := filepath.Glob(c.Path + ".quarantine-*"); len(m) == 0 {
		t.Fatal("old database must be quarantined, not deleted")
	}
}

func TestBackupOfCorruptCopyIsNotInstalled(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, nil)
	defer s.Close(ctx)
	if err := os.WriteFile(filepath.Join(t.TempDir(), "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "junk")
	os.WriteFile(bad, []byte("junk junk junk junk junk junk junk junk junk junk junk junk"), 0o600)
	if err := CheckFile(ctx, bad, false); err == nil {
		t.Fatal("junk verified")
	}
}

func TestCheckDetectsCorruptionAndDegrades(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, nil)
	populate(t, s)
	s.rw.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(new(int), new(int), new(int))
	b, _ := os.ReadFile(c.Path)
	for i := 0; i < 64; i++ {
		b[4096*5+i] ^= 0xff
	}
	f, _ := os.OpenFile(c.Path, os.O_WRONLY, 0) // the test deliberately damages the live file
	f.WriteAt(b[4096*5:4096*5+64], 4096*5)
	f.Close()
	err := s.Check(ctx, false)
	if !errors.Is(err, store.ErrCorrupt) {
		t.Fatalf("%v", err)
	}
	if s.Health().OK {
		t.Fatal("corruption must degrade")
	}
	if err := s.PutCache(ctx, "x", "k", nil); err == nil {
		t.Fatal("write while degraded")
	}
	s.Close(ctx)
}

func TestDiskFullDegradesAndProbeRecovers(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, map[string]int64{"t.big": 1 << 20})
	defer s.Close(ctx)
	populate(t, s)
	var pages int
	s.rw.QueryRow(`PRAGMA page_count`).Scan(&pages)
	s.rw.Exec(fmt.Sprintf(`PRAGMA max_page_count = %d`, pages)) // writer connection persists: the file cannot grow
	err := s.PutCache(ctx, "t.big", "k", make([]byte, 200000))
	if err == nil {
		t.Fatal("want SQLITE_FULL")
	}
	if s.Health().OK {
		t.Fatalf("SQLITE_FULL must degrade: %v", err)
	}
	if err := s.PutCache(ctx, "t.big", "k", []byte("x")); !errors.Is(err, store.ErrDegraded) {
		t.Fatalf("%v", err)
	}
	if _, err := s.LoadCatalogue(ctx); err != nil {
		t.Fatal("reads must keep working")
	}
	s.rw.Exec(`PRAGMA max_page_count = 1073741823`)
	if err := s.probe(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Health().OK {
		t.Fatal("probe did not recover")
	}
}

func TestHotQueryPlansUseIndexes(t *testing.T) {
	s, _ := open(t, testConfig(t), nil)
	defer s.Close(ctx)
	plan := func(q string, args ...any) string {
		rows, err := s.rw.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var sb strings.Builder
		for rows.Next() {
			var a, b, c int
			var d string
			rows.Scan(&a, &b, &c, &d)
			sb.WriteString(d + "\n")
		}
		return sb.String()
	}
	for name, tc := range map[string]struct{ q, want string }{
		"commits page": {`SELECT sha FROM commit_log WHERE repo_id=1 ORDER BY committed_at DESC, id DESC LIMIT 30`, "USING INDEX commit_log_recent"},
		"item by key":  {`SELECT id FROM item WHERE repo_id=1 AND kind='a.b'`, "INDEX sqlite_autoindex_item_1"},
		"cache lru":    {`SELECT key FROM cache WHERE ns='x' AND key<>'y' ORDER BY use_seq, rowid LIMIT 256`, "USING INDEX cache_lru"},
		"repo by key":  {`SELECT id FROM repo WHERE source='a' AND owner='b' AND name='c'`, "sqlite_autoindex_repo_1"},
	} {
		p := plan(tc.q)
		if !strings.Contains(p, tc.want) || strings.Contains(p, "TEMP B-TREE") {
			t.Fatalf("%s: plan\n%s", name, p)
		}
	}
}

// Differential property test: random operation sequences against the memory store as the model.
func TestWorkerTimingInSynctestBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var backups, probes, flushes atomic.Int32
		var healthy atomic.Bool
		healthy.Store(true)
		w := &worker{probeEvery: 30 * time.Second, backupEvery: time.Hour, backupEnabled: true, firstBackup: firstBackupDelay(time.Hour, 0, false),
			healthy: healthy.Load,
			probe:   func(context.Context) error { probes.Add(1); healthy.Store(true); return nil },
			backup:  func(context.Context) error { backups.Add(1); return nil },
			flush:   func(context.Context) error { flushes.Add(1); return nil },
			onErr:   func(string, error) {}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { w.run(ctx); close(done) }()
		time.Sleep(5*time.Minute + time.Second) // first backup after min(interval, 5m)
		if backups.Load() != 1 || flushes.Load() != 1 || probes.Load() != 0 {
			t.Fatalf("t=5m: backups=%d flushes=%d probes=%d", backups.Load(), flushes.Load(), probes.Load())
		}
		healthy.Store(false)
		time.Sleep(31 * time.Second)
		if probes.Load() != 1 || !healthy.Load() {
			t.Fatalf("probes=%d", probes.Load())
		}
		time.Sleep(time.Hour) // fixed delay after completion: next backup one hour after the first
		if backups.Load() != 2 {
			t.Fatalf("backups=%d", backups.Load())
		}
		cancel()
		<-done
	})
	if d := firstBackupDelay(time.Hour, 20*time.Minute, true); d != 40*time.Minute {
		t.Fatal(d)
	}
	if d := firstBackupDelay(time.Hour, 59*time.Minute, true); d != 5*time.Minute {
		t.Fatal(d)
	}
	if d := firstBackupDelay(time.Hour, 3*time.Hour, true); d != 5*time.Minute {
		t.Fatal(d)
	}
}

func TestInfoAndPrune(t *testing.T) {
	c := testConfig(t)
	s, _, err := Open(ctx, c, Deps{CacheNamespaces: map[string]int64{"a.one": 1 << 20, "a.two": 1 << 20}, KnownKinds: []string{"docs.k0", "docs.k1"}})
	if err != nil {
		t.Fatal(err)
	}
	populate(t, s)
	s.PutCache(ctx, "a.one", "k", make([]byte, 1000))
	s.PutCache(ctx, "a.two", "k", make([]byte, 500))
	s.Close(ctx)
	// reopen with a smaller set of kinds and namespaces: the rest is pruned
	s2, rep, err := Open(ctx, c, Deps{CacheNamespaces: map[string]int64{"a.one": 1 << 20}, KnownKinds: []string{"docs.k0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close(ctx)
	if rep.Pruned["cache"] != 1 || rep.Pruned["item"] == 0 {
		t.Fatalf("%+v", rep.Pruned)
	}
	in := s2.Info()
	if in.CacheBytes["a.one"] != 1000 || in.CacheBytes["a.two"] != 0 || in.SchemaVersion != 1 || in.DBBytes == 0 || !strings.HasPrefix(in.SQLiteVersion, "3.") {
		t.Fatalf("%+v", in)
	}
}

func TestOrphanWALIsNotReplayedIntoANewDatabase(t *testing.T) {
	c := testConfig(t)
	s, _ := open(t, c, nil)
	populate(t, s)
	// simulate a crash: copy the -wal aside while open, then delete the main file only
	wal, _ := os.ReadFile(c.Path + "-wal")
	s.Close(ctx)
	os.Remove(c.Path)
	os.WriteFile(c.Path+"-wal", wal, 0o600)
	s2, rep := open(t, c, nil)
	defer s2.Close(ctx)
	if !rep.Created {
		t.Fatal("expected a new database")
	}
	if got, _ := s2.LoadCatalogue(ctx); len(got) != 0 {
		t.Fatalf("orphan WAL leaked into a new database: %d repos", len(got))
	}
}

func BenchmarkPutItem25KB(b *testing.B) {
	s, _, _ := Open(ctx, testConfigB(b), Deps{})
	defer s.Close(ctx)
	s.ReplaceCatalogue(ctx, []store.Repo{mkRepo(0)})
	pl := make([]byte, 25000)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := s.PutItem(ctx, store.Item{Key: store.ItemKey{Repo: mkRepo(0).Key, Kind: "docs.refresh"}, State: "ok", FetchedAt: t0, CheckedAt: t0, ETag: "e", ContentSHA: "s", Payload: pl}); err != nil {
			b.Fatal(err)
		}
	}
}

func testConfigB(b *testing.B) Config {
	c := DefaultConfig()
	c.Path = filepath.Join(b.TempDir(), "scp.db")
	return c
}

func BenchmarkAppendCommits30(b *testing.B) {
	s, _, _ := Open(ctx, testConfigB(b), Deps{})
	defer s.Close(ctx)
	s.ReplaceCatalogue(ctx, []store.Repo{mkRepo(0)})
	k := mkRepo(0).Key
	for i := 0; i < 30; i++ {
		s.AppendCommits(ctx, k, []store.CommitRow{{SHA: fmt.Sprint("s", i), CommittedAt: t0.Add(time.Duration(i) * time.Second), Payload: make([]byte, 400)}}, 30)
	}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		s.AppendCommits(ctx, k, []store.CommitRow{{SHA: fmt.Sprint("n", i), CommittedAt: t0.Add(time.Hour + time.Duration(i)*time.Second), Payload: make([]byte, 400)}}, 30)
	}
}

func BenchmarkSavePlans1000(b *testing.B) {
	s, _, _ := Open(ctx, testConfigB(b), Deps{})
	defer s.Close(ctx)
	var rs []store.Repo
	var rows []store.PlanRow
	for i := 0; i < 1000; i++ {
		r := store.Repo{Key: store.RepoKey{Source: "s", Owner: "o", Name: fmt.Sprint("r", i)}, Slug: fmt.Sprint("r", i), Meta: []byte("{}"), SeenAt: t0}
		rs = append(rs, r)
	}
	s.ReplaceCatalogue(ctx, rs)
	for _, r := range rs {
		for _, kd := range []string{"docs.refresh", "commits.refresh", "manifests.refresh"} {
			s.PutItem(ctx, store.Item{Key: store.ItemKey{Repo: r.Key, Kind: kd}, State: "ok", FetchedAt: t0, CheckedAt: t0})
			rows = append(rows, store.PlanRow{Key: store.ItemKey{Repo: r.Key, Kind: kd}, NextDue: t0, UnchangedStreak: 1})
		}
	}
	b.ResetTimer()
	for b.Loop() {
		if err := s.SavePlans(ctx, rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWarmLoad34k(b *testing.B) {
	c := testConfigB(b)
	s, _, _ := Open(ctx, c, Deps{})
	var rs []store.Repo
	for i := 0; i < 1000; i++ {
		rs = append(rs, store.Repo{Key: store.RepoKey{Source: "s", Owner: "o", Name: fmt.Sprint("r", i)}, Slug: fmt.Sprint("r", i), Meta: make([]byte, 0, 0), SeenAt: t0})
		rs[i].Meta = []byte(`{"d":"` + strings.Repeat("x", 1500) + `"}`)
	}
	s.ReplaceCatalogue(ctx, rs)
	pl := make([]byte, 45000)
	for _, r := range rs {
		s.PutItem(ctx, store.Item{Key: store.ItemKey{Repo: r.Key, Kind: "docs.refresh"}, State: "ok", FetchedAt: t0, CheckedAt: t0, Payload: pl})
		for _, kd := range []string{"catalog.stats", "integrations.jenkins", "integrations.snyk"} {
			s.PutItem(ctx, store.Item{Key: store.ItemKey{Repo: r.Key, Kind: kd}, State: "ok", FetchedAt: t0, CheckedAt: t0, Payload: []byte(`{"s":"ok"}`)})
		}
		var cs []store.CommitRow
		for j := 0; j < 30; j++ {
			cs = append(cs, store.CommitRow{SHA: fmt.Sprintf("%040d", j), CommittedAt: t0.Add(-time.Duration(j) * time.Hour), Payload: make([]byte, 400)})
		}
		s.AppendCommits(ctx, r.Key, cs, 30)
	}
	s.Close(ctx)
	b.ResetTimer()
	for b.Loop() {
		s2, _, err := Open(ctx, c, Deps{})
		if err != nil {
			b.Fatal(err)
		}
		n := 0
		s2.LoadCatalogue(ctx)
		s2.LoadItems(ctx, func(store.Item) error { n++; return nil })
		for _, r := range rs {
			cs, _ := s2.LoadCommits(ctx, r.Key, 30)
			n += len(cs)
		}
		if n != 34000 {
			b.Fatal(n)
		}
		s2.Close(ctx)
	}
}

var _ = storetest.NSBig
