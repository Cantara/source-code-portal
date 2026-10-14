package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// V-06: the in-memory byte accounting must equal SUM(size) after concurrent puts and never exceed the cap.
func TestCacheAccountingUnderConcurrentPuts(t *testing.T) {
	for round := 0; round < 3; round++ {
		s, _ := open(t, testConfig(t), map[string]int64{"t.big": 20000})
		var wg sync.WaitGroup
		for g := 0; g < 16; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := 0; i < 300; i++ {
					_ = s.PutCache(context.Background(), "t.big", fmt.Sprintf("k%d-%d", g, i%30), make([]byte, 300+(i*g)%11))
				}
			}(g)
		}
		wg.Wait()
		var actual int64
		if err := s.ro.QueryRow(`SELECT coalesce(sum(size),0) FROM cache WHERE ns='t.big'`).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if got := s.Info().CacheBytes["t.big"]; got != actual || actual > 20000 {
			t.Fatalf("round %d: accounted %d, actual %d, cap 20000", round, got, actual)
		}
		_ = s.Close(context.Background())
	}
}
