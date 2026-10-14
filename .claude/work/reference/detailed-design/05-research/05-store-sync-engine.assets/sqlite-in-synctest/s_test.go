package s

import (
	"database/sql"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteInsideBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "x.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		if _, err := db.Exec(`create table t(a)`); err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		for i := 0; i < 3; i++ {
			db.Exec(`insert into t values(1)`)
		}
		t.Logf("fake time elapsed during real file I/O: %v", time.Since(t0))
		time.Sleep(time.Second)
		synctest.Wait()
		t.Log("bubble idle OK")
	})
}
