package main

import (
	"fmt"
	bolt "go.etcd.io/bbolt"
)

func main() { db, err := bolt.Open("/tmp/x.db", 0600, nil); fmt.Println(db, err) }
