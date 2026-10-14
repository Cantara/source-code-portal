package python_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/python"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

func run(t *testing.T, tree map[string][]byte, lim manifests.Limits) (manifests.TreeResult, time.Duration) {
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

func TestBombs(t *testing.T) {
	cases := []struct {
		name, file, data string
		lim              manifests.Limits
		want             string
	}{
		{"toml-key-depth", "pyproject.toml", "[" + strings.Repeat("a.", 100000) + "a]\nx=1\n", manifests.Limits{}, "limit.depth"},
		{"toml-array-depth", "pyproject.toml", "a = " + strings.Repeat("[", 100000), manifests.Limits{}, "python.syntax"},
		{"toml-size", "uv.lock", strings.Repeat("#", 8<<20+1), manifests.Limits{}, "limit.size"},
		{"requirements-size", "requirements.txt", strings.Repeat("a\n", 600000), manifests.Limits{}, "limit.size"},
		{"requirements-nul", "requirements.txt", "a==1\x00\n", manifests.Limits{}, "encoding.unsupported"},
		{"requirements-utf16", "requirements.txt", "\xff\xfea\x00", manifests.Limits{}, "encoding.unsupported"},
		{"hash-empty", "requirements.txt", "a==1 --hash=\n", manifests.Limits{}, ""},
		{"marker-bomb", "requirements.txt", "a; " + strings.Repeat("(", 20000) + "python_version>'3'" + strings.Repeat(")", 20000) + "\n", manifests.Limits{}, ""},
		{"extras-bomb", "requirements.txt", "a[" + strings.Repeat("x,", 100000) + "x]==1\n", manifests.Limits{}, ""},
		{"line-continuations", "requirements.txt", strings.Repeat("a \\\n", 200000), manifests.Limits{}, ""},
		{"group-cycle", "pyproject.toml", "[dependency-groups]\na=[{include-group='b'}]\nb=[{include-group='a'}]\nc=[{include-group='c'}]\n", manifests.Limits{}, "python.group-cycle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, d := run(t, map[string][]byte{c.file: []byte(c.data)}, c.lim)
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

// TestIncludeGraph: requirement includes that cycle, nest too deep or escape the repository stay inside the fetched set.
func TestIncludeGraph(t *testing.T) {
	tree := map[string][]byte{
		"a/requirements.txt":    []byte("-r requirements-b.txt\nx==1\n"),
		"a/requirements-b.txt":  []byte("-r requirements.txt\n-r ../../../etc/passwd\n-r /abs/path\n-r https://evil.example/r.txt\ny==2\n"),
		"deep/requirements.txt": []byte("-r requirements-d1.txt\n"),
	}
	for i := 1; i < 40; i++ {
		tree[fmt.Sprintf("deep/requirements-d%d.txt", i)] = []byte(fmt.Sprintf("-r requirements-d%d.txt\npkg%d==1\n", i+1, i))
	}
	res, d := run(t, tree, manifests.Limits{})
	c := codes(res)
	if c["python.include-cycle"] == 0 || c["python.include-outside-repo"] < 3 {
		t.Errorf("diagnostics: %v", c)
	}
	if d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

// TestSecrets: credentials in index URLs and direct references never reach the output.
func TestSecrets(t *testing.T) {
	tree := map[string][]byte{
		"requirements.txt": []byte("--index-url https://user:pw1@pypi.example.com/simple\n--extra-index-url https://tok2@x.example/simple\ncorp @ https://user:pw3@git.example/corp-1.0.tar.gz\nother @ git+https://tok4@github.com/o/r.git@v1\n"),
		"pyproject.toml":   []byte("[project]\nname='p'\nversion='1'\ndependencies=['z @ https://bob:pw5@example.com/z.whl']\n"),
	}
	res, _ := run(t, tree, manifests.Limits{})
	b, _ := json.Marshal(res)
	for _, s := range []string{"pw1", "tok2", "pw3", "tok4", "pw5", "bob:", "user:"} {
		if strings.Contains(string(b), s) {
			t.Errorf("output contains %q", s)
		}
	}
}

// TestPyprojectLocatorIsLinear (VF-12-02): the line of an array element is found with an index built in one pass; a 1 MiB dependency array on ONE line
// (or on 100,000 lines) must not be quadratic. The first version took 11.4 s for the single-line case.
func TestPyprojectLocatorIsLinear(t *testing.T) {
	var one, many strings.Builder
	one.WriteString("[project]\nname=\"x\"\ndependencies=[")
	many.WriteString("[project]\nname=\"x\"\ndependencies=[\n")
	for i := 0; one.Len() < 1<<20-4000; i++ {
		fmt.Fprintf(&one, "\"p%d>=1\",", i)
	}
	for i := 0; many.Len() < 1<<20-4000; i++ {
		fmt.Fprintf(&many, "  \"p%d>=1\",\n", i)
	}
	one.WriteString("]\n")
	many.WriteString("]\n")
	for name, src := range map[string]string{"one-line": one.String(), "one-per-line": many.String()} {
		res, d := run(t, map[string][]byte{"pyproject.toml": []byte(src)}, manifests.Limits{})
		if d > 3*time.Second {
			t.Errorf("%s: %v", name, d)
		}
		if len(res.Units) != 1 || len(res.Units[0].Dependencies) == 0 {
			t.Errorf("%s: no dependencies (%v)", name, res.Diagnostics)
		}
	}
	// the line of an element is still right
	res, _ := run(t, map[string][]byte{"pyproject.toml": []byte("[project]\nname=\"x\"\ndependencies=[\n  \"a>=1\",\n  \"b>=2\", \"c\"\n]\n")}, manifests.Limits{})
	got := map[string]int32{}
	for _, d := range res.Units[0].Dependencies {
		got[d.Target.Name] = d.Line
	}
	if got["a"] != 4 || got["b"] != 5 || got["c"] != 5 {
		t.Errorf("lines = %v", got)
	}
}

// TestExactPin (VF-12-22): an exported helper must not hand whitespace or a marker back as a version.
func TestExactPin(t *testing.T) {
	for in, want := range map[string]string{"==1.0": "1.0", "== 1.0": "1.0", " ==1.0 ": "1.0", "==1!2.0": "1!2.0", "==1.0+local": "1.0+local", "==1.0.dev1": "1.0.dev1"} {
		if got, ok := python.ExactPin(in); !ok || got != want {
			t.Errorf("ExactPin(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "==", "== ", "===1.0", "==1.0.*", "==1.0,<2", "~=1.0", "!=1.0", ">=1", "==1.0 ; python_version<'3'", "==1 0"} {
		if got, ok := python.ExactPin(in); ok {
			t.Errorf("ExactPin(%q) = %q, want no pin", in, got)
		}
	}
}
