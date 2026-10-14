package maven_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

func pom(body string) []byte {
	return []byte(`<project><modelVersion>4.0.0</modelVersion><groupId>g</groupId><artifactId>a</artifactId><version>1</version>` + body + `</project>`)
}

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

func TestParserBombs(t *testing.T) {
	bomb := `<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;"><!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">]><project><name>&lol3;</name></project>`
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"entity-bomb", []byte(bomb), "maven.syntax"},
		{"depth-bomb", []byte(strings.Repeat("<a>", 100000)), "limit.depth"},
		{"utf16-bom", []byte("\xff\xfe<\x00p\x00/\x00>\x00"), "encoding.unsupported"},
		{"utf16-declared", []byte(`<?xml version="1.0" encoding="UTF-16"?><project/>`), "encoding.unsupported"},
		{"unknown-charset", []byte(`<?xml version="1.0" encoding="klingon"?><project/>`), "encoding.unsupported"},
		{"invalid-utf8", []byte("<project><name>\xff\xfe</name></project>"), "encoding.unsupported"},
		{"nul", []byte("<project><name>a\x00b</name></project>"), "encoding.unsupported"},
		{"not-a-project", []byte("<settings/>"), "maven.not-a-project"},
		{"truncated", []byte("<project><dependencies><dependency>"), "maven.syntax"},
		{"two-roots", []byte("<project/><project/>"), "maven.syntax"},
		{"size", []byte("<project>" + strings.Repeat(" ", 1<<20) + "</project>"), "limit.size"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, d := runTree(t, map[string][]byte{"pom.xml": c.data}, manifests.Limits{})
			if codes(res)[c.want] == 0 {
				t.Errorf("want %s, got %v", c.want, codes(res))
			}
			if d > 2*time.Second {
				t.Errorf("took %v", d)
			}
			for _, p := range res.Parsed {
				parsertest.CheckParse(t, newReg(t), newReg(t).Match("pom.xml")[0], "pom.xml", c.data)
				_ = p
			}
		})
	}
}

// TestPropertyBomb: exponential property chains and cycles stop at the expansion budget.
func TestPropertyBomb(t *testing.T) {
	var props strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&props, "<p%d>${p%d}${p%d}${p%d}${p%d}</p%d>", i, i+1, i+1, i+1, i+1, i)
	}
	props.WriteString("<p40>xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx</p40><a>${b}</a><b>${a}</b><s>${s}</s>")
	body := `<properties>` + props.String() + `</properties><dependencies><dependency><groupId>x</groupId><artifactId>y</artifactId><version>${p0}</version></dependency>
<dependency><groupId>x</groupId><artifactId>cyc</artifactId><version>${a}</version></dependency>
<dependency><groupId>x</groupId><artifactId>self</artifactId><version>${s}</version></dependency></dependencies>`
	res, d := runTree(t, map[string][]byte{"pom.xml": pom(body)}, manifests.Limits{})
	c := codes(res)
	if c["limit.expand"] == 0 {
		t.Errorf("want limit.expand, got %v", c)
	}
	if c["maven.property-cycle"] == 0 {
		t.Errorf("want maven.property-cycle, got %v", c)
	}
	if d > 2*time.Second {
		t.Errorf("took %v", d)
	}
	for _, dep := range res.Units[0].Dependencies {
		if len(dep.Requirement) > 1<<20 {
			t.Errorf("requirement of %d bytes escaped the budget", len(dep.Requirement))
		}
	}
}

// TestManyBombedPoms: the run-wide budget bounds the total work of a reactor of hostile POMs.
func TestManyBombedPoms(t *testing.T) {
	var props strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&props, "<p%d>${p%d}${p%d}${p%d}${p%d}</p%d>", i, i+1, i+1, i+1, i+1, i)
	}
	props.WriteString("<p30>yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy</p30>")
	tree := map[string][]byte{}
	for i := 0; i < 200; i++ {
		tree[fmt.Sprintf("m%d/pom.xml", i)] = []byte(fmt.Sprintf(`<project><modelVersion>4.0.0</modelVersion><groupId>g</groupId><artifactId>m%d</artifactId><version>1</version><properties>%s</properties><dependencies><dependency><groupId>x</groupId><artifactId>y</artifactId><version>${p0}</version></dependency></dependencies></project>`, i, props.String()))
	}
	_, d := runTree(t, tree, manifests.Limits{})
	if d > 5*time.Second {
		t.Errorf("200 bombed POMs took %v", d)
	}
}

// TestChainCycles: parent cycles, self parents, deep chains and BOM import cycles terminate with a diagnostic.
func TestChainCycles(t *testing.T) {
	mk := func(a, parent string) []byte {
		return []byte(fmt.Sprintf(`<project><modelVersion>4.0.0</modelVersion><parent><groupId>g</groupId><artifactId>%s</artifactId><version>1</version><relativePath>../%s/pom.xml</relativePath></parent><artifactId>%s</artifactId></project>`, parent, parent, a))
	}
	tree := map[string][]byte{"a/pom.xml": mk("a", "b"), "b/pom.xml": mk("b", "a"), "self/pom.xml": mk("self", "self")}
	res, d := runTree(t, tree, manifests.Limits{})
	if codes(res)["maven.parent-cycle"] == 0 {
		t.Errorf("no cycle diagnostic: %v", codes(res))
	}
	if d > time.Second {
		t.Errorf("took %v", d)
	}
	deep := map[string][]byte{}
	for i := 0; i < 100; i++ {
		deep[fmt.Sprintf("d%d/pom.xml", i)] = mk(fmt.Sprintf("d%d", i), fmt.Sprintf("d%d", i+1))
	}
	res, d = runTree(t, deep, manifests.Limits{})
	if codes(res)["maven.parent-cycle"] == 0 || d > 2*time.Second {
		t.Errorf("deep chain: %v in %v", codes(res), d)
	}
	bom := func(a, other string) []byte {
		return []byte(fmt.Sprintf(`<project><modelVersion>4.0.0</modelVersion><groupId>g</groupId><artifactId>%s</artifactId><version>1</version><packaging>pom</packaging><dependencyManagement><dependencies><dependency><groupId>g</groupId><artifactId>%s</artifactId><version>1</version><type>pom</type><scope>import</scope></dependency></dependencies></dependencyManagement></project>`, a, other))
	}
	res, _ = runTree(t, map[string][]byte{"x/pom.xml": bom("x", "y"), "y/pom.xml": bom("y", "x")}, manifests.Limits{})
	if codes(res)["maven.bom-cycle"] == 0 {
		t.Errorf("no BOM cycle diagnostic: %v", codes(res))
	}
}

// TestPathEscapes: relativePath and module entries never leave the repository.
func TestPathEscapes(t *testing.T) {
	tree := map[string][]byte{"sub/pom.xml": []byte(`<project><modelVersion>4.0.0</modelVersion><parent><groupId>g</groupId><artifactId>p</artifactId><version>1</version><relativePath>../../../etc/pom.xml</relativePath></parent><artifactId>s</artifactId>
<modules><module>../../..</module><module>/abs</module><module>a\b</module></modules></project>`)}
	res, _ := runTree(t, tree, manifests.Limits{})
	c := codes(res)
	if c["maven.relative-path-escape"] == 0 || c["maven.module-missing"] != 1 || c["maven.module-invalid"] < 2 {
		t.Errorf("diagnostics: %v", c)
	}
	for _, m := range res.Units[0].Members {
		t.Errorf("unexpected member %q", m)
	}
}

// TestOrderIndependent: assembling the same parsed files in any order gives identical units.
func TestOrderIndependent(t *testing.T) {
	tree, err := parsertest.LoadTree("testdata/reactor-inheritance")
	if err != nil {
		t.Fatal(err)
	}
	reg := newReg(t)
	base := manifests.RunTree(context.Background(), reg, parsertest.TreeConfig(), tree)
	asm, _ := reg.Assembler(manifests.Maven)
	paths := make(manifests.PathSet, 0, len(tree))
	for p := range tree {
		paths = append(paths, p)
	}
	var want []byte
	for seed := int64(0); seed < 6; seed++ {
		ps := append([]manifests.Parsed(nil), base.Parsed...)
		rand.New(rand.NewSource(seed)).Shuffle(len(ps), func(i, j int) { ps[i], ps[j] = ps[j], ps[i] })
		us, err := asm.Assemble(context.Background(), paths, ps)
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(us, func(i, j int) bool { return us[i].Dir < us[j].Dir })
		got, _ := json.Marshal(us)
		if want == nil {
			want = got
		} else if string(got) != string(want) {
			t.Fatalf("seed %d: assembly depends on input order", seed)
		}
	}
}

// TestSecretsInPomSource: nothing from a POM's repositories/credentials sections is emitted.
func TestNoCredentialsEmitted(t *testing.T) {
	p := pom(`<repositories><repository><id>r</id><url>https://user:pw123@repo.example.com/m2</url></repository></repositories>
<distributionManagement><repository><url>https://tok:s3cret@x/y</url></repository></distributionManagement>
<scm><url>https://u:p4ss@git/x.git</url></scm>`)
	res, _ := runTree(t, map[string][]byte{"pom.xml": p}, manifests.Limits{})
	b, _ := json.Marshal(res)
	for _, s := range []string{"pw123", "s3cret", "p4ss"} {
		if strings.Contains(string(b), s) {
			t.Errorf("output contains %q", s)
		}
	}
}
