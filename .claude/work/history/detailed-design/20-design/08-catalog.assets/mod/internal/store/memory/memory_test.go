package memory_test

import (
	"errors"
	"testing"

	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/memory"
	"github.com/cantara/source-code-portal/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, storetest.Harness{
		New: func(t *testing.T, c storetest.Config) store.Store {
			return memory.New(memory.Options{CacheNamespaces: c.CacheNamespaces})
		},
		Degrade: func(t *testing.T, s store.Store) {
			s.(*memory.Store).SetHealth(store.Health{OK: false, Reason: "injected"})
		},
	})
}

func TestFailNext(t *testing.T) {
	s := memory.New(memory.Options{})
	boom := errors.New("boom")
	s.FailNext("LoadCatalogue", boom)
	if _, err := s.LoadCatalogue(t.Context()); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := s.LoadCatalogue(t.Context()); err != nil {
		t.Fatal("FailNext fires once")
	}
}
