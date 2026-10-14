package bench

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type drv struct{ name, driver, dsn string }

func drivers(path string, sync string) []drv {
	return []drv{
		{"modernc", "sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(%s)&_pragma=foreign_keys(1)&_txlock=immediate", path, sync)},
		{"ncruces", "sqlite3", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(wal)&_pragma=synchronous(%s)&_pragma=foreign_keys(1)&_txlock=immediate", path, sync)},
	}
}

const nRepos = 1000

var readme = strings.Repeat("h", 25_000)
var raw = strings.Repeat("r", 20_000)
var commitBlob = strings.Repeat("m", 400)

func populate(db *sql.DB) error {
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for i := 0; i < nRepos; i++ {
		name := fmt.Sprintf("repo-%04d", i)
		if _, err := tx.Exec(`INSERT INTO repo VALUES('o',?,?,?)`, name, []byte(strings.Repeat("c", 1500)), now); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO item(org,repo,kind,etag,content_sha,fetched_at,status,payload) VALUES('o',?,'readme','W/"abc"','sha',?, 'ok', ?)`, name, now, []byte(readme+raw)); err != nil {
			return err
		}
		for _, k := range []string{"status.build", "status.issues", "status.release"} {
			if _, err := tx.Exec(`INSERT INTO item(org,repo,kind,fetched_at,status,payload) VALUES('o',?,?,?, 'ok', ?)`, name, k, now, []byte(`{"s":"ok"}`)); err != nil {
				return err
			}
		}
		for j := 0; j < 30; j++ {
			if _, err := tx.Exec(`INSERT INTO commit_log VALUES('o',?,?,?,?)`, name, fmt.Sprintf("%040d", j), now-int64(j*3600), []byte(commitBlob)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func warmLoad(db *sql.DB) (int, int, error) {
	n, bytes := 0, 0
	rows, err := db.Query(`SELECT repo, kind, etag, fetched_at, status, payload FROM item WHERE org='o'`)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var repo, kind, status string
		var etag sql.NullString
		var at int64
		var p []byte
		if err := rows.Scan(&repo, &kind, &etag, &at, &status, &p); err != nil {
			return 0, 0, err
		}
		n++
		bytes += len(p)
	}
	rows.Close()
	rows, err = db.Query(`SELECT repo, sha, committed_at, payload FROM commit_log WHERE org='o' ORDER BY repo, committed_at DESC`)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var repo, sha string
		var at int64
		var p []byte
		if err := rows.Scan(&repo, &sha, &at, &p); err != nil {
			return 0, 0, err
		}
		n++
		bytes += len(p)
	}
	return n, bytes, rows.Close()
}

func TestWarmLoadAndOpen(t *testing.T) {
	for _, sy := range []string{"NORMAL", "FULL"} {
		for _, d := range drivers("", sy) {
			dir := t.TempDir()
			path := filepath.Join(dir, "scp.db")
			dd := drivers(path, sy)[map[string]int{"modernc": 0, "ncruces": 1}[d.name]]
			t0 := time.Now()
			db, err := sql.Open(dd.driver, dd.dsn)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			if err := db.Ping(); err != nil {
				t.Fatal(err)
			}
			openT := time.Since(t0)
			t0 = time.Now()
			if err := populate(db); err != nil {
				t.Fatal(err)
			}
			popT := time.Since(t0)
			db.Close()
			fi, _ := os.Stat(path)
			wal, _ := os.Stat(path + "-wal")
			var walSize int64
			if wal != nil {
				walSize = wal.Size()
			}
			// reopen (cold process-level, warm OS cache) and warm-load
			t0 = time.Now()
			db, err = sql.Open(dd.driver, dd.dsn)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			if err := db.Ping(); err != nil {
				t.Fatal(err)
			}
			reopenT := time.Since(t0)
			t0 = time.Now()
			n, by, err := warmLoad(db)
			if err != nil {
				t.Fatal(err)
			}
			loadT := time.Since(t0)
			t0 = time.Now()
			var res string
			db.QueryRow(`PRAGMA quick_check`).Scan(&res)
			qcT := time.Since(t0)
			t0 = time.Now()
			db.QueryRow(`PRAGMA integrity_check`).Scan(&res)
			icT := time.Since(t0)
			var ver string
			db.QueryRow(`select sqlite_version()`).Scan(&ver)
			// per-item upsert commit latency
			t0 = time.Now()
			const N = 300
			for i := 0; i < N; i++ {
				_, err := db.Exec(`INSERT INTO item(org,repo,kind,fetched_at,status,payload) VALUES('o',?,'readme',?, 'ok', ?)
				  ON CONFLICT(org,repo,kind) DO UPDATE SET fetched_at=excluded.fetched_at, payload=excluded.payload, etag='x'`, fmt.Sprintf("repo-%04d", i), time.Now().Unix(), []byte(readme))
				if err != nil {
					t.Fatal(err)
				}
			}
			upT := time.Since(t0) / N
			t0 = time.Now()
			dst := filepath.Join(dir, "backup.db")
			if _, err := db.Exec(`VACUUM INTO '` + dst + `'`); err != nil {
				t.Fatal(err)
			}
			viT := time.Since(t0)
			bi, _ := os.Stat(dst)
			db.Close()
			t.Logf("sync=%-6s %-8s sqlite=%s open=%v populate(1 tx, 1000 repos)=%v db=%.1fMB wal=%.1fMB | reopen=%v warmload(%d rows, %.1fMB)=%v quick_check=%v integrity_check=%v | autocommit upsert(25KB)=%v/op | VACUUM INTO=%v (%.1fMB)",
				sy, d.name, ver, openT.Round(time.Millisecond), popT.Round(time.Millisecond), float64(fi.Size())/1e6, float64(walSize)/1e6, reopenT.Round(time.Millisecond), n, float64(by)/1e6, loadT.Round(time.Millisecond), qcT.Round(time.Millisecond), icT.Round(time.Millisecond), upT.Round(time.Microsecond), viT.Round(time.Millisecond), float64(bi.Size())/1e6)
		}
	}
}

// One writer (MaxOpenConns=1 write pool) + 4-conn read pool; readers run continuously while writer upserts.
func TestConcurrentReadWrite(t *testing.T) {
	for _, d := range drivers("", "NORMAL") {
		dir := t.TempDir()
		path := filepath.Join(dir, "scp.db")
		dd := drivers(path, "NORMAL")[map[string]int{"modernc": 0, "ncruces": 1}[d.name]]
		w, err := sql.Open(dd.driver, dd.dsn)
		if err != nil {
			t.Fatal(err)
		}
		w.SetMaxOpenConns(1)
		if err := populate(w); err != nil {
			t.Fatal(err)
		}
		rdsn := strings.Replace(dd.dsn, "&_txlock=immediate", "", 1) + "&_pragma=query_only(1)"
		r, err := sql.Open(dd.driver, rdsn)
		if err != nil {
			t.Fatal(err)
		}
		r.SetMaxOpenConns(4)
		var reads atomic.Int64
		var busy atomic.Int64
		var wg sync.WaitGroup
		stop := make(chan struct{})
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					var n int
					if err := r.QueryRow(`SELECT count(*) FROM commit_log WHERE org='o' AND repo=?`, "repo-0042").Scan(&n); err != nil {
						busy.Add(1)
					}
					reads.Add(1)
				}
			}()
		}
		t0 := time.Now()
		writes := 0
		var wbusy int
		for time.Since(t0) < 3*time.Second {
			if _, err := w.Exec(`INSERT INTO item(org,repo,kind,fetched_at,status,payload) VALUES('o',?,'readme',?, 'ok', ?)
				  ON CONFLICT(org,repo,kind) DO UPDATE SET fetched_at=excluded.fetched_at, payload=excluded.payload`, fmt.Sprintf("repo-%04d", writes%1000), time.Now().Unix(), []byte(readme)); err != nil {
				wbusy++
			}
			writes++
		}
		close(stop)
		wg.Wait()
		t.Logf("%-8s 3s mixed: writes=%d (%.0f/s, errors=%d)  reads=%d (%.0f/s, errors=%d)", d.name, writes, float64(writes)/3, wbusy, reads.Load(), float64(reads.Load())/3, busy.Load())
		w.Close()
		r.Close()
	}
}
