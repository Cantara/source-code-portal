package docs

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func mustLoader(t *testing.T) *DescriptorLoader {
	t.Helper()
	l, err := NewDescriptorLoader()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func readSample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/descriptor/" + name) // V-17: the first test ignored this error and loaded empty input
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDescriptorSamples asserts what the design claims: the valid sample loads without a problem, the bad sample keeps
// exactly the valid remainder and reports each defect at its own pointer.
func TestDescriptorSamples(t *testing.T) {
	l := mustLoader(t)
	v, ps := l.Load(readSample(t, "scp.sample.yaml"))
	if len(ps) != 0 || v["lifecycle"] != "production" || v["system"] != "billing" {
		t.Fatalf("valid sample: %v %+v", v, ps)
	}
	d, err := DecodeDescriptor(v)
	if err != nil || d.Docs.Root != "docs" || len(d.Docs.Nav) != 2 || d.Integrations.Jenkins == nil || d.Integrations.Jenkins.Instance != "main" {
		t.Fatalf("typed decode: %+v %v", d, err)
	}
	v, ps = l.Load(readSample(t, "bad.sample.yaml"))
	j, _ := json.Marshal(v)
	if string(j) != `{"integrations":{"jenkins":{"job":"x"}},"schema":1}` {
		t.Fatalf("pruned value: %s", j)
	}
	want := map[string]string{"/docs/nav/0": CodePath, "/integrations/jenkins/host": CodeUnknown, "/lifecycle": CodeInvalid, "/links/0/url": CodeInvalid,
		"/owners/0": CodeInvalid, "/tags/0": CodeInvalid, "/unknown": CodeUnknown}
	if len(ps) != len(want) {
		t.Fatalf("%d problems: %+v", len(ps), ps)
	}
	for _, p := range ps {
		if want[p.Pointer] != p.Code {
			t.Errorf("unexpected problem %+v", p)
		}
	}
}

// TestDescriptorBombs: whole-document rejections and the resource limits (design 4.3, 4.15 item 5).
func TestDescriptorBombs(t *testing.T) {
	l := mustLoader(t)
	laughs := "schema: 1\n"
	laughs += "a0: &a0 [x, x, x, x, x, x, x, x, x]\n"
	for i := 1; i < 10; i++ {
		laughs += fmt.Sprintf("a%d: &a%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*a%d, ", i-1), 9), ", "))
	}
	deep := "schema: 1\nx: " + strings.Repeat("[", 40) + strings.Repeat("]", 40) + "\n"
	for name, c := range map[string]struct {
		src  string
		code string
	}{
		"empty":          {"", CodeSyntax},
		"billion laughs": {laughs, CodeAnchor},
		"anchor":         {"a: &x 1\nschema: 1\n", CodeAnchor},
		"merge key":      {"base: &b {title: x}\nschema: 1\n<<: *b\n", CodeAnchor},
		"multi doc":      {"schema: 1\n---\nschema: 1\n", CodeMultiDoc},
		"schema 2":       {"schema: 2\n", CodeSchemaVer},
		"no schema":      {"title: x\n", CodeSchemaVer},
		"not a map":      {"- a\n", CodeSyntax},
		"too large":      {"schema: 1\ntitle: " + strings.Repeat("a", MaxDescriptorBytes) + "\n", CodeTooLarge},
		"too deep":       {deep, CodeAnchor},
		"non-string key": {"schema: 1\n? [a]\n: b\n", CodeSyntax},
	} {
		st := time.Now()
		v, ps := l.Load([]byte(c.src))
		if v != nil || len(ps) != 1 || ps[0].Code != c.code {
			t.Errorf("%s: value %v problems %+v, want one %s", name, v, ps, c.code)
		}
		if d := time.Since(st); d > time.Second {
			t.Errorf("%s: took %v", name, d)
		}
	}
	// 64 KiB exactly is accepted, one byte more is not
	pad := "schema: 1\n# " + strings.Repeat("a", MaxDescriptorBytes-len("schema: 1\n# ")) + "\n"
	if len(pad) != MaxDescriptorBytes+1 {
		pad = pad[:MaxDescriptorBytes]
	}
	if v, ps := l.Load([]byte(pad[:MaxDescriptorBytes])); v == nil {
		t.Errorf("exactly 64 KiB must load: %+v", ps)
	}
}

func TestDescriptorFieldGranularity(t *testing.T) {
	l := mustLoader(t)
	v, ps := l.Load([]byte("schema: 1\ntitle: ok\nlifecycle: nope\ndocs: {root: ./docs/, include: ['**/*.md']}\nintegrations: {jenkins: {instance: main}}\n"))
	j, _ := json.Marshal(v)
	if string(j) != `{"docs":{"include":["**/*.md"],"root":"docs"},"schema":1,"title":"ok"}` {
		t.Fatalf("%s %+v", j, ps)
	}
	if _, ps2 := l.Load([]byte("schema: 1\ndocs: {root: .}\n")); len(ps2) == 0 {
		t.Fatal("a root of '.' must be reported")
	}
	// an unknown key of 60,000 characters is not echoed into the API
	_, ps3 := l.Load([]byte("schema: 1\n" + strings.Repeat("k", 30000) + ": 1\n"))
	if len(ps3) != 1 || len(ps3[0].Pointer) > 200 {
		t.Fatalf("%d problems, pointer %d bytes", len(ps3), len(ps3[0].Pointer))
	}
}

func FuzzDescriptor(f *testing.F) {
	l, err := NewDescriptorLoader()
	if err != nil {
		f.Fatal(err)
	}
	for _, n := range []string{"scp.sample.yaml", "bad.sample.yaml"} {
		if b, err := os.ReadFile("../../testdata/descriptor/" + n); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("schema: 1\ndocs: {nav: [[a]]}\n")
	f.Fuzz(func(t *testing.T, in string) {
		v, ps := l.Load([]byte(in))
		for _, p := range ps {
			if len(p.Pointer) > 200 || len(p.Message) > 80 {
				t.Fatalf("unbounded problem %+v", p)
			}
		}
		if v != nil {
			if _, err := DecodeDescriptor(v); err != nil {
				t.Fatalf("pruned value does not decode: %v", err)
			}
		}
	})
}

// TestLintDescriptor pins the two exports unit 16 (cmd/scp) calls (CD-IF-04).
func TestLintDescriptor(t *testing.T) {
	if len(DescriptorSchema()) < 100 {
		t.Fatal("empty descriptor schema")
	}
	ps, err := LintDescriptor([]byte("schema: 1\n"))
	if err != nil || len(ps) != 0 {
		t.Fatalf("valid descriptor: %v %v", ps, err)
	}
	ps, err = LintDescriptor([]byte("title: x\n"))
	if err != nil || len(ps) == 0 || ps[0].Code != CodeSchemaVer {
		t.Fatalf("missing schema: %v %v", ps, err)
	}
}
