package sqlite

import (
	"errors"

	"modernc.org/sqlite"
)

// driver is the seam for the escape-hatch driver (ncruces/go-sqlite3): the database/sql driver name, the extraction of the
// SQLite primary result code from a driver error, and the process-wide strictness switch (modernc only).
type driver struct {
	name   string
	code   func(error) (int, bool)
	strict func()
}

// defaultDriver is modernc.org/sqlite. A function, not a package variable (G5).
func defaultDriver() driver {
	return driver{
		name: "sqlite",
		code: func(err error) (int, bool) {
			var se *sqlite.Error
			if errors.As(err, &se) {
				return se.Code() & 0xff, true
			}
			return 0, false
		},
		strict: func() { sqlite.StrictPragmas(true) },
	}
}
