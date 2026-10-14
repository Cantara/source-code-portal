// Package crosscheck holds the tests that in the real repository live in cmd/scp (they need both the YAML library and the
// catalog packages, which the dependency matrix keeps apart): strict decoding of catalog YAML, agreement of the hand-written
// JSON Schema with the decoder, and the lint output of the example catalogue against the example repository list.
package crosscheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/scm"
)

type doc struct {
	Catalog catalog.Config `yaml:"catalog"`
}

func decode(t *testing.T, y []byte) (catalog.Config, error) {
	t.Helper()
	d := doc{Catalog: catalog.DefaultConfig()}
	dec := yaml.NewDecoder(bytes.NewReader(y))
	dec.KnownFields(true)
	err := dec.Decode(&d)
	return d.Catalog, err
}

func TestStrictDecodeRejects(t *testing.T) {
	bad := map[string]string{
		"unknown group key": "catalog:\n  groups:\n    - {id: a, display_name: A, default_repo: a, match: [{exact: a}], colour: red}\n",
		"two kinds":         "catalog:\n  groups:\n    - {id: a, display_name: A, default_repo: a, match: [{exact: a, prefix: b}]}\n",
		"no kind":           "catalog:\n  groups:\n    - {id: a, display_name: A, default_repo: a, match: [{}]}\n",
		"unknown kind":      "catalog:\n  groups:\n    - {id: a, display_name: A, default_repo: a, match: [{suffix: a}]}\n",
		"catch_all false":   "catalog:\n  groups:\n    - {id: a, display_name: A, default_repo: a, match: [{catch_all: false}]}\n",
		"duplicate key":     "catalog:\n  groups:\n    - {id: a, id: b, display_name: A, default_repo: a, match: [{exact: a}]}\n",
		"scalar matcher":    "catalog:\n  groups:\n    - {id: a, display_name: A, default_repo: a, match: [Widget*]}\n",
		"bad duration":      "catalog:\n  heads: {interval: soon}\n",
	}
	for name, y := range bad {
		if _, err := decode(t, []byte(y)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
	good := "catalog:\n  heads: {batch_size: 20}\n  groups:\n    - {id: a, display_name: A, default_repo: a, match: [{exact: a}, {catch_all: true}, {topic: iam}]}\n"
	c, err := decode(t, []byte(good))
	if err != nil || len(c.Groups[0].Match) != 3 || c.Groups[0].Match[1].Kind != catalog.KindCatchAll || c.Heads.Interval == 0 {
		t.Fatalf("good document: %v %+v", err, c)
	}
}

type repoInfo struct {
	Name           string `json:"name"`
	DefaultBranch  string `json:"default_branch"`
	Archived, Fork bool
	Topics         []string `json:"topics"`
}

func loadRepos(t *testing.T) []scm.Repository {
	t.Helper()
	b, err := os.ReadFile("../../../fixtures/example-repos.json")
	if err != nil {
		t.Fatal(err)
	}
	var infos []repoInfo
	if err := json.Unmarshal(b, &infos); err != nil {
		t.Fatal(err)
	}
	var repos []scm.Repository
	for i, r := range infos {
		repos = append(repos, scm.Repository{Ref: scm.RepoRef{Source: "github", ID: scm.RepoID(i + 1), Owner: "acme", Name: r.Name}, Slug: scm.Slug(strings.ToLower(r.Name)),
			DefaultBranch: r.DefaultBranch, Topics: r.Topics, Archived: r.Archived, Fork: r.Fork, Visibility: scm.VisPublic})
	}
	return repos
}

func exampleCatalogs(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob("../../../fixtures/*.catalog.yaml")
	if len(files) == 0 {
		t.Fatal("no example catalogue in fixtures/")
	}
	return files
}

// The example catalogue decodes strictly, compiles, and the REAL kernel membership has the expected member counts.
func TestExampleCatalogBuildsMembership(t *testing.T) {
	repos := loadRepos(t)
	in, err := os.ReadFile("../../../fixtures/example.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := decode(t, in)
	if err != nil {
		t.Fatalf("strict decode: %v", err)
	}
	c, err := catalog.Compile(cfg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mr, err := c.BuildMembership(repos)
	if err != nil {
		t.Fatalf("the real kernel rejects the membership: %v", err)
	}
	want := map[scm.GroupID]int{"atlas": 4, "billing": 2, "ledger": 2, "search": 0, "everything": len(repos)}
	for g, n := range want {
		if got := len(mr.Membership.ReposOf(g)); got != n {
			t.Errorf("group %s: %d members, want %d", g, got, n)
		}
	}
}

// Lint of the example catalogue against the example repository list equals the committed output (out/example.lint.txt).
func TestExampleCatalogLintGolden(t *testing.T) {
	repos := loadRepos(t)
	in, _ := os.ReadFile("../../../fixtures/example.catalog.yaml")
	cfg, err := decode(t, in)
	if err != nil {
		t.Fatal(err)
	}
	fs := catalog.Lint(cfg, catalog.ValidateContext{Sources: []scm.SourceID{"github"}}, nil, repos)
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.String() + "\n")
	}
	b.WriteString(fmt.Sprintf("%d findings, errors=%v\n", len(fs), catalog.HasErrors(fs)))
	want, err := os.ReadFile("../../../out/example.lint.txt")
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("lint output differs from out/example.lint.txt:\n%s", b.String())
	}
}

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	var doc any
	if err := json.Unmarshal(catalog.Schema, &doc); err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("catalog.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("catalog.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func toJSONValue(t *testing.T, y []byte) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal(y, &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	var out any
	json.Unmarshal(b, &out)
	return out
}

// The hand-written schema accepts every example catalogue, and rejects the documents the strict decoder rejects.
func TestSchemaAgreesWithDecoder(t *testing.T) {
	s := compileSchema(t)
	for _, f := range exampleCatalogs(t) {
		in, _ := os.ReadFile(f)
		if _, err := decode(t, in); err != nil {
			t.Errorf("%s: strict decode: %v", f, err)
		}
		v := toJSONValue(t, in).(map[string]any)["catalog"]
		if err := s.Validate(v); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	bad := []string{
		"{groups: [{id: a, display_name: A, default_repo: a, match: [{exact: a, prefix: b}]}]}",
		"{groups: [{id: A, display_name: A, default_repo: a, match: [{exact: a}]}]}",
		"{groups: [{id: a, display_name: A, default_repo: a, match: []}]}",
		"{groups: [{id: a, display_name: A, default_repo: a, match: [{catch_all: false}]}]}",
		"{groups: [{id: a, display_name: A, default_repo: a, match: [{glob: 'a[b]'}]}]}",
		"{groups: [{id: a, display_name: A, match: [{exact: a}]}]}",
		"{heads: {interval: soon}}",
		"{ungrouped: maybe}",
	}
	for _, y := range bad {
		if err := s.Validate(toJSONValue(t, []byte(y))); err == nil {
			t.Errorf("schema accepted %s", y)
		}
	}
	good := "{ungrouped: ignore, slug_prefix_by_source: {acme: 'a-', 'acme/other': 'o-'}, heads: {batch_size: 20, interval: 2m}, groups: [{id: a, display_name: A, default_repo: a, sources: [acme], match: [{regexp: '(?i)x.*'}, {topic: iam}, {catch_all: true}], ref: release, aliases: ['Old A'], integrations: {jenkins: {job_prefix: 'J-'}}}]}"
	if err := s.Validate(toJSONValue(t, []byte(good))); err != nil {
		t.Errorf("schema rejected the good document: %v", err)
	}
	// and the Go strict decoder agrees on the good one
	if _, err := decode(t, []byte("catalog: "+good)); err != nil {
		t.Errorf("decoder rejected the good document: %v", err)
	}
}
