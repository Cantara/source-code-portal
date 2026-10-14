package npm_test

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

func parseOne(t *testing.T, name string, data []byte, lim manifests.Limits) (manifests.Parsed, time.Duration) {
	t.Helper()
	reg := newReg(t)
	ps := reg.Match(name)
	if len(ps) != 1 {
		t.Fatalf("%s matched %d parsers", name, len(ps))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	out, err := ps[0].Parse(ctx, manifests.File{Path: name, Data: data, BlobSHA: "b"}, lim)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	parsertest.CheckParse(t, reg, ps[0], name, data)
	return out, time.Since(start)
}

func hasCode(p manifests.Parsed, code string) bool {
	for _, d := range p.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestAdversarial(t *testing.T) {
	deepJSON := strings.Repeat("[", 100000)
	deepYAML := "a: " + strings.Repeat("[", 100000)
	laughs := "a: &a [x,x,x,x,x,x,x,x,x]\n"
	prev := "a"
	for _, n := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		laughs += fmt.Sprintf("%s: &%s [*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s]\n", n, n, prev, prev, prev, prev, prev, prev, prev, prev, prev)
		prev = n
	}
	var many strings.Builder
	many.WriteString(`{"name":"x","dependencies":{`)
	for i := 0; i < 5000; i++ {
		if i > 0 {
			many.WriteByte(',')
		}
		fmt.Fprintf(&many, `"d%d":"^1.0.0"`, i)
	}
	many.WriteString(`}}`)
	cases := []struct {
		name  string
		file  string
		data  []byte
		lim   manifests.Limits
		code  string
		maxMS int
	}{
		{"json-depth-bomb", "package.json", []byte(deepJSON), manifests.Limits{}, "limit.depth", 1000},
		{"json-node-budget", "package.json", []byte(many.String()), manifests.Limits{MaxNodes: 1000}, "limit.nodes", 1000},
		{"json-size", "package.json", []byte(`{"name":"` + strings.Repeat("a", 1<<20) + `"}`), manifests.Limits{}, "limit.size", 100},
		{"lock-size", "package-lock.json", []byte(strings.Repeat(" ", 8<<20+1)), manifests.Limits{}, "limit.size", 100},
		{"yaml-depth-bomb", "pnpm-workspace.yaml", []byte(deepYAML), manifests.Limits{}, "", 2000},
		{"yaml-alias-bomb-lock", "pnpm-lock.yaml", []byte("lockfileVersion: '9.0'\n" + laughs), manifests.Limits{}, "", 2000},
		{"yaml-alias-bomb-workspace", "pnpm-workspace.yaml", []byte(laughs), manifests.Limits{}, "", 2000},
		{"utf16-bom", "package.json", []byte("\xff\xfe{\x00}\x00"), manifests.Limits{}, "encoding.unsupported", 100},
		{"nul-byte", "package.json", []byte("{\"name\":\"a\x00b\"}"), manifests.Limits{}, "encoding.unsupported", 100},
		{"invalid-utf8", "yarn.lock", []byte("a@1:\n  version \"\xff\xfe\"\n"), manifests.Limits{}, "encoding.unsupported", 100},
		{"json-syntax", "package-lock.json", []byte(`{"packages": {`), manifests.Limits{}, "npm.syntax", 100},
		{"json-trailing-garbage", "package.json", []byte(`{"name":"a"} x`), manifests.Limits{}, "npm.syntax", 100},
		{"yaml-syntax", "pnpm-lock.yaml", []byte("importers: [\n  - : :\n"), manifests.Limits{}, "npm.syntax", 100},
		{"bun-binary", "bun.lockb", []byte("\x00\x01\x02"), manifests.Limits{}, "npm.bun-binary-lockfile", 100},
		{"bun-nan", "bun.lock", []byte(`{"lockfileVersion": 1, "packages": {"a": 5, "b": [], "c": [7]}}`), manifests.Limits{}, "npm.lock-entry-invalid", 100},
		{"yarn-garbage", "yarn.lock", []byte("\n\n  indented first\nx:\n    deeper\n"), manifests.Limits{}, "", 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, d := parseOne(t, c.file, c.data, c.lim)
			if c.code != "" && !hasCode(out, c.code) {
				t.Errorf("want diagnostic %s, got %+v", c.code, out.Diagnostics)
			}
			if d > time.Duration(c.maxMS)*time.Millisecond {
				t.Errorf("took %v, budget %dms", d, c.maxMS)
			}
		})
	}
}

// TestLockCycles: dependency cycles, self links and dangling edges terminate and stay in range.
func TestLockCycles(t *testing.T) {
	lock := `{"lockfileVersion":3,"packages":{
 "":{"dependencies":{"a":"*","self":"*","l1":"*"}},
 "node_modules/a":{"version":"1.0.0","dependencies":{"b":"*","a":"*","ghost":"*"}},
 "node_modules/b":{"version":"1.0.0","dependencies":{"a":"*"}},
 "node_modules/self":{"link":true,"resolved":"node_modules/self"},
 "node_modules/l1":{"link":true,"resolved":"node_modules/l2"},
 "node_modules/l2":{"link":true,"resolved":"node_modules/l1"}}}`
	out, _ := parseOne(t, "package-lock.json", []byte(lock), manifests.Limits{})
	if len(out.Locked) != 2 {
		t.Fatalf("locked = %d, want 2", len(out.Locked))
	}
	for _, l := range out.Locked {
		if len(l.DependsOn) != 1 {
			t.Errorf("%s depends on %v, want exactly the other node", l.ID.Name, l.DependsOn)
		}
	}
}

// TestSecretsNeverLeak: credentials embedded in specifiers never reach the output.
func TestSecretsNeverLeak(t *testing.T) {
	pkg := `{"name":"s","dependencies":{
 "a":"git+https://user:hunter2@github.com/x/a.git#v1",
 "b":"https://tok3n@example.com/b.tgz",
 "c":"git+ssh://git@github.com/x/c.git",
 "d":"https://example.com/d.tgz?token=abc"}}`
	out, _ := parseOne(t, "package.json", []byte(pkg), manifests.Limits{})
	b, _ := json.Marshal(out)
	for _, s := range []string{"hunter2", "tok3n", "user:"} {
		if strings.Contains(string(b), s) {
			t.Errorf("output contains %q: %s", s, b)
		}
	}
}

// TestHostileText: bidi and control characters are removed from every emitted string.
func TestHostileText(t *testing.T) {
	pkg := "{\"name\":\"ev‮il\",\"version\":\"1\u0007.0\",\"dependencies\":{\"x⁦y\":\"^1‎.0\"}}"
	out, _ := parseOne(t, "package.json", []byte(pkg), manifests.Limits{})
	b, _ := json.Marshal(out)
	if strings.ContainsAny(string(b), "‮⁦‎") || strings.Contains(string(b), `\u0007`) {
		t.Errorf("control characters survive: %s", b)
	}
}

// TestWorkspaceGlobBounds: pathological patterns finish quickly and never match outside the repository.
func TestWorkspaceGlobBounds(t *testing.T) {
	root := `{"name":"r","workspaces":["` + strings.Repeat("**/", 14) + `x","../../etc","/abs","a/../../b","!**"]}`
	tree := map[string][]byte{"package.json": []byte(root)}
	for i := 0; i < 300; i++ {
		tree[fmt.Sprintf("a/b/c/d/e/f/g/p%d/package.json", i)] = []byte(fmt.Sprintf(`{"name":"p%d"}`, i))
	}
	reg := newReg(t)
	start := time.Now()
	res, err := manifests.RunTree(context.Background(), reg, parsertest.TreeConfig(), tree), error(nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("assembly took %v", d)
	}
	for _, u := range res.Units {
		for _, m := range u.Members {
			if strings.HasPrefix(m, "..") || strings.HasPrefix(m, "/") {
				t.Errorf("member escapes: %q", m)
			}
		}
	}
}
