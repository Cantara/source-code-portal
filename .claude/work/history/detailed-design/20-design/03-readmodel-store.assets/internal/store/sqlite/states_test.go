package sqlite

import (
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/freshness"
)

// The item.status CHECK list in 0001_init.sql must be exactly freshness.States() (the kernel states that "the SQL CHECK
// constraint is generated from and tested against States()").
func TestItemStatusCheckMatchesFreshnessStates(t *testing.T) {
	s, _ := open(t, testConfig(t), nil)
	var ddl string
	if err := s.ro.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='item'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	i := strings.Index(ddl, "status IN (")
	if i < 0 {
		t.Fatal("no status CHECK")
	}
	list := ddl[i+len("status IN (") : i+strings.Index(ddl[i:], ")")]
	got := map[string]bool{}
	for _, f := range strings.Split(list, ",") {
		got[strings.Trim(strings.TrimSpace(f), "'")] = true
	}
	if len(got) != len(freshness.States()) {
		t.Fatalf("CHECK has %v, kernel has %v", got, freshness.States())
	}
	for _, st := range freshness.States() {
		if !got[string(st)] {
			t.Errorf("state %q missing from the CHECK", st)
		}
	}
}
