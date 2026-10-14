package config

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

type secretSlot struct {
	path string
	ptr  *secret.Secret
}

// assign builds the typed value from the validated tree. The schema has already enforced types and constraints,
// so a failure here is a bug in the machinery or a rule the schema cannot express.
func (l *loader) assign(v reflect.Value, f *Field, node any, path []seg) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	here := joinPath(path)
	bad := func(format string, a ...any) { l.viol(here, fmt.Sprintf(format, a...), "") }
	if node == nil {
		return
	}
	switch f.Kind {
	case KindStruct:
		m, ok := node.(map[string]any)
		if !ok {
			bad("want a mapping")
			return
		}
		for _, c := range f.Children {
			if cv, has := m[c.Name]; has {
				l.assign(v.FieldByIndex(c.Index), c, cv, append(append([]seg(nil), path...), seg{key: c.Name}))
			}
		}
	case KindList:
		list, ok := node.([]any)
		if !ok {
			bad("want a list")
			return
		}
		out := reflect.MakeSlice(v.Type(), len(list), len(list))
		v.Set(out)
		for i, e := range list {
			l.assign(v.Index(i), f.Elem, e, append(append([]seg(nil), path...), seg{idx: i, isI: true}))
		}
	case KindMap:
		m, ok := node.(map[string]any)
		if !ok {
			bad("want a mapping")
			return
		}
		out := reflect.MakeMapWithSize(v.Type(), len(m))
		for k, e := range m {
			ev := reflect.New(v.Type().Elem()).Elem()
			l.assign(ev, f.Elem, e, append(append([]seg(nil), path...), seg{key: k}))
			out.SetMapIndex(reflect.ValueOf(k).Convert(v.Type().Key()), ev)
		}
		v.Set(out)
	case KindString:
		s, ok := node.(string)
		if !ok {
			bad("want a string")
			return
		}
		v.SetString(s)
	case KindInt, KindBytes:
		var n int64
		switch x := node.(type) {
		case int64:
			n = x
		case int:
			n = int64(x)
		case uint64:
			n = int64(x)
		case string:
			var err error
			if n, err = ParseBytes(x); err != nil || f.Kind != KindBytes {
				bad("%v", err)
				return
			}
		default:
			bad("want an integer")
			return
		}
		if v.OverflowInt(n) {
			bad("%d overflows %s", n, v.Type())
			return
		}
		v.SetInt(n)
	case KindUint:
		var n uint64
		switch x := node.(type) {
		case int64:
			n = uint64(x)
		case int:
			n = uint64(x)
		case uint64:
			n = x
		default:
			bad("want a non-negative integer")
			return
		}
		v.SetUint(n)
	case KindFloat:
		switch x := node.(type) {
		case float64:
			v.SetFloat(x)
		case int:
			v.SetFloat(float64(x))
		case int64:
			v.SetFloat(float64(x))
		default:
			bad("want a number")
		}
	case KindBool:
		b, ok := node.(bool)
		if !ok {
			bad("want true or false")
			return
		}
		v.SetBool(b)
	case KindDuration:
		s, ok := node.(string)
		if !ok {
			bad("want a duration string such as 30s")
			return
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			bad("%v", err)
			return
		}
		v.SetInt(int64(d))
	case KindText:
		var s string
		switch x := node.(type) { // a text leaf also takes a bare integer (ByteSize: 524288), formatted and handed to UnmarshalText
		case string:
			s = x
		case int64:
			s = strconv.FormatInt(x, 10)
		case uint64:
			s = strconv.FormatUint(x, 10)
		default:
			bad("want a string or an integer")
			return
		}
		tu, ok := v.Addr().Interface().(encoding.TextUnmarshaler)
		if !ok {
			bad("internal: %s is not a TextUnmarshaler", v.Type())
			return
		}
		if err := tu.UnmarshalText([]byte(s)); err != nil {
			bad("%v", err)
		}
	case KindSecret:
		sp := v.Addr().Interface().(*secret.Secret)
		switch x := node.(type) {
		case literalSecret:
			*sp = secret.New(string(x))
		case map[string]any:
			p, _ := x["file"].(string)
			*sp = secret.Ref(p)
		default:
			bad("a secret must be written as {file: /absolute/path}")
			return
		}
		l.secrets = append(l.secrets, secretSlot{path: here, ptr: sp})
	}
}

func (f *Field) rangeText() string {
	lo, hi := "-", "-"
	if f.HasMin {
		lo = f.fmtBound(f.Min)
	}
	if f.HasMax {
		hi = f.fmtBound(f.Max)
	}
	return lo + " .. " + hi
}

func (f *Field) fmtBound(n int64) string {
	if f.Kind == KindDuration {
		return time.Duration(n).String()
	}
	return fmt.Sprintf("%d bytes", n)
}
