package main

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
)

func main() {
	db, _ := sql.Open("sqlite", "file:/tmp/x?_pragma=journal_mode(WAL)")
	var v string
	err := db.QueryRow("select sqlite_version()").Scan(&v)
	fmt.Println(v, err)
}
