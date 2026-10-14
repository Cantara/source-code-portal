package sqlite

import (
	"net/url"
	"path/filepath"
	"strconv"
)

type role int

const (
	roleRW role = iota // the single writer
	roleRO             // warm load, admin reads, GetCache
	roleBK             // backup and check: read-only, never query_only (VACUUM INTO fails under query_only)
)

// buildDSN returns a file: URI. The path is escaped by net/url (spaces, '?', '#', '%'), so a configurable path can never add
// a parameter or a pragma; combined with sqlite.StrictPragmas(true) no input reaches SQL text.
func buildDSN(path string, r role, c Config) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := url.Values{}
	add := func(p string) { q.Add("_pragma", p) }
	add("busy_timeout(" + strconv.FormatInt(c.BusyTimeout.Milliseconds(), 10) + ")")
	add("temp_store(2)") // MEMORY: the container root filesystem is read-only and distroless has no /tmp
	switch r {
	case roleRW:
		add("journal_mode(WAL)")
		add("synchronous(" + c.Sync + ")")
		add("foreign_keys(1)")
		add("trusted_schema(0)")
		add("journal_size_limit(" + strconv.FormatInt(int64(c.JournalSizeLimit), 10) + ")")
		q.Set("_txlock", "immediate")
	case roleRO:
		q.Set("mode", "ro")
		add("query_only(1)")
		add("trusted_schema(0)")
	case roleBK:
		q.Set("mode", "ro")
		add("trusted_schema(0)")
	}
	return u.String() + "?" + q.Encode()
}
