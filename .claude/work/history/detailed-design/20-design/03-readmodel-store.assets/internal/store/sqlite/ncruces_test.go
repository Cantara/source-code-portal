package sqlite

import (
	"context"
	"errors"
	"testing"

	ncsqlite "github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/storetest"
)

// The escape hatch: the same Store on ncruces/go-sqlite3 v0.35.6. Three seams only: driver name, error-code extraction, strictness switch.
func ncruces() driver {
	return driver{
		name: "sqlite3",
		code: func(err error) (int, bool) {
			var e *ncsqlite.Error
			if errors.As(err, &e) {
				return int(e.Code()) & 0xff, true
			}
			return 0, false
		},
		strict: func() {},
	}
}

func TestConformanceNcruces(t *testing.T) {
	var cfg Config
	opn := func(t *testing.T, ns map[string]int64) *Store {
		s, _, err := Open(context.Background(), cfg, Deps{CacheNamespaces: ns, drv: ncruces()})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	storetest.Run(t, storetest.Harness{
		New: func(t *testing.T, c storetest.Config) store.Store {
			cfg = testConfig(t)
			return opn(t, c.CacheNamespaces)
		},
		Reopen: func(t *testing.T, s store.Store) store.Store {
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			return opn(t, storetest.DefaultConfig().CacheNamespaces)
		},
		Degrade: func(t *testing.T, s store.Store) { s.(*Store).markDegraded("test", errors.New("injected")) },
	})
}

func TestNcrucesBackupAndCheck(t *testing.T) {
	cfg := testConfig(t)
	s, _, err := Open(context.Background(), cfg, Deps{drv: ncruces()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	populate(t, s)
	bak := cfg.Path + ".bak"
	if err := s.Backup(context.Background(), bak); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
}
