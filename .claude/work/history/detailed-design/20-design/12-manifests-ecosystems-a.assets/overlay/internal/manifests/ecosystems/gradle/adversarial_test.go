package gradle_test

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

func runTree(t *testing.T, tree map[string][]byte, lim manifests.Limits) (manifests.TreeResult, time.Duration) {
	t.Helper()
	start := time.Now()
	res, err := manifests.RunTree(context.Background(), newReg(t), parsertest.TreeConfigLimits(lim), tree), error(nil)
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

func TestLexicalBombs(t *testing.T) {
	cases := []struct {
		name, file string
		data       string
		lim        manifests.Limits
		want       string
	}{
		{"brace-depth", "build.gradle", strings.Repeat("a {", 100000), manifests.Limits{}, "limit.depth"},
		{"paren-depth", "build.gradle", "dependencies { implementation " + strings.Repeat("(", 100000) + " }", manifests.Limits{}, ""},
		{"unterminated-string", "build.gradle.kts", "dependencies { implementation(\"a:b:" + strings.Repeat("x", 5000), manifests.Limits{}, ""},
		{"unterminated-comment", "build.gradle", "/* " + strings.Repeat("x", 5000), manifests.Limits{}, ""},
		{"template-brace-bomb", "build.gradle.kts", "x(\"" + strings.Repeat("${", 50000), manifests.Limits{}, ""},
		{"token-budget", "build.gradle", strings.Repeat("a b c d e f g h\n", 1000), manifests.Limits{MaxNodes: 500}, "limit.nodes"},
		{"size", "build.gradle", strings.Repeat(" ", 1<<20+1), manifests.Limits{}, "limit.size"},
		{"nul", "build.gradle", "x\x00y", manifests.Limits{}, "encoding.unsupported"},
		{"utf16", "settings.gradle", "\xff\xfei\x00", manifests.Limits{}, "encoding.unsupported"},
		{"toml-depth", "gradle/libs.versions.toml", "[" + strings.Repeat("a.", 100000) + "a]\nx=1\n", manifests.Limits{}, "limit.depth"},
		{"toml-syntax", "gradle/libs.versions.toml", "[versions\nx", manifests.Limits{}, "gradle.syntax"},
		{"lock-garbage", "gradle.lockfile", strings.Repeat("::=\n", 1000), manifests.Limits{}, "gradle.lockfile-syntax"},
		{"props-long-continuation", "gradle.properties", strings.Repeat("a=b\\\n", 100000), manifests.Limits{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, d := runTree(t, map[string][]byte{c.file: []byte(c.data)}, c.lim)
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

// TestTemplateBomb: variables that double at each level hit limit.expand; cycles are reported, not looped.
func TestTemplateBomb(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "val v%d = \"${v%d}${v%d}${v%d}${v%d}\"\n", i, i+1, i+1, i+1, i+1)
	}
	b.WriteString("val v40 = \"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"\ndependencies { implementation(\"g:a:${v0}\") }\n")
	res, d := runTree(t, map[string][]byte{"build.gradle.kts": []byte(b.String())}, manifests.Limits{})
	if codes(res)["limit.expand"] == 0 {
		t.Errorf("want limit.expand, got %v", codes(res))
	}
	if d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

// TestSecrets: credentials in gradle.properties and in URLs of settings never reach the output.
func TestSecrets(t *testing.T) {
	tree := map[string][]byte{
		"gradle.properties":   []byte("signing.password=hunter2\nmavenCentralPassword=pw3\nghToken=ghp_abc\nplain=ok\nmyApiKey=k3y\nsonatypeUsername=bob\n"),
		"settings.gradle.kts": []byte("rootProject.name = \"x\"\nincludeBuild(\"https://user:topsecret@example.com/build\")\n"),
		"build.gradle.kts":    []byte("group = \"g\"\nversion = \"1\"\n"),
	}
	res, _ := runTree(t, tree, manifests.Limits{})
	b, _ := json.Marshal(res)
	for _, s := range []string{"hunter2", "pw3", "ghp_abc", "k3y", "bob", "topsecret"} {
		if strings.Contains(string(b), s) {
			t.Errorf("output contains %q", s)
		}
	}
	if !strings.Contains(string(b), "plain") {
		t.Errorf("non-secret property was dropped")
	}
}

// TestIncludeEscapes: include and includeBuild entries never produce paths outside the repository.
func TestIncludeEscapes(t *testing.T) {
	tree := map[string][]byte{
		"sub/settings.gradle": []byte("include '../../../etc', ':a:../../b', 'x\\y', '', ':'\nincludeFlat '../../escape'\n"),
		"sub/build.gradle":    []byte("group='g'\n"),
	}
	res, _ := runTree(t, tree, manifests.Limits{})
	for _, u := range res.Units {
		if strings.HasPrefix(u.Dir, "..") {
			t.Errorf("unit dir escapes: %q", u.Dir)
		}
		for _, m := range u.Members {
			if strings.HasPrefix(m, "..") || strings.HasPrefix(m, "/") {
				t.Errorf("member escapes: %q", m)
			}
		}
	}
}
