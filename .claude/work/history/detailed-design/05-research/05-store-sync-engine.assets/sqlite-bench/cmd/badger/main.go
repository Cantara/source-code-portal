package main

import (
	"fmt"
	"os"

	badger "github.com/dgraph-io/badger/v4"
)

func main() {
	d, _ := os.MkdirTemp("", "bg")
	db, err := badger.Open(badger.DefaultOptions(d))
	if err != nil {
		panic(err)
	}
	fmt.Println(db.Update(func(tx *badger.Txn) error { return tx.Set([]byte("a"), []byte("b")) }), db.Close())
}
