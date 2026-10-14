package bench

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestBolt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.bolt")
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	pay := []byte(readme + raw)
	t0 := time.Now()
	err = db.Update(func(tx *bolt.Tx) error {
		it, _ := tx.CreateBucketIfNotExists([]byte("item"))
		cm, _ := tx.CreateBucketIfNotExists([]byte("commit"))
		for i := 0; i < nRepos; i++ {
			n := fmt.Sprintf("repo-%04d", i)
			it.Put([]byte(n+"/readme"), pay)
			for _, k := range []string{"build", "issues", "release"} {
				it.Put([]byte(n+"/status."+k), []byte(`{"s":"ok"}`))
			}
			for j := 0; j < 30; j++ {
				cm.Put([]byte(fmt.Sprintf("%s/%040d", n, j)), []byte(strings.Repeat("m", 400)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pop := time.Since(t0)
	db.Close()
	t0 = time.Now()
	db, _ = bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	reopen := time.Since(t0)
	t0 = time.Now()
	n, by := 0, 0
	db.View(func(tx *bolt.Tx) error {
		for _, b := range []string{"item", "commit"} {
			tx.Bucket([]byte(b)).ForEach(func(k, v []byte) error {
				c := make([]byte, len(v))
				copy(c, v)
				n++
				by += len(c)
				return nil
			})
		}
		return nil
	})
	load := time.Since(t0)
	t0 = time.Now()
	for i := 0; i < 300; i++ {
		db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("item")).Put([]byte(fmt.Sprintf("repo-%04d/readme", i)), []byte(readme))
		})
	}
	up := time.Since(t0) / 300
	t0 = time.Now()
	err = db.View(func(tx *bolt.Tx) error {
		for range tx.Check() {
		}
		return nil
	})
	chk := time.Since(t0)
	t.Logf("bbolt %s: populate(1 tx)=%v reopen=%v warmload(%d keys, %.1fMB, copied)=%v upsert(25KB, fsync per tx)=%v/op check=%v err=%v", "v1.5.0", pop.Round(time.Millisecond), reopen.Round(time.Millisecond), n, float64(by)/1e6, load.Round(time.Millisecond), up.Round(time.Microsecond), chk.Round(time.Millisecond), err)
	db.Close()
}
