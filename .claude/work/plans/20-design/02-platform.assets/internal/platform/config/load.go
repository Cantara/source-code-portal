package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// MaxFileBytes bounds the configuration file (an operator file is a few KiB; a larger one is a mistake or an attack on the parser).
const MaxFileBytes = 1 << 20

// Layer names where a value came from.
type Layer string

const (
	LayerDefault Layer = "default"
	LayerFile    Layer = "file"
	LayerEnv     Layer = "env"
	LayerFlag    Layer = "flag"
)

// Options configures Load. Nothing here reads the process environment or the file system on its own.
type Options struct {
	Defaults    []byte                       // embedded default.yaml (the only source of defaults)
	File        string                       // "" = no file
	ReadFile    func(string) ([]byte, error) // required when File is set (cmd/scp passes os.ReadFile)
	Environ     []string                     // KEY=VALUE pairs as os.Environ returns them; nil = skip the environment layer
	Set         []string                     // repeated --set path=value flags
	SkipSecrets bool                         // lint: validate {file: ...} references without reading the files
	Overlays    []Overlay                    // hand-written schema fragments that replace generated subtrees (see Overlay)
}

// Applied records one non-default value so that startup can log which layer set which key (never the value).
type Applied struct {
	Layer Layer
	Path  string
	From  string // env variable or flag text
}

// Result carries non-fatal findings.
type Result struct {
	Applied  []Applied
	Warnings []string
}

// Violation is one configuration problem.
type Violation struct {
	Path    string
	Message string
	File    string
	Line    int
}

// Error is returned by Load; it lists every problem, not just the first.
type Error struct{ Violations []Violation }

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d configuration problem(s):", len(e.Violations))
	for _, v := range e.Violations {
		b.WriteString("\n  ")
		switch {
		case v.File != "" && v.Line > 0:
			fmt.Fprintf(&b, "%s:%d: ", v.File, v.Line)
		case v.File != "":
			fmt.Fprintf(&b, "%s: ", v.File)
		}
		if v.Path != "" {
			b.WriteString(v.Path + ": ")
		}
		b.WriteString(v.Message)
	}
	return b.String()
}

// Validator is implemented by section and element types for cross-field rules. report paths are relative
// to the receiver; Load prefixes them with the receiver's own path. The signature uses only builtin types on
// purpose: a feature implements it without importing this package.
type Validator interface {
	Validate(report func(path, msg string))
}

type loader struct {
	opts     Options
	env      map[string]string
	envKeys  []string
	envUsed  map[string]bool
	tree     map[string]any
	lines    map[string]int
	file     string
	viols    []Violation
	warnings []string
	applied  []Applied
	secrets  []secretSlot
	layer    Layer
}

func (l *loader) viol(path, msg, _ string) {
	v := Violation{Path: path, Message: msg}
	for _, o := range l.viols { // identical reports collapse (a promoted Validate and the inline call can both fire)
		if o.Path == path && o.Message == msg {
			return
		}
	}
	if l.lines != nil {
		if n, ok := l.lines[path]; ok {
			v.File, v.Line = l.file, n
		}
	}
	l.viols = append(l.viols, v)
}

// setLeaf writes a value at a path from the current layer and records it.
func (l *loader) setLeaf(path []seg, val any, from string) {
	n, err := set(l.tree, path, val)
	if err != nil {
		l.viol(joinPath(path), from+": "+err.Error(), "")
		return
	}
	l.tree = n.(map[string]any)
	l.applied = append(l.applied, Applied{Layer: l.layer, Path: joinPath(path), From: from})
}

// Load fills dst (a pointer to the root config struct) from the layers
// embedded default < file < environment < flags, validates it against the generated JSON Schema and then
// calls every Validate method. All problems are reported together.
func Load(dst any, o Options) (Result, error) {
	root, err := Describe(dst)
	if err != nil {
		return Result{}, err
	}
	l := &loader{opts: o, tree: map[string]any{}, env: map[string]string{}}
	l.layer = LayerDefault
	if dt, ok := l.parse(o.Defaults, "default.yaml"); ok {
		l.tree = merge(l.tree, dt).(map[string]any)
	}
	if o.File != "" {
		l.layer = LayerFile
		data, rerr := o.ReadFile(o.File)
		if rerr != nil {
			return Result{}, fmt.Errorf("config file: %w", rerr)
		}
		if len(data) > MaxFileBytes {
			return Result{}, fmt.Errorf("config file %s: %d bytes exceeds the %d byte limit", o.File, len(data), MaxFileBytes)
		}
		l.file = o.File
		if ft, ok := l.parse(data, o.File); ok {
			l.lines = lineIndex(data)
			l.tree = merge(l.tree, ft).(map[string]any)
			walkLeaves(ft, nil, func(p []seg) { l.applied = append(l.applied, Applied{Layer: LayerFile, Path: joinPath(p)}) })
		}
	}
	if o.Environ != nil {
		l.layer = LayerEnv
		for _, kv := range o.Environ {
			if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "SCP_") {
				l.env[k] = v
			}
		}
		l.overlayEnv(root)
	}
	l.layer = LayerFlag
	for _, s := range o.Set {
		l.applySet(root, s)
	}
	if len(l.viols) > 0 {
		return Result{}, l.fail()
	}
	sch, err := compile(dst, root, l.defaultsTree(), o.Overlays)
	if err != nil {
		return Result{}, err
	}
	l.validateTree(sch, root)
	if len(l.viols) > 0 {
		return Result{}, l.fail()
	}
	rv := reflect.ValueOf(dst).Elem()
	l.assign(rv, root, l.tree, nil)
	if !o.SkipSecrets {
		for _, s := range l.secrets {
			if err := s.ptr.Resolve(); err != nil {
				l.viol(s.path, err.Error(), "")
			}
		}
	}
	if len(l.viols) == 0 {
		l.runValidators(rv, root, nil)
	}
	if len(l.viols) > 0 {
		return Result{}, l.fail()
	}
	return Result{Applied: l.applied, Warnings: l.warnings}, nil
}

func (l *loader) defaultsTree() any {
	dl := &loader{}
	t, _ := dl.parse(l.opts.Defaults, "default.yaml")
	return t
}

func (l *loader) fail() error {
	sort.SliceStable(l.viols, func(i, j int) bool {
		a, b := l.viols[i], l.viols[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Path < b.Path
	})
	return &Error{Violations: l.viols}
}

// parse reads exactly one YAML document into a normalised tree.
func (l *loader) parse(data []byte, name string) (map[string]any, bool) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, true
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var v any
	if err := dec.Decode(&v); err != nil {
		l.viols = append(l.viols, Violation{File: name, Message: strings.TrimPrefix(err.Error(), "yaml: ")})
		return nil, false
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		l.viols = append(l.viols, Violation{File: name, Message: "more than one YAML document"})
		return nil, false
	}
	n, err := normalize(v, nil)
	if err != nil {
		l.viols = append(l.viols, Violation{File: name, Message: err.Error()})
		return nil, false
	}
	m, ok := n.(map[string]any)
	if !ok {
		l.viols = append(l.viols, Violation{File: name, Message: "the document must be a mapping"})
		return nil, false
	}
	return m, true
}

// normalize converts yaml.v3's generic output: string keys only, int -> int64.
func normalize(v any, path []seg) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			n, err := normalize(e, append(path, seg{key: k}))
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case map[any]any:
		return nil, fmt.Errorf("%s: mapping keys must be strings", joinPath(path))
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			n, err := normalize(e, append(path, seg{idx: i, isI: true}))
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case int:
		return int64(x), nil
	}
	return v, nil
}

func walkLeaves(v any, path []seg, fn func([]seg)) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkLeaves(x[k], append(append([]seg(nil), path...), seg{key: k}), fn)
		}
	case []any:
		for i, e := range x {
			walkLeaves(e, append(append([]seg(nil), path...), seg{idx: i, isI: true}), fn)
		}
	default:
		fn(path)
	}
}

// lineIndex maps "a.b[0].c" to the line of its value in the file.
func lineIndex(data []byte) map[string]int {
	out := map[string]int{}
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 {
		return out
	}
	var walk func(n *yaml.Node, path []seg)
	walk = func(n *yaml.Node, path []seg) {
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				p := append(append([]seg(nil), path...), seg{key: n.Content[i].Value})
				out[joinPath(p)] = n.Content[i].Line
				walk(n.Content[i+1], p)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				p := append(append([]seg(nil), path...), seg{idx: i, isI: true})
				out[joinPath(p)] = c.Line
				walk(c, p)
			}
		}
	}
	walk(doc.Content[0], nil)
	return out
}

func (l *loader) runValidators(v reflect.Value, f *Field, path []seg) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	l.callValidator(v, path)
	switch f.Kind {
	case KindStruct:
		for _, idx := range f.Inlines { // embedded yaml:",inline" structs validate themselves at this path
			l.callValidator(v.FieldByIndex(idx), path)
		}
		for _, c := range f.Children {
			l.runValidators(v.FieldByIndex(c.Index), c, append(append([]seg(nil), path...), seg{key: c.Name}))
		}
	case KindList:
		for i := 0; i < v.Len(); i++ {
			l.runValidators(v.Index(i), f.Elem, append(append([]seg(nil), path...), seg{idx: i, isI: true}))
		}
	case KindMap:
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, k := range keys {
			ev := reflect.New(v.Type().Elem()).Elem()
			ev.Set(v.MapIndex(k))
			l.runValidators(ev, f.Elem, append(append([]seg(nil), path...), seg{key: k.String()}))
		}
	}
}

// pathed is the structural interface a feature's validation error may implement so the loader can place it:
// the unit never imports this package. ConfigPath is relative to the section whose Validate returned the error
// (an already absolute path, one that starts with the section path, is also accepted).
type pathed interface{ ConfigPath() string }

// callValidator runs a section's Validate method in any of the three accepted shapes (Describe refuses any other):
//
//	Validate(report func(path, msg string))   preferred: no import, no error juggling
//	Validate() error                          the error tree (errors.Join, multiple %w) is walked
//	Validate(prefix string) error             called with the section's own path as prefix; walked the same way
func (l *loader) callValidator(v reflect.Value, path []seg) {
	if !v.CanAddr() {
		return
	}
	m := v.Addr().MethodByName("Validate")
	if !m.IsValid() {
		return
	}
	section := joinPath(path)
	abs := func(p string) string {
		switch {
		case p == "":
			return section
		case section == "" || p == section || strings.HasPrefix(p, section+".") || strings.HasPrefix(p, section+"["):
			return p
		}
		return section + "." + p
	}
	switch fn := m.Interface().(type) {
	case func(func(string, string)):
		fn(func(p, msg string) { l.viol(abs(p), msg, "") })
	case func() error:
		l.walkErr(fn(), section, abs)
	case func(string) error:
		l.walkErr(fn(section), section, abs)
	}
}

// walkErr flattens an error tree and reports each leaf at the path it names (or at the section).
func (l *loader) walkErr(err error, section string, abs func(string) string) {
	var leaves []error
	var flat func(e error)
	flat = func(e error) {
		if e == nil {
			return
		}
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			for _, c := range j.Unwrap() {
				flat(c)
			}
			return
		}
		leaves = append(leaves, e)
	}
	flat(err)
	for _, e := range leaves {
		msg := e.Error()
		var pe pathed
		if errors.As(e, &pe) {
			p := pe.ConfigPath()
			l.viol(abs(p), strings.TrimPrefix(msg, p+": "), "")
			continue
		}
		// "store.backup.interval: must be at least 1m": a leading absolute path in the text is promoted to the path.
		if head, rest, ok := strings.Cut(msg, ": "); ok && section != "" && (head == section || strings.HasPrefix(head, section+".") || strings.HasPrefix(head, section+"[")) && !strings.ContainsAny(head, " \t") {
			l.viol(head, rest, "")
			continue
		}
		l.viol(section, msg, "")
	}
}

// applySet applies one --set path=value flag.
func (l *loader) applySet(root *Field, s string) {
	p, val, ok := strings.Cut(s, "=")
	if !ok {
		l.viol("", fmt.Sprintf("--set %q: want path=value", s), "")
		return
	}
	segs, err := parsePath(p)
	if err != nil {
		l.viol("", fmt.Sprintf("--set %q: %v", s, err), "")
		return
	}
	f := lookup(root, segs)
	if f == nil {
		l.viol(p, "--set: no such configuration key", "")
		return
	}
	switch f.Kind {
	case KindStruct, KindMap:
		l.viol(p, "--set: a section cannot be set from a flag", "")
	case KindSecret:
		l.viol(p, "--set: secrets cannot be set from a flag (the value would be visible in the process list); use SCP_..._FILE or {file: ...}", "")
	case KindList:
		if f.Elem.Kind == KindStruct {
			l.viol(p, "--set: set a field of a list element, for example path[0].field", "")
			return
		}
		v, err := coerceList(f, val)
		if err != nil {
			l.viol(p, "--set: "+err.Error(), "")
			return
		}
		l.setLeaf(segs, v, "--set "+p)
	default:
		v, err := coerce(f, val)
		if err != nil {
			l.viol(p, "--set: "+err.Error(), "")
			return
		}
		l.setLeaf(segs, v, "--set "+p)
	}
}

// lookup finds the field a path addresses.
func lookup(root *Field, segs []seg) *Field {
	cur := root
	for _, s := range segs {
		switch {
		case s.isI:
			if cur.Kind != KindList {
				return nil
			}
			cur = cur.Elem
		case cur.Kind == KindStruct:
			if cur = cur.Child(s.key); cur == nil {
				return nil
			}
		case cur.Kind == KindMap:
			cur = cur.Elem
		default:
			return nil
		}
	}
	return cur
}
