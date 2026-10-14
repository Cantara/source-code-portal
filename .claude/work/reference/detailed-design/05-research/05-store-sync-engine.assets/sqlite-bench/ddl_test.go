package bench

import (
	"database/sql"
	_ "embed"
	"os"
	"path/filepath"
	"testing"
)

func TestDDLAndStatements(t *testing.T) {
	ddl, _ := os.ReadFile("ddl/0001_init.sql")
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "d.db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	must := func(q string, a ...any) sql.Result {
		r, err := db.Exec(q, a...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return r
	}
	must(string(ddl))
	must(`INSERT INTO repo(org,name,seen_at,meta) VALUES('o','r1',1,'{}')`)
	// upsert success (200)
	up := `INSERT INTO item(repo_id,kind,status,fetched_at,checked_at,next_due_at,attempts,etag,content_sha,error,payload)
	       VALUES(1,'readme','ok',?,?,?,0,?,?,NULL,?)
	       ON CONFLICT(repo_id,kind) DO UPDATE SET status=excluded.status, fetched_at=excluded.fetched_at, checked_at=excluded.checked_at,
	         next_due_at=excluded.next_due_at, attempts=0, etag=excluded.etag, content_sha=excluded.content_sha, error=NULL, payload=excluded.payload`
	must(up, 100, 100, 4000, `W/"a"`, "sha1", []byte("<p>hi</p>"))
	// 304 revalidation: no payload rewrite
	r := must(`UPDATE item SET fetched_at=?, checked_at=?, next_due_at=?, attempts=0, error=NULL, status='ok' WHERE repo_id=1 AND kind='readme'`, 200, 200, 4100)
	// failure: stale-while-error keeps payload and fetched_at
	must(`UPDATE item SET status='error', error=?, checked_at=?, next_due_at=?, attempts=attempts+1 WHERE repo_id=1 AND kind='readme'`, "502", 300, 400)
	var st string
	var fa int
	var pl []byte
	var at int
	db.QueryRow(`SELECT status,fetched_at,payload,attempts FROM item WHERE repo_id=1 AND kind='readme'`).Scan(&st, &fa, &pl, &at)
	if st != "error" || fa != 200 || string(pl) != "<p>hi</p>" || at != 1 {
		t.Fatal(st, fa, string(pl), at)
	}
	_ = r
	// commits: insert-or-ignore then trim to K=3
	for i := 0; i < 6; i++ {
		must(`INSERT OR IGNORE INTO commit_log(repo_id,sha,committed_at,payload) VALUES(1,?,?,?)`, string(rune('a'+i)), 1000+i, []byte("{}"))
	}
	must(`DELETE FROM commit_log WHERE repo_id=? AND id NOT IN (SELECT id FROM commit_log WHERE repo_id=? ORDER BY committed_at DESC, id DESC LIMIT ?)`, 1, 1, 3)
	var n int
	db.QueryRow(`SELECT count(*) FROM commit_log`).Scan(&n)
	if n != 3 {
		t.Fatal(n)
	}
	rows, _ := db.Query(`EXPLAIN QUERY PLAN SELECT id FROM commit_log WHERE repo_id=1 ORDER BY committed_at DESC, id DESC LIMIT 3`)
	for rows.Next() {
		var a, b, c int
		var d string
		rows.Scan(&a, &b, &c, &d)
		t.Log("plan:", d)
	}
	rows.Close()
	// cascade on repo disappearance
	must(`DELETE FROM repo WHERE id=1`)
	db.QueryRow(`SELECT (SELECT count(*) FROM item)+(SELECT count(*) FROM commit_log)`).Scan(&n)
	if n != 0 {
		t.Fatal("cascade failed", n)
	}
	var ver, ic string
	db.QueryRow(`select sqlite_version()`).Scan(&ver)
	db.QueryRow(`PRAGMA integrity_check`).Scan(&ic)
	t.Log("sqlite", ver, "integrity", ic)
}
