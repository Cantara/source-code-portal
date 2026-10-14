package bench

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Proves the migration-runner shape works on both drivers: multi-statement script + PRAGMA user_version inside one IMMEDIATE tx,
// rollback leaves user_version untouched.
func TestMigrationShapeBothDrivers(t *testing.T) {
	ddl, _ := os.ReadFile("ddl/0001_init.sql")
	for _, d := range drivers("", "NORMAL") {
		path := filepath.Join(t.TempDir(), "m.db")
		dd := drivers(path, "NORMAL")[map[string]int{"modernc": 0, "ncruces": 1}[d.name]]
		db, err := sql.Open(dd.driver, dd.dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		ctx := context.Background()
		apply := func(version int, script string) error {
			tx, err := db.BeginTx(ctx, nil) // _txlock=immediate in the DSN
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, script); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
				return err
			}
			return tx.Commit()
		}
		if err := apply(1, string(ddl)); err != nil {
			t.Fatalf("%s: %v", d.name, err)
		}
		if err := apply(2, "CREATE TABLE ok(a); THIS IS BROKEN"); err == nil {
			t.Fatalf("%s: broken migration must fail", d.name)
		}
		var v int
		db.QueryRow("PRAGMA user_version").Scan(&v)
		var n int
		db.QueryRow("select count(*) from sqlite_master where name='ok'").Scan(&n)
		if v != 1 || n != 0 {
			t.Fatalf("%s: user_version=%d ok-table=%d (rollback failed)", d.name, v, n)
		}
		t.Logf("%s: multi-statement DDL + user_version in one tx OK; failed migration rolled back (user_version=%d)", d.name, v)
		db.Close()
	}
}
