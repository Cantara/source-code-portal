package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	invopop "github.com/invopop/jsonschema"
	tekuri "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const schemaID = "https://github.com/Cantara/source-code-portal/schemas/config-v1.json"

var durationPattern = `^-?([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`

// Schema returns the JSON Schema (draft 2020-12) of the root struct, with descriptions from the struct tags,
// defaults from the embedded default document, and x-scp-owner / x-scp-env extension keywords.
func Schema(dst any, defaults []byte, overlays ...Overlay) ([]byte, error) {
	root, err := Describe(dst)
	if err != nil {
		return nil, err
	}
	l := &loader{}
	dt, _ := l.parse(defaults, "default.yaml")
	m, err := schemaMap(dst, root, dt, overlays)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, "", "  ")
}

func schemaMap(dst any, root *Field, defaults any, overlays []Overlay) (map[string]any, error) {
	r := &invopop.Reflector{
		FieldNameTag: "yaml", RequiredFromJSONSchemaTags: true, DoNotReference: true, AllowAdditionalProperties: false,
		BaseSchemaID: schemaID,
		Mapper: func(t reflect.Type) *invopop.Schema {
			switch {
			case t == durationType:
				return &invopop.Schema{Type: "string", Pattern: durationPattern, Examples: []any{"500ms", "30s", "5m"}}
			case t == secretType:
				var s invopop.Schema
				_ = json.Unmarshal([]byte(`{"type":"object","required":["file"],"additionalProperties":false,"properties":{"file":{"type":"string","pattern":"^/","description":"Absolute path of a file that holds the secret."}}}`), &s)
				return &s
			case reflect.PointerTo(t).Implements(textUnmarshaler):
				// a text leaf is a string ("120/min burst 60"); an integer-kinded one (a ByteSize) may also be a bare integer
				if k := t.Kind(); k >= reflect.Int && k <= reflect.Uint64 {
					return &invopop.Schema{OneOf: []*invopop.Schema{{Type: "string"}, {Type: "integer"}}}
				}
				return &invopop.Schema{Type: "string"}
			}
			return nil
		},
	}
	raw, err := json.Marshal(r.Reflect(dst))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["$id"] = schemaID
	m["title"] = "Source Code Portal configuration (version 1)"
	patch(m, root, defaults)
	for _, o := range overlays {
		if err := applyOverlay(m, o); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Overlay replaces the generated schema of one subtree with a hand-written JSON Schema, for the few shapes struct tags
// cannot express (a oneOf of matcher forms). Path is the dotted yaml path of the subtree, "[]" marking a list element:
// "catalog" or "github.sources[].discovery". The generated description, default and x-scp-* keywords are kept unless
// the overlay sets them itself. The struct stays the source of truth for decoding and env names; the overlay only
// tightens validation, so a key the struct does not have still fails at assign time.
type Overlay struct {
	Path   string
	Schema []byte
}

func applyOverlay(root map[string]any, o Overlay) error {
	var repl map[string]any
	if err := json.Unmarshal(o.Schema, &repl); err != nil {
		return fmt.Errorf("config schema overlay %s: %w", o.Path, err)
	}
	node := root
	for _, part := range strings.Split(o.Path, ".") {
		list := strings.HasSuffix(part, "[]")
		name := strings.TrimSuffix(part, "[]")
		props, _ := node["properties"].(map[string]any)
		next, ok := props[name].(map[string]any)
		if !ok {
			return fmt.Errorf("config schema overlay %s: no key %q", o.Path, name)
		}
		if list {
			if next, ok = next["items"].(map[string]any); !ok {
				return fmt.Errorf("config schema overlay %s: %q is not a list", o.Path, name)
			}
		}
		node = next
	}
	for _, keep := range []string{"description", "default", "x-scp-owner", "x-scp-env"} {
		if v, ok := node[keep]; ok {
			if _, set := repl[keep]; !set {
				repl[keep] = v
			}
		}
	}
	for k := range node {
		delete(node, k)
	}
	for k, v := range repl {
		node[k] = v
	}
	return nil
}

func patch(node map[string]any, f *Field, def any) {
	if node == nil {
		return
	}
	switch f.Kind {
	case KindStruct:
		props, _ := node["properties"].(map[string]any)
		dm, _ := def.(map[string]any)
		for _, c := range f.Children {
			cn, _ := props[c.Name].(map[string]any)
			if cn == nil {
				continue
			}
			if f.Path == "" {
				cn["x-scp-owner"] = c.Owner
			}
			patch(cn, c, dm[c.Name])
		}
	case KindList:
		items, _ := node["items"].(map[string]any)
		patch(items, f.Elem, nil)
	case KindMap:
		ap, _ := node["additionalProperties"].(map[string]any)
		patch(ap, f.Elem, nil)
	case KindBytes:
		for _, k := range []string{"type", "enum"} {
			delete(node, k)
		}
		node["type"] = []any{"integer", "string"}
		node["pattern"] = `^[0-9]+(B|KiB|MiB|GiB)?$`
	case KindUint:
		node["minimum"] = 0
	}
	if f.Env != "" && !f.NoEnv && f.Kind != KindStruct && f.Kind != KindMap && !(f.Kind == KindList && f.Elem.Kind == KindStruct) {
		node["x-scp-env"] = f.Env
	}
	if f.HasMin {
		node["x-scp-min"] = f.fmtBound(f.Min)
	}
	if f.HasMax {
		node["x-scp-max"] = f.fmtBound(f.Max)
	}
	if def != nil && f.Kind != KindStruct {
		node["default"] = jsonable(def)
	}
}

func jsonable(v any) any {
	switch x := v.(type) {
	case int64:
		return x
	case literalSecret:
		return nil
	}
	return v
}

func compile(dst any, root *Field, defaults any, overlays []Overlay) (*tekuri.Schema, error) {
	m, err := schemaMap(dst, root, defaults, overlays)
	if err != nil {
		return nil, err
	}
	c := tekuri.NewCompiler()
	if err := c.AddResource(schemaID, m); err != nil {
		return nil, fmt.Errorf("config schema: %w", err)
	}
	s, err := c.Compile(schemaID)
	if err != nil {
		return nil, fmt.Errorf("config schema: %w", err)
	}
	return s, nil
}

// forValidation replaces values that JSON Schema cannot represent.
func forValidation(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = forValidation(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = forValidation(e)
		}
		return out
	case literalSecret:
		return map[string]any{"file": "/from-environment"}
	case uint64:
		return float64(x)
	case time.Time:
		return x.Format(time.RFC3339)
	}
	return v
}

func (l *loader) validateTree(s *tekuri.Schema, root *Field) {
	err := s.Validate(forValidation(l.tree))
	if err == nil {
		l.checkRanges(l.tree, root, nil)
		return
	}
	ve, ok := err.(*tekuri.ValidationError)
	if !ok {
		l.viol("", err.Error(), "")
		return
	}
	pr := message.NewPrinter(language.English)
	seen := map[string]bool{}
	defer func() { l.checkRanges(l.tree, root, nil) }()
	var walk func(e *tekuri.ValidationError)
	walk = func(e *tekuri.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		path := l.instancePath(e.InstanceLocation)
		msg := e.ErrorKind.LocalizedString(pr)
		if ap, ok := e.ErrorKind.(*kind.AdditionalProperties); ok {
			parent := lookup(root, mustPath(path))
			for _, p := range ap.Properties {
				m := fmt.Sprintf("unknown key %q", p)
				if parent != nil && parent.Kind == KindStruct {
					var names []string
					for _, c := range parent.Children {
						names = append(names, c.Name)
					}
					if s := closest(p, names); s != "" {
						m += fmt.Sprintf(" (did you mean %q?)", s)
					}
				}
				k := path + "." + p
				if path == "" {
					k = p
				}
				if !seen[k+m] {
					seen[k+m] = true
					l.viol(k, m, "")
				}
			}
			return
		}
		if !seen[path+msg] {
			seen[path+msg] = true
			l.viol(path, msg, "")
		}
	}
	walk(ve)
}

func mustPath(p string) []seg {
	s, _ := parsePath(p)
	return s
}

// instancePath renders a JSON Schema instance location as a.b[0].c by looking at the tree.
func (l *loader) instancePath(loc []string) string {
	var segs []seg
	var cur any = l.tree
	for _, p := range loc {
		switch c := cur.(type) {
		case []any:
			var n int
			fmt.Sscanf(p, "%d", &n)
			segs = append(segs, seg{idx: n, isI: true})
			if n < len(c) {
				cur = c[n]
			}
		case map[string]any:
			segs = append(segs, seg{key: p})
			cur = c[p]
		default:
			segs = append(segs, seg{key: p})
		}
	}
	return joinPath(segs)
}

func closest(s string, names []string) string {
	sort.Strings(names)
	best, bd := "", 3
	for _, n := range names {
		if d := levenshtein(s, n); d < bd {
			best, bd = n, d
		}
	}
	return best
}

// Reference renders the configuration reference (Markdown): one table row per key with type, default, environment name and description.
func Reference(dst any, defaults []byte) (string, error) {
	root, err := Describe(dst)
	if err != nil {
		return "", err
	}
	l := &loader{}
	dt, _ := l.parse(defaults, "default.yaml")
	var b strings.Builder
	b.WriteString("| Key | Type | Default | Environment | Description |\n|---|---|---|---|---|\n")
	root.Walk(func(f *Field) {
		if f.Path == "" || f.Name == "" {
			return
		}
		def := ""
		if v, ok := getAt(dt, mustPath(strings.ReplaceAll(f.Path, "[]", "[0]"))); ok && f.Kind != KindStruct {
			bs, _ := json.Marshal(jsonable(v))
			def = "`" + string(bs) + "`"
		}
		env := ""
		if f.Env != "" && !f.NoEnv && f.Kind != KindStruct && f.Kind != KindMap && !(f.Kind == KindList && f.Elem.Kind == KindStruct) {
			env = "`" + f.Env + "`"
			if f.Kind == KindSecret {
				env += " or `" + f.Env + "_FILE`"
			}
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", f.Path, f.Kind, def, env, strings.ReplaceAll(f.Doc, "|", "\\|"))
	})
	return b.String(), nil
}

// checkRanges applies the scp min/max bounds of duration and bytes fields (JSON Schema cannot express them on strings).
func (l *loader) checkRanges(node any, f *Field, path []seg) {
	switch f.Kind {
	case KindStruct:
		m, _ := node.(map[string]any)
		for _, c := range f.Children {
			if v, ok := m[c.Name]; ok {
				l.checkRanges(v, c, append(append([]seg(nil), path...), seg{key: c.Name}))
			}
		}
	case KindList:
		list, _ := node.([]any)
		for i, e := range list {
			l.checkRanges(e, f.Elem, append(append([]seg(nil), path...), seg{idx: i, isI: true}))
		}
	case KindMap:
		m, _ := node.(map[string]any)
		for k, e := range m {
			l.checkRanges(e, f.Elem, append(append([]seg(nil), path...), seg{key: k}))
		}
	case KindDuration, KindBytes:
		if !f.HasMin && !f.HasMax {
			return
		}
		var n int64
		switch x := node.(type) {
		case string:
			if f.Kind == KindDuration {
				d, err := time.ParseDuration(x)
				if err != nil {
					return
				}
				n = int64(d)
			} else {
				b, err := ParseBytes(x)
				if err != nil {
					return
				}
				n = b
			}
		case int64:
			n = x
		default:
			return
		}
		if (f.HasMin && n < f.Min) || (f.HasMax && n > f.Max) {
			l.viol(joinPath(path), fmt.Sprintf("%v is outside the allowed range %s", node, f.rangeText()), "")
		}
	}
}
