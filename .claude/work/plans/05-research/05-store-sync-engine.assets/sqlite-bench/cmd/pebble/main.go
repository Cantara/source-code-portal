package main

import (
	"fmt"
	"os"

	"github.com/cockroachdb/pebble/v2"
)

func main() {
	d, _ := os.MkdirTemp("", "pb")
	db, err := pebble.Open(d, &pebble.Options{})
	if err != nil {
		panic(err)
	}
	db.Set([]byte("a"), []byte("b"), pebble.Sync)
	v, c, _ := db.Get([]byte("a"))
	fmt.Println(string(v), db.Close(), c.Close())
}
