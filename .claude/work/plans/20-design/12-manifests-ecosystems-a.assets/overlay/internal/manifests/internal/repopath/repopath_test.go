package repopath

import (
	"strings"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pat, name string
		want      bool
	}{
		{"packages/*", "packages/a", true},
		{"packages/*", "packages/a/b", false},
		{"packages/**", "packages", true},
		{"packages/**", "packages/a/b/c", true},
		{"**/x", "x", true},
		{"**/x", "a/b/x", true},
		{"**/x", "a/b/y", false},
		{"a/**/b/**/c", "a/b/c", true},
		{"a/**/b/**/c", "a/x/y/b/z/c", true},
		{"a/**/b/**/c", "a/x/y/z/c", false},
		{"[", "a", false},
	}
	for _, c := range cases {
		if got := Match(c.pat, c.name); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pat, c.name, got, c.want)
		}
	}
}

func TestMatchIsPolynomial(t *testing.T) {
	pat := strings.Repeat("**/a/", 7) + "b"
	name := strings.Repeat("a/", 200) + "c"
	start := time.Now()
	if Match(pat, name) {
		t.Error("must not match")
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("took %v", d)
	}
}

func TestRel(t *testing.T) {
	for in, want := range map[string]bool{"a/b": true, "../x": true, "./": true, ".": true, "": false, "/abs": false, "a\\b": false, "a\x00b": false, "a\rb": false, "C:/x": false, strings.Repeat("a", 513): false, "a\u202eb": false} {
		if _, ok := Rel(in); ok != want {
			t.Errorf("Rel(%q) ok=%v, want %v", in, ok, want)
		}
	}
}

func TestJoin(t *testing.T) {
	for _, c := range []struct {
		dir, rel, want string
		ok             bool
	}{
		{"a", "b", "a/b", true}, {"", "b", "b", true}, {"a", "..", "", true}, {"a", "../..", "", false}, {"a", "/x", "", false}, {"a", "x\ny", "", false}, {"a/b", "../c", "a/c", true},
	} {
		got, ok := Join(c.dir, c.rel)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Join(%q, %q) = %q, %v", c.dir, c.rel, got, ok)
		}
	}
}
