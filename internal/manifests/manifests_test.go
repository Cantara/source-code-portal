package manifests

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// repo is a fake repository: files by path, blob SHA = the content itself, prefixed.
type repo struct {
	files map[string]string
	err   error
	blobs int
}

func (r *repo) Tree(context.Context, string, string, string, bool) ([]github.TreeEntry, error) {
	if r.err != nil {
		return nil, r.err
	}
	var out []github.TreeEntry
	for p, c := range r.files {
		out = append(out, github.TreeEntry{Path: p, Type: "blob", SHA: "blob:" + c, Size: int64(len(c))})
	}
	return out, nil
}

func (r *repo) Blob(_ context.Context, _, _, sha string) ([]byte, error) {
	r.blobs++
	return []byte(strings.TrimPrefix(sha, "blob:")), nil
}

func run(t *testing.T, m *Manifests, prev store.Item) (sync.Result, payload) {
	t.Helper()
	res, err := m.Spec().Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x", Prev: prev})
	if err != nil {
		t.Fatal(err)
	}
	return res, decode(res.Payload)
}

func paths(p payload) string {
	var out []string
	for _, r := range p.Records {
		out = append(out, r.Manifest.Path)
	}
	return strings.Join(out, ",")
}

func TestJob(t *testing.T) {
	r := &repo{files: map[string]string{
		"go.mod":                          "module github.com/acme/x\n\ngo 1.27\n",
		"web/package.json":                `{"name":"x-web","dependencies":{"react":"^19"}}`,
		"web/node_modules/a/package.json": `{"name":"a"}`,
		"vendor/b/go.mod":                 "module b\n",
		".github/pom.xml":                 "<project/>",
		"a/b/c/d/e/go.mod":                "module deep\n",
		"a/b/c/d/go.mod":                  "module shallow-enough\n",
		"src/testdata/pom.xml":            "<project/>",
		"service/pom.xml":                 "<project><groupId>g</groupId><artifactId>a</artifactId></project>",
	}}
	m := New(map[string]GitHub{"gh": r})
	res, p := run(t, m, store.Item{})
	if want := "go.mod,service/pom.xml,web/package.json,a/b/c/d/go.mod"; res.State != snapshot.StateOK || paths(p) != want || r.blobs != 4 {
		t.Fatalf("first run: %s, %s, %d blobs; want %s", res.State, paths(p), r.blobs, want)
	}
	prev := store.Item{State: res.State, ContentSHA: res.ContentSHA, Payload: res.Payload}
	if again, _ := run(t, m, prev); !again.Unchanged || r.blobs != 4 {
		t.Errorf("unchanged repository: Unchanged %v, %d blobs", again.Unchanged, r.blobs)
	}
	r.files["go.mod"] = "module github.com/acme/x\n\ngo 1.28\n"
	if again, p := run(t, m, prev); again.Unchanged || r.blobs != 5 || p.Records[0].Manifest.Toolchain[0].Version != "1.28" {
		t.Errorf("changed go.mod: %d blobs, %+v", r.blobs, p.Records[0].Manifest)
	}

	r.files["big/package.json"] = strings.Repeat(" ", maxFileBytes+1)
	if _, p := run(t, m, prev); r.blobs != 6 || p.Records[1].Manifest.Problems[0].Code != "too-large" {
		t.Errorf("a large file is fetched or not reported: %d blobs, %+v", r.blobs, p.Records[1].Manifest)
	}

	r.err = &github.Error{Kind: github.NotFound, Code: "empty"}
	if res, p := run(t, m, prev); res.State != snapshot.StateNoContent || len(p.Records) != 0 {
		t.Errorf("empty repository: %s, %s", res.State, paths(p))
	}
}

func TestGoMod(t *testing.T) {
	got := parseGoMod([]byte(`module github.com/acme/x

go 1.27.2

toolchain go1.27.3

require (
	github.com/spf13/cobra v1.10.1
	golang.org/x/mod v0.42.0 // indirect
)

tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
`))
	want := snapshot.Manifest{Ecosystem: "go", Name: "github.com/acme/x", Language: "Go", BuildTool: "Go modules",
		Toolchain: []snapshot.Toolchain{{Name: "go", Version: "1.27.2"}, {Name: "go", Version: "1.27.3"}},
		Dependencies: []snapshot.Dependency{{Name: "github.com/spf13/cobra", Version: "v1.10.1", Scope: "runtime"},
			{Name: "golang.org/x/mod", Version: "v0.42.0", Scope: "indirect"},
			{Name: "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen", Scope: "tool"}}}
	if d := cmp.Diff(want, got); d != "" {
		t.Error(d)
	}
	bad := parseGoMod([]byte("module x\nrequire (\n\tbroken\n"))
	if len(bad.Problems) != 1 || bad.Problems[0].Code != "syntax" || !strings.HasPrefix(bad.Problems[0].Pointer, "/line/") {
		t.Errorf("syntax error: %+v", bad.Problems)
	}
	if bin := parseGoMod([]byte("module \xff\n")); len(bin.Problems) != 1 || bin.Problems[0].Code != "encoding" {
		t.Errorf("invalid UTF-8: %+v", bin.Problems)
	}
}

func TestPackageJSON(t *testing.T) {
	beside := map[string]bool{"pnpm-lock.yaml": true}
	got := parsePackageJSON([]byte("\xef\xbb\xbf"+`{"name":"@acme/web","version":"2.0.0","engines":{"node":">=24"},
		"dependencies":{"react":"^19.3.0","zod":4},"devDependencies":{"typescript":"~6.0.3","`+"\u202e"+`bidi":"1"},
		"peerDependencies":{"react-dom":"*"},"optionalDependencies":{"fsevents":"^2"},"scripts":{"x":"rm -rf /"}}`),
		func(n string) bool { return beside[n] })
	want := snapshot.Manifest{Ecosystem: "npm", Name: "@acme/web", Version: "2.0.0", Language: "TypeScript", BuildTool: "pnpm",
		Toolchain: []snapshot.Toolchain{{Name: "node", Version: ">=24"}},
		Dependencies: []snapshot.Dependency{{Name: "react", Version: "^19.3.0", Scope: "runtime"}, {Name: "react-dom", Version: "*", Scope: "peer"},
			{Name: "fsevents", Version: "^2", Scope: "optional"}, {Name: "bidi", Version: "1", Scope: "dev"},
			{Name: "typescript", Version: "~6.0.3", Scope: "dev"}}}
	sortDependencies(got.Dependencies)
	if d := cmp.Diff(want, got); d != "" {
		t.Error(d)
	}
	if m := parsePackageJSON([]byte(`{"name":"x","packageManager":"yarn@4.1.0"}`), func(string) bool { return true }); m.BuildTool != "yarn" ||
		m.Language != "TypeScript" {
		t.Errorf("packageManager and tsconfig.json: %+v", m)
	}
	for _, in := range []string{`[`, `[]`, `{"name":1}`, `{"dependencies":[]}`} {
		if m := parsePackageJSON([]byte(in), func(string) bool { return false }); len(m.Problems) != 1 {
			t.Errorf("%s: problems %+v", in, m.Problems)
		}
	}
}

func TestPOM(t *testing.T) {
	got := parsePOM([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <parent><groupId>no.cantara</groupId><artifactId>cantara-parent</artifactId><version>7</version></parent>
  <artifactId>whydah-sso</artifactId>
  <version>${revision}</version>
  <properties>
    <revision>3.1.0-SNAPSHOT</revision>
    <maven.compiler.release>21</maven.compiler.release>
    <jackson.version>2.20.0</jackson.version>
  </properties>
  <dependencyManagement><dependencies>
    <dependency><groupId>com.fasterxml.jackson</groupId><artifactId>jackson-bom</artifactId><version>${jackson.version}</version>
      <type>pom</type><scope>import</scope></dependency>
    <dependency><groupId>org.slf4j</groupId><artifactId>slf4j-api</artifactId><version>2.0.17</version></dependency>
  </dependencies></dependencyManagement>
  <dependencies>
    <dependency><groupId>${project.groupId}</groupId><artifactId>whydah-api</artifactId><version>${project.version}</version></dependency>
    <dependency><groupId>org.slf4j</groupId><artifactId>slf4j-api</artifactId></dependency>
    <dependency><groupId>jakarta.servlet</groupId><artifactId>jakarta.servlet-api</artifactId><scope>provided</scope></dependency>
    <dependency><groupId>org.junit.jupiter</groupId><artifactId>junit-jupiter</artifactId><version>${junit}</version><scope>test</scope></dependency>
  </dependencies>
  <profiles><profile><dependencies><dependency><groupId>p</groupId><artifactId>p</artifactId></dependency></dependencies></profile></profiles>
</project>`))
	want := snapshot.Manifest{Ecosystem: "maven", Name: "no.cantara:whydah-sso", Version: "3.1.0-SNAPSHOT", Language: "Java", BuildTool: "Maven",
		Toolchain: []snapshot.Toolchain{{Name: "java", Version: "21"}},
		Dependencies: []snapshot.Dependency{{Name: "no.cantara:cantara-parent", Version: "7", Scope: "parent"},
			{Name: "com.fasterxml.jackson:jackson-bom", Version: "2.20.0", Scope: "import"},
			{Name: "no.cantara:whydah-api", Version: "3.1.0-SNAPSHOT", Scope: "runtime"}, {Name: "org.slf4j:slf4j-api", Scope: "runtime"},
			{Name: "jakarta.servlet:jakarta.servlet-api", Scope: "provided"},
			{Name: "org.junit.jupiter:junit-jupiter", Version: "${junit}", Scope: "test"}}}
	sortDependencies(got.Dependencies)
	if d := cmp.Diff(want, got); d != "" {
		t.Error(d)
	}

	latin := parsePOM([]byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><project><groupId>g</groupId><artifactId>caf\xe9</artifactId></project>"))
	if latin.Name != "g:café" || len(latin.Problems) != 0 {
		t.Errorf("ISO-8859-1: %+v", latin)
	}
	if k := parsePOM([]byte(`<project><groupId>org.jetbrains.kotlin</groupId><artifactId>k</artifactId><dependencies><dependency>
		<groupId>org.jetbrains.kotlin</groupId><artifactId>kotlin-stdlib-jdk8</artifactId></dependency></dependencies></project>`)); k.Language != "Kotlin" {
		t.Errorf("Kotlin: %+v", k)
	}
}

func TestPOMHostile(t *testing.T) {
	laughs := `<?xml version="1.0"?><!DOCTYPE project [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;&a;&a;">]>` +
		`<project><artifactId>&b;</artifactId></project>`
	for name, tc := range map[string]struct{ in, code string }{
		"entities":   {laughs, "syntax"},
		"external":   {`<!DOCTYPE project SYSTEM "file:///etc/passwd"><project><artifactId>&xxe;</artifactId></project>`, "syntax"},
		"nesting":    {"<project>" + strings.Repeat("<a>", 100) + strings.Repeat("</a>", 100) + "</project>", "too-complex"},
		"tokens":     {"<project>" + strings.Repeat("<a/>", maxXMLTokens) + "</project>", "too-complex"},
		"encoding":   {`<?xml version="1.0" encoding="UTF-16"?><project/>`, "encoding"},
		"other root": {`<html/>`, "syntax"},
		"empty":      {``, "syntax"},
		"unclosed":   {`<project><artifactId>x`, "syntax"},
	} {
		m := parsePOM([]byte(tc.in))
		if len(m.Problems) != 1 || m.Problems[0].Code != tc.code || m.Name != "" {
			t.Errorf("%s: %+v", name, m)
		}
	}
	long := parsePOM([]byte("<project><groupId>g</groupId><artifactId>" + strings.Repeat("x", maxXMLText+10) + "</artifactId></project>"))
	if len(long.Problems) != 1 || long.Problems[0].Code != "too-long" || len([]rune(long.Name)) > maxName {
		t.Errorf("long value: %d runes, %+v", len([]rune(long.Name)), long.Problems)
	}
}

func FuzzParsers(f *testing.F) {
	for _, s := range []string{"module x\ngo 1.27\nrequire a v1\n", `{"name":"x","dependencies":{"a":"1"}}`,
		"<project><groupId>g</groupId><artifactId>a</artifactId><properties><v>${v}</v></properties></project>"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, m := range []snapshot.Manifest{parseGoMod(b), parsePackageJSON(b, func(string) bool { return false }), parsePOM(b)} {
			if len(m.Dependencies) > maxDependencies || len([]rune(m.Name)) > maxName {
				t.Fatalf("bounds: %d dependencies, name %q", len(m.Dependencies), m.Name)
			}
		}
	})
}

func TestAPI(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	entry := func(id int64, name string) *snapshot.RepoEntry {
		return &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "Acme", Name: name}}
	}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{entry(1, "lib"), entry(2, "app")}, map[snapshot.RepoKey]snapshot.RepoKey{"acme/old": "acme/lib"},
			snapshot.SourceStatus{ID: "gh"})
		b.UpdateRepo("acme/lib", func(e *snapshot.RepoEntry) {
			e.Manifests = []snapshot.Manifest{{Path: "go.mod", Ecosystem: "go", Name: "github.com/acme/lib", Language: "Go", BuildTool: "Go modules",
				Toolchain: []snapshot.Toolchain{{Name: "go", Version: "1.27"}}}}
		})
		b.UpdateRepo("acme/app", func(e *snapshot.RepoEntry) {
			e.Manifests = []snapshot.Manifest{
				{Path: "go.mod", Ecosystem: "go", Name: "github.com/acme/app", Language: "Go", BuildTool: "Go modules",
					Toolchain:    []snapshot.Toolchain{{Name: "go", Version: "1.27"}},
					Dependencies: []snapshot.Dependency{{Name: "github.com/acme/lib", Version: "v1.0.0", Scope: "runtime"}, {Name: "x", Scope: "runtime"}}},
				{Path: "web/package.json", Ecosystem: "npm", Language: "TypeScript", BuildTool: "pnpm",
					Toolchain: []snapshot.Toolchain{{Name: "node", Version: ">=24"}}, Problems: []snapshot.Problem{{Code: "invalid", Message: "m"}}}}
		})
	})
	p.Flush()
	defer p.Close()
	a := API{Current: p.Current}
	res, err := a.GetManifests(t.Context(), oapi.GetManifestsRequestObject{Owner: "acme", Name: "APP"})
	if err != nil {
		t.Fatal(err)
	}
	got := res.(oapi.GetManifests200JSONResponse).Body
	if want := (oapi.Technology{Languages: []string{"Go", "TypeScript"}, BuildTools: []string{"Go modules", "pnpm"},
		Toolchains: []oapi.ToolchainVersions{{Name: "go", Versions: []string{"1.27"}}, {Name: "node", Versions: []string{">=24"}}}}); !cmp.Equal(want, got.Technology) {
		t.Errorf("technology: %s", cmp.Diff(want, got.Technology))
	}
	if d := got.Manifests[0].Dependencies; d[0].Repos[0] != "acme/lib" || len(d[1].Repos) != 0 {
		t.Errorf("dependency links: %+v", d)
	}
	if want := []oapi.RepoLink{{Address: "acme/lib", Owner: "Acme", Name: "lib", Via: []string{"github.com/acme/lib"}}}; !cmp.Equal(want, got.Uses) ||
		len(got.UsedBy) != 0 || got.Manifests[1].Problems[0].Code != "invalid" {
		t.Errorf("uses %+v, usedBy %+v", got.Uses, got.UsedBy)
	}
	res, _ = a.GetManifests(t.Context(), oapi.GetManifestsRequestObject{Owner: "acme", Name: "lib"})
	if used := res.(oapi.GetManifests200JSONResponse).Body.UsedBy; len(used) != 1 || used[0].Address != "acme/app" || used[0].Via[0] != "github.com/acme/lib" {
		t.Errorf("usedBy = %+v", used)
	}
	if res, _ := a.GetManifests(t.Context(), oapi.GetManifestsRequestObject{Owner: "acme", Name: "old"}); res.(oapi.GetManifests308Response).Headers.Location !=
		"/api/v1/repos/acme/lib/manifests" {
		t.Errorf("old address: %+v", res)
	}
	if res, _ := a.GetManifests(t.Context(), oapi.GetManifestsRequestObject{Owner: "acme", Name: "nope"}); func() bool {
		_, ok := res.(oapi.GetManifests404ApplicationProblemPlusJSONResponse)
		return !ok
	}() {
		t.Errorf("unknown repository: %T", res)
	}
}
