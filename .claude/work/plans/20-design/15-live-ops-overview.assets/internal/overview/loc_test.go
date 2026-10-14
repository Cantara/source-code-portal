package overview

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLOCTripwire is the executable form of the ADR-001 exit condition: overview stays composition only, and when it
// approaches 2,500 non-test lines (non-blank, non-comment) the unit must be split by output (design 15 section 14).
// 2,300 is the warning line recorded in the log; 2,500 fails the build.
func TestLOCTripwire(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" || strings.HasPrefix(l, "//") {
				continue
			}
			total++
		}
		fh.Close()
	}
	t.Logf("overview non-test lines: %d (warning at 2300, failure at 2500)", total)
	if total > 2500 {
		t.Fatalf("internal/overview has %d non-test lines: the ADR-001 exit condition is met, split the package by output", total)
	}
}
