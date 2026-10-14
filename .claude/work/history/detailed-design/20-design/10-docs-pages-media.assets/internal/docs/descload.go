package docs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/docs/schema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// MaxDescriptorBytes bounds the descriptor file.
const MaxDescriptorBytes = 64 << 10

// DescriptorProblem is one pruned or rejected piece of a descriptor.
type DescriptorProblem struct {
	Pointer string // JSON pointer into the descriptor, "" = whole document
	Code    string // stable code, see Code* constants
	Message string // bounded, no instance data beyond 80 bytes
}

// Severity is "error" when the descriptor as a whole was unusable and "warning" when only a field was dropped
// (the API member `severity`).
func (p DescriptorProblem) Severity() string {
	switch p.Code {
	case CodeSyntax, CodeTooLarge, CodeMultiDoc, CodeAnchor, CodeSchemaVer:
		return "error"
	}
	return "warning"
}

const (
	CodeSyntax    = "descriptor.syntax"
	CodeTooLarge  = "descriptor.too_large"
	CodeMultiDoc  = "descriptor.multi_document"
	CodeAnchor    = "descriptor.anchor_or_alias"
	CodeSchemaVer = "descriptor.schema_version"
	CodeInvalid   = "descriptor.invalid_field"
	CodeUnknown   = "descriptor.unknown_field"
	CodePath      = "descriptor.bad_path"
)

// DescriptorLoader holds the compiled schema. Safe for concurrent use.
type DescriptorLoader struct{ root *jsonschema.Schema }

// NewLoader compiles the embedded schema. Called once from docs.New.
func NewDescriptorLoader() (*DescriptorLoader, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.ScpV1()))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	const loc = "https://scp.invalid/scp-v1.json"
	if err := c.AddResource(loc, doc); err != nil {
		return nil, err
	}
	s, err := c.Compile(loc)
	if err != nil {
		return nil, err
	}
	return &DescriptorLoader{root: s}, nil
}

// Load parses src. It returns the pruned value (JSON-shaped, ready to
// json.Unmarshal into a typed struct) and the problems. A nil value means the
// descriptor as a whole is unusable (syntax, size, schema version).
func (l *DescriptorLoader) Load(src []byte) (map[string]any, []DescriptorProblem) {
	if len(src) > MaxDescriptorBytes {
		return nil, []DescriptorProblem{{"", CodeTooLarge, "descriptor exceeds 64 KiB"}}
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var n yaml.Node
	if err := dec.Decode(&n); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, []DescriptorProblem{{"", CodeSyntax, "empty descriptor"}}
		}
		return nil, []DescriptorProblem{{"", CodeSyntax, trunc(err.Error())}}
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, []DescriptorProblem{{"", CodeMultiDoc, "more than one YAML document"}}
	}
	if bad := walkNode(&n, 0); bad != "" {
		return nil, []DescriptorProblem{{"", CodeAnchor, bad}}
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, []DescriptorProblem{{"", CodeSyntax, trunc(err.Error())}}
	}
	js, err := json.Marshal(v) // fails on non-string keys
	if err != nil {
		return nil, []DescriptorProblem{{"", CodeSyntax, "non-string mapping key"}}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(js))
	if err != nil {
		return nil, []DescriptorProblem{{"", CodeSyntax, trunc(err.Error())}}
	}
	obj, ok := inst.(map[string]any)
	if !ok {
		return nil, []DescriptorProblem{{"", CodeSyntax, "descriptor must be a mapping"}}
	}
	if sv, ok := obj["schema"]; !ok || !isOne(sv) {
		return nil, []DescriptorProblem{{"/schema", CodeSchemaVer, "schema must be 1"}}
	}
	var ps []DescriptorProblem
	out, _ := l.prune(obj, l.root, "", &ps).(map[string]any)
	if out == nil { // cannot happen while "schema" is const 1, but a schema edit must not turn into a panic
		return nil, append(ps, DescriptorProblem{"", CodeSyntax, "nothing valid in the descriptor"})
	}
	// Path-safety is stricter than the schema: no '..' segments.
	pathCheck(out, "", &ps)
	for _, k := range []string{"docs", "integrations"} {
		if m, ok := out[k].(map[string]any); ok && len(m) == 0 {
			delete(out, k)
		}
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Pointer < ps[j].Pointer })
	return out, ps
}

func isOne(v any) bool {
	switch x := v.(type) {
	case json.Number:
		return x.String() == "1"
	case float64:
		return x == 1
	}
	return false
}

// prune keeps the largest valid sub-structure. Order of attempts:
//  1. whole value valid -> keep.
//  2. object with declared properties -> prune each declared property,
//     drop unknown ones; then re-validate the object (cross-field rules such
//     as required); if it still fails, drop it.
//  3. array with an items schema -> prune each item, drop the failing ones,
//     truncate to maxItems; then re-validate (uniqueItems); drop if invalid.
//  4. otherwise drop.
func (l *DescriptorLoader) prune(v any, s *jsonschema.Schema, ptr string, ps *[]DescriptorProblem) any {
	if s.Validate(v) == nil {
		return v
	}
	d := deref(s)
	before := len(*ps)
	switch x := v.(type) {
	case map[string]any:
		if d.Properties == nil {
			break
		}
		out := map[string]any{}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sub, ok := d.Properties[k]
			p := trunc200(ptr + "/" + esc(k))
			if !ok {
				*ps = append(*ps, DescriptorProblem{p, CodeUnknown, "unknown field dropped"})
				continue
			}
			if r := l.prune(x[k], sub, p, ps); r != nil {
				out[k] = r
			}
		}
		if len(out) == 0 && len(x) > 0 && len(*ps) > before {
			return nil // children already reported; nothing left
		}
		if s.Validate(out) == nil {
			return out
		}
	case []any:
		it := d.Items2020
		if it == nil {
			break
		}
		var out []any
		for i, e := range x {
			if r := l.prune(e, it, fmt.Sprintf("%s/%d", ptr, i), ps); r != nil {
				out = append(out, r)
			}
		}
		if d.MaxItems != nil && len(out) > *d.MaxItems {
			*ps = append(*ps, DescriptorProblem{ptr, CodeInvalid, "array truncated to maxItems"})
			out = out[:*d.MaxItems]
		}
		if len(out) == 0 && len(*ps) > before {
			return nil
		}
		if s.Validate(any(out)) == nil && len(out) > 0 {
			return out
		}
	}
	*ps = append(*ps, DescriptorProblem{ptr, CodeInvalid, trunc(firstCause(s.Validate(v)))})
	return nil
}

func deref(s *jsonschema.Schema) *jsonschema.Schema {
	for s.Ref != nil && s.Properties == nil && s.Items2020 == nil {
		s = s.Ref
	}
	return s
}

func firstCause(err error) string {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return "invalid"
	}
	for len(ve.Causes) > 0 {
		ve = ve.Causes[0]
	}
	kp := ve.ErrorKind.KeywordPath()
	if len(kp) == 0 {
		return "invalid"
	}
	return "fails " + kp[len(kp)-1]
}

func pathCheck(m map[string]any, ptr string, ps *[]DescriptorProblem) {
	docs, _ := m["docs"].(map[string]any)
	if docs == nil {
		return
	}
	if r, ok := docs["root"].(string); ok {
		switch {
		case hasDotDot(r):
			delete(docs, "root")
			*ps = append(*ps, DescriptorProblem{"/docs/root", CodePath, "path contains '..'"})
		default:
			// "./docs", "docs//x" and "docs/" match the schema pattern but would match no tree entry: normalise them (V-27)
			if c := path.Clean(strings.Trim(r, "/")); c == "." || c == "" {
				delete(docs, "root")
				*ps = append(*ps, DescriptorProblem{"/docs/root", CodePath, "root is the repository root; use the default"})
			} else {
				docs["root"] = c
			}
		}
	}
	if nav, ok := docs["nav"].([]any); ok {
		docs["nav"] = pruneNav(nav, "/docs/nav", ps)
		if len(docs["nav"].([]any)) == 0 {
			delete(docs, "nav")
		}
	}
}

func pruneNav(nav []any, ptr string, ps *[]DescriptorProblem) []any {
	out := nav[:0:0]
	for i, e := range nav {
		p := fmt.Sprintf("%s/%d", ptr, i)
		switch x := e.(type) {
		case string:
			if hasDotDot(x) {
				*ps = append(*ps, DescriptorProblem{p, CodePath, "path contains '..'"})
				continue
			}
		case map[string]any:
			if s, ok := x["path"].(string); ok && hasDotDot(s) {
				*ps = append(*ps, DescriptorProblem{p + "/path", CodePath, "path contains '..'"})
				continue
			}
			if items, ok := x["items"].([]any); ok {
				x["items"] = pruneNav(items, p+"/items", ps)
			}
		}
		out = append(out, e)
	}
	return out
}

func hasDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// walkNode rejects anchors, aliases and merge keys, and bounds depth.
func walkNode(n *yaml.Node, depth int) string {
	if depth > 32 {
		return "nesting too deep"
	}
	if n.Anchor != "" || n.Kind == yaml.AliasNode || n.Tag == "!!merge" {
		return "anchors, aliases and merge keys are not allowed"
	}
	for _, c := range n.Content {
		if s := walkNode(c, depth+1); s != "" {
			return s
		}
	}
	return ""
}

func esc(s string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(s) }
func trunc(s string) string {
	if len(s) > 80 {
		s = s[:80]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1] // never cut inside a multi-byte character
		}
	}
	return s
}

// trunc200 bounds a JSON pointer: an unknown key of 60,000 characters must not be echoed into the API.
func trunc200(s string) string {
	if len(s) > 200 {
		s = s[:200]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
