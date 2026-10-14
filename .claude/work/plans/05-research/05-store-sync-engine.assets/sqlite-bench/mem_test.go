package bench

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func rss() (heap, sys float64) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapInuse) / (1 << 20), float64(m.Sys) / (1 << 20)
}

func TestConnMemory(t *testing.T) {
	for _, idx := range []int{0, 1} {
		path := filepath.Join(t.TempDir(), "m.db")
		d := drivers(path, "NORMAL")[idx]
		h0, s0 := rss()
		db, err := sql.Open(d.driver, d.dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(5)
		if _, err := db.Exec(`create table t(a); insert into t values (1)`); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, _ := db.Conn(t.Context())
				defer c.Close()
				c.ExecContext(t.Context(), `select count(*) from t`)
				var n int
				c.QueryRowContext(t.Context(), `select sum(a) from t`).Scan(&n)
				runtime.Gosched()
				// hold conns concurrently
				for j := 0; j < 1000; j++ {
					c.QueryRowContext(t.Context(), `select 1`).Scan(&n)
				}
			}()
		}
		wg.Wait()
		h1, s1 := rss()
		t.Logf("%-8s 5 open conns: HeapInuse +%.1f MB, runtime Sys +%.1f MB (pool still open)", d.name, h1-h0, s1-s0)
		db.Close()
	}
}
