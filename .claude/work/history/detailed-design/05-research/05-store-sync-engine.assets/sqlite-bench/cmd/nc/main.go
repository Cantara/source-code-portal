package main

import (
	"database/sql"
	"fmt"
	_ "github.com/ncruces/go-sqlite3/driver"
)

func main() {
	db, _ := sql.Open("sqlite3", "file:/tmp/x?_pragma=journal_mode(wal)")
	var v string
	err := db.QueryRow("select sqlite_version()").Scan(&v)
	fmt.Println(v, err)
}
