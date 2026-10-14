package docs

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/docs/internal/render"
	"github.com/cantara/source-code-portal/internal/docs/schema"
)

// TestFrontMatterSchemaMatchesCode is the differential test of verification V-05: the served frontmatter-v1.json and the
// in-code checks of render.ParseFrontMatter had drifted (title 200 vs 120, order +-100000 vs +-10000, description minimum,
// tag uniqueness). Every portal key value of the table must be accepted or refused identically by both.
func TestFrontMatterSchemaMatchesCode(t *testing.T) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.FrontMatterV1()))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource("https://scp.invalid/fm.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("https://scp.invalid/fm.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"title: ok", "title: ''", "title: " + strings.Repeat("a", 120), "title: " + strings.Repeat("a", 121), "title: 5", "title: [a]",
		"description: ok", "description: ''", "description: " + strings.Repeat("b", 300), "description: " + strings.Repeat("b", 301),
		"order: 5", "order: -100000", "order: 100001", "order: 100000", "order: 1.5", "order: x",
		"tags: [a, b]", "tags: [a, a]", "tags: [Has Space]", "tags: []", "tags: x", "tags: [" + strings.Repeat("a, ", 20) + "z]",
		"hidden: true", "hidden: false", "hidden: 1", "hidden: maybe",
	}
	for _, y := range cases {
		var v any
		if err := yaml.Unmarshal([]byte(y), &v); err != nil {
			t.Fatal(err)
		}
		js, _ := json.Marshal(v)
		inst, _ := jsonschema.UnmarshalJSON(bytes.NewReader(js))
		schemaOK := sch.Validate(inst) == nil
		m, probs := render.ParseFrontMatter([]byte(y))
		codeOK := len(probs) == 0 && len(m) == 1
		if schemaOK != codeOK {
			t.Errorf("%.60q: schema accepts=%v, code accepts=%v (%v)", y, schemaOK, codeOK, probs)
		}
	}
}
