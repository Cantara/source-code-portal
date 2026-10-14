package cargo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

func run(t *testing.T, tree map[string][]byte) (manifests.TreeResult, time.Duration) {
	t.Helper()
	start := time.Now()
	res, err := manifests.RunTree(context.Background(), newReg(t), parsertest.TreeConfig(), tree), error(nil)
	if err != nil {
		t.Fatal(err)
	}
	return res, time.Since(start)
}

func codes(res manifests.TreeResult) map[string]int {
	m := map[string]int{}
	for _, d := range res.Diagnostics {
		m[d.Code]++
	}
	for _, p := range res.Parsed {
		for _, d := range p.Diagnostics {
			m[d.Code]++
		}
	}
	for _, u := range res.Units {
		for _, d := range u.Diagnostics {
			m[d.Code]++
		}
	}
	return m
}

func TestBombs(t *testing.T) {
	cases := []struct{ name, file, data, want string }{
		{"key-depth", "Cargo.toml", "[" + strings.Repeat("a.", 100000) + "a]\nx=1\n", "limit.depth"},
		{"array-depth", "Cargo.toml", "a = " + strings.Repeat("[", 100000), "cargo.syntax"},
		{"lock-size", "Cargo.lock", strings.Repeat("#", 8<<20+1), "limit.size"},
		{"nul", "Cargo.toml", "[package]\nname = \"a\x00b\"\n", "encoding.unsupported"},
		{"self-dependency", "Cargo.lock", "[[package]]\nname='a'\nversion='1'\ndependencies=['a']\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, d := run(t, map[string][]byte{c.file: []byte(c.data)})
			if c.want != "" && codes(res)[c.want] == 0 {
				t.Errorf("want %s, got %v", c.want, codes(res))
			}
			if d > 3*time.Second {
				t.Errorf("took %v", d)
			}
			reg := newReg(t)
			for _, p := range reg.Match(c.file) {
				parsertest.CheckParse(t, reg, p, c.file, []byte(c.data))
			}
		})
	}
}

// TestWorkspaceBounds: member globs and path dependencies stay inside the repository and finish quickly.
func TestWorkspaceBounds(t *testing.T) {
	tree := map[string][]byte{"Cargo.toml": []byte("[workspace]\nmembers=['../../*','/abs','a/../../b','" + strings.Repeat("**/", 14) + "x','crates/*']\n[dependencies]\nescape={path='../../../etc'}\n")}
	for i := 0; i < 300; i++ {
		tree[fmt.Sprintf("crates/c%d/Cargo.toml", i)] = []byte(fmt.Sprintf("[package]\nname='c%d'\nversion='0.1.0'\n", i))
	}
	res, d := run(t, tree)
	if d > 3*time.Second {
		t.Errorf("took %v", d)
	}
	for _, u := range res.Units {
		for _, m := range u.Members {
			if strings.HasPrefix(m, "..") || strings.HasPrefix(m, "/") {
				t.Errorf("member escapes: %q", m)
			}
		}
	}
}

func TestSecrets(t *testing.T) {
	tree := map[string][]byte{"Cargo.toml": []byte("[package]\nname='s'\nversion='1'\n[dependencies]\na={git='https://user:pw@github.com/o/a.git'}\nb={git='https://tok@example.com/b.git',branch='x'}\n")}
	res, _ := run(t, tree)
	b, _ := json.Marshal(res)
	for _, s := range []string{"user:pw", "tok@", ":pw@"} {
		if strings.Contains(string(b), s) {
			t.Errorf("output contains %q", s)
		}
	}
}
