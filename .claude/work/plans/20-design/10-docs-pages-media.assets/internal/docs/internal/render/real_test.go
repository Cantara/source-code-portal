package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealDocs(t *testing.T) {
	files, _ := filepath.Glob("../../../../testdata/md/*")
	if len(files) < 13 {
		t.Fatalf("the real corpus is missing: %d files", len(files)) // V-13: the first test silently ran on zero files
	}
	p := New()
	for _, f := range files {
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		st := time.Now()
		out, err := p.Markdown(context.Background(), Input{Source: b, Route: testResolver(), Limits: Limits{MaxSourceBytes: 512000}})
		if err != nil || len(out.HTML.String()) == 0 || time.Since(st) > 2*time.Second*timeScale {
			t.Errorf("%s: err=%v html=%d took %v", filepath.Base(f), err, len(out.HTML.String()), time.Since(st))
		}
		fmt.Printf("%-70s %7d B -> %7d B html  %8v err=%v toc=%d\n", filepath.Base(f), len(b), len(out.HTML.String()), time.Since(st).Round(time.Millisecond), err, len(out.TOC))
	}
}
