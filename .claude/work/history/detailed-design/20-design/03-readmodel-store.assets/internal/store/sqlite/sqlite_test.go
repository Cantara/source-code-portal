package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/storetest"
)

func testConfig(t testing.TB) Config {
	c := DefaultConfig()
	c.Path = filepath.Join(t.TempDir(), "scp.db")
	return c
}

func open(t testing.TB, c Config, ns map[string]int64) (*Store, OpenReport) {
	t.Helper()
	s, rep, err := Open(context.Background(), c, Deps{CacheNamespaces: ns, BuildID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return s, rep
}

func TestConformance(t *testing.T) {
	var cfg Config
	storetest.Run(t, storetest.Harness{
		New: func(t *testing.T, c storetest.Config) store.Store {
			cfg = testConfig(t)
			s, _ := open(t, cfg, c.CacheNamespaces)
			return s
		},
		Reopen: func(t *testing.T, s store.Store) store.Store {
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			n, rep := open(t, cfg, storetest.DefaultConfig().CacheNamespaces)
			if rep.Rebuilt != "" || rep.Created {
				t.Fatalf("reopen must not rebuild: %+v", rep)
			}
			return n
		},
		Degrade: func(t *testing.T, s store.Store) { s.(*Store).markDegraded("test", errors.New("injected")) },
	})
}

func TestOpenRejectsBadConfig(t *testing.T) {
	c := DefaultConfig()
	c.Path = ""
	c.Sync = "off"
	_, _, err := Open(context.Background(), c, Deps{})
	if err == nil {
		t.Fatal("want error")
	}
	t.Log(err)
}

func TestSchemaFilesAreContiguous(t *testing.T) {
	ms, err := loadMigrations(migrationsFS, "migrations")
	if err != nil || len(ms) == 0 {
		t.Fatal(err, len(ms))
	}
	_ = os.Stdout
}
