// Package config is the strict layered configuration machinery (rank 1: imports platform/secret only).
// It knows no feature: the root struct is composed in cmd/scp from feature-owned Config types.
package config

import (
	"encoding"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// Kind classifies a configuration field.
type Kind uint8

const (
	KindStruct Kind = iota + 1
	KindList        // a YAML sequence; Elem describes one element
	KindMap         // a YAML mapping with free keys; Elem describes a value
	KindString
	KindInt
	KindUint
	KindFloat
	KindBool
	KindDuration // string such as 30s; an integer is refused
	KindBytes    // int64 field tagged scp:"unit=bytes"; 64MiB or an integer
	KindSecret   // {file: /abs/path} in YAML, SCP_X_FILE or SCP_X in the environment
	KindText     // encoding.TextUnmarshaler, written as a string
)

func (k Kind) String() string {
	return [...]string{"", "object", "list", "map", "string", "integer", "unsigned integer", "number", "boolean", "duration", "bytes", "secret", "text"}[k]
}

// Field describes one configuration key. Path uses "[]" for a list element: "github.sources[].auth.kind".
type Field struct {
	Name     string // yaml name; "" for list and map elements
	Path     string
	Kind     Kind
	Doc      string
	Owner    string // package path of the root section type that contains the field
	Type     reflect.Type
	Index    []int    // struct field index path inside the parent struct
	Children []*Field // KindStruct, in declaration order
	Inlines  [][]int  // KindStruct: index paths of embedded yaml:",inline" structs (their Validate methods run at this path)
	Elem     *Field   // KindList, KindMap
	NoEnv    bool     // scp:"noenv"
	Env      string   // env template, e.g. SCP_GITHUB_SOURCES_{i}_AUTH_KIND; "" when not settable
	Tag      reflect.StructTag
	Min, Max int64 // bounds of a duration (nanoseconds) or bytes field from scp:"min=...,max=..."; HasMin/HasMax tell if set
	HasMin   bool
	HasMax   bool
}

var yamlName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var (
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
	durationType    = reflect.TypeFor[time.Duration]()
	secretType      = reflect.TypeFor[secret.Secret]()
)

// Describe walks the root struct and returns its field tree. It fails on the first malformed declaration set,
// listing all of them: a field without a yaml name, without jsonschema_description, a non-snake_case name,
// a colliding environment name, an unsupported type.
func Describe(root any) (*Field, error) {
	t := reflect.TypeOf(root)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("config root must be a struct, got %s", t)
	}
	d := &describer{envs: map[string]string{}}
	f := d.field("", "", t, "", "", nil, "", false)
	f.Path = ""
	if len(d.errs) > 0 {
		sort.Strings(d.errs)
		return nil, fmt.Errorf("config declarations: %s", strings.Join(d.errs, "; "))
	}
	return f, nil
}

type describer struct {
	errs []string
	envs map[string]string
}

func (d *describer) errf(format string, a ...any) { d.errs = append(d.errs, fmt.Sprintf(format, a...)) }

func envSegment(name string) string { return strings.ToUpper(strings.ReplaceAll(name, "-", "_")) }

// field builds the descriptor for a value of type t at yaml path `path`, env template `env`.
func (d *describer) field(name, path string, t reflect.Type, doc, owner string, idx []int, env string, noenv bool) *Field {
	f := &Field{Name: name, Path: path, Doc: doc, Owner: owner, Type: t, Index: idx, Env: env, NoEnv: noenv}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == secretType:
		f.Kind = KindSecret
	case t == durationType:
		f.Kind = KindDuration
	case reflect.PointerTo(t).Implements(textUnmarshaler):
		f.Kind = KindText
	case t.Kind() == reflect.Struct:
		f.Kind = KindStruct
		if m, ok := reflect.PointerTo(t).MethodByName("Validate"); ok {
			switch m.Type.String() { // the receiver is the first parameter
			case "func(*" + t.String() + ", func(string, string))", "func(*" + t.String() + ") error", "func(*" + t.String() + ", string) error":
			default:
				d.errf("%s: Validate has the signature %s; use Validate(report func(path, msg string)), Validate() error or Validate(prefix string) error", path, m.Type)
			}
		}
		var addFields func(st reflect.Type, prefix []int, ownOverride string)
		addFields = func(st reflect.Type, prefix []int, ownOverride string) {
			for i := 0; i < st.NumField(); i++ {
				sf := st.Field(i)
				if !sf.IsExported() {
					continue
				}
				tagName, tagOpts, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
				if tagName == "-" {
					continue
				}
				if sf.Anonymous && strings.Contains(","+tagOpts+",", ",inline,") {
					// yaml:",inline": the embedded struct's keys appear at this level (two owners, one YAML mapping).
					if sf.Type.Kind() != reflect.Struct {
						d.errf("%s.%s: an inline field must be an embedded struct value, not %s", st, sf.Name, sf.Type)
						continue
					}
					f.Inlines = append(f.Inlines, append(append([]int(nil), prefix...), sf.Index...))
					addFields(sf.Type, append(append([]int(nil), prefix...), sf.Index...), sf.Type.PkgPath())
					continue
				}
				here := path + "." + tagName
				if path == "" {
					here = tagName
				}
				if tagName == "" || !yamlName.MatchString(tagName) {
					d.errf("%s.%s: yaml name %q must be snake_case", t, sf.Name, tagName)
					continue
				}
				fdoc := sf.Tag.Get("jsonschema_description")
				if fdoc == "" {
					d.errf("%s: no jsonschema_description (every key is documented)", here)
				}
				own := owner
				if ownOverride != "" {
					own = ownOverride
				} else if path == "" { // a root section: its owner is the package that declares the section type
					pt := sf.Type
					for pt.Kind() == reflect.Pointer {
						pt = pt.Elem()
					}
					own = pt.PkgPath()
				}
				cenv := ""
				if env != "" || path == "" {
					base := env
					if base == "" {
						base = "SCP"
					}
					cenv = base + "_" + envSegment(tagName)
				}
				sc := scpOpts(sf.Tag)
				child := d.field(tagName, here, sf.Type, fdoc, own, append(append([]int(nil), prefix...), sf.Index...), cenv, sc["noenv"] != "")
				child.Tag = sf.Tag
				if sc["unit"] == "bytes" {
					if child.Kind != KindInt {
						d.errf("%s: unit=bytes needs an integer field", here)
					}
					child.Kind = KindBytes
				}
				for _, b := range []struct {
					key string
					set *bool
					val *int64
				}{{"min", &child.HasMin, &child.Min}, {"max", &child.HasMax, &child.Max}} {
					raw, ok := sc[b.key]
					if !ok {
						continue
					}
					switch child.Kind {
					case KindDuration:
						dv, err := time.ParseDuration(raw)
						if err != nil {
							d.errf("%s: scp %s=%q: %v", here, b.key, raw, err)
						}
						*b.val, *b.set = int64(dv), true
					case KindBytes:
						bv, err := ParseBytes(raw)
						if err != nil {
							d.errf("%s: scp %s=%q: %v", here, b.key, raw, err)
						}
						*b.val, *b.set = bv, true
					default:
						d.errf("%s: scp %s is only for duration and bytes fields (use jsonschema minimum/maximum)", here, b.key)
					}
				}
				for _, prev := range f.Children {
					if prev.Name == tagName {
						d.errf("%s: key declared twice (an inline field repeats it)", here)
					}
				}
				f.Children = append(f.Children, child)
			}

		}
		addFields(t, nil, "")
	case t.Kind() == reflect.Slice:
		f.Kind = KindList
		elemEnv := ""
		if env != "" {
			elemEnv = env + "_{i}"
		}
		f.Elem = d.field("", path+"[]", t.Elem(), doc, owner, nil, elemEnv, noenv)
		if f.Elem.Kind == KindList || f.Elem.Kind == KindMap {
			d.errf("%s: list of lists/maps is not supported", path)
		}
		if f.Elem.Kind != KindStruct && env != "" {
			f.Env = env // comma-separated list in one variable
			f.Elem.Env = ""
		}
	case t.Kind() == reflect.Map:
		f.Kind = KindMap
		if t.Key().Kind() != reflect.String {
			d.errf("%s: map keys must be strings", path)
		}
		elemEnv := ""
		if env != "" {
			elemEnv = env + "_{k}" // {k}: an entry key present in the defaults or the file
		}
		f.Elem = d.field("", path+".*", t.Elem(), doc, owner, nil, elemEnv, noenv)
		f.Env = "" // the map itself is not a variable; its entries' fields are
	case t.Kind() == reflect.String:
		f.Kind = KindString
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64:
		f.Kind = KindInt
	case t.Kind() >= reflect.Uint && t.Kind() <= reflect.Uint64:
		f.Kind = KindUint
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		f.Kind = KindFloat
	case t.Kind() == reflect.Bool:
		f.Kind = KindBool
	default:
		d.errf("%s: unsupported type %s", path, t)
	}
	settable := f.Kind != KindStruct && f.Kind != KindMap && !(f.Kind == KindList && f.Elem.Kind == KindStruct)
	if f.Env != "" && !f.NoEnv && settable {
		canon := strings.ReplaceAll(strings.ReplaceAll(f.Env, "{i}", "#"), "{k}", "#")
		if prev, dup := d.envs[canon]; dup {
			d.errf("environment name %s is used by %s and %s", f.Env, prev, path)
		}
		d.envs[canon] = path
		if f.Kind == KindSecret { // the _FILE twin of a secret is a second variable that no other key may claim
			twin := canon + "_FILE"
			if prev, dup := d.envs[twin]; dup {
				d.errf("environment name %s_FILE is used by %s (the file form of a secret) and %s", f.Env, path, prev)
			}
			d.envs[twin] = path + " (_FILE)"
		}
	}
	return f
}

// scpOpts parses scp:"a,b=c".
func scpOpts(tag reflect.StructTag) map[string]string {
	m := map[string]string{}
	for _, o := range strings.Split(tag.Get("scp"), ",") {
		if o == "" {
			continue
		}
		k, v, _ := strings.Cut(o, "=")
		if v == "" {
			v = "true"
		}
		m[k] = v
	}
	return m
}

// Walk calls fn for every field in declaration order (depth first), including list and map elements.
func (f *Field) Walk(fn func(*Field)) {
	fn(f)
	for _, c := range f.Children {
		c.Walk(fn)
	}
	if f.Elem != nil {
		f.Elem.Walk(fn)
	}
}

// Child returns the struct child with the given yaml name.
func (f *Field) Child(name string) *Field {
	for _, c := range f.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}
