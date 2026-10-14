package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var bytesRe = regexp.MustCompile(`^([0-9]+)(B|KiB|MiB|GiB)?$`)

// ParseBytes parses "512000", "16KiB", "64MiB", "2GiB" (IEC units only; SI units are refused so that 64MB cannot mean two things).
func ParseBytes(s string) (int64, error) {
	m := bytesRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("%q is not a byte size (use an integer or a number with B, KiB, MiB or GiB)", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", s)
	}
	mult := map[string]int64{"": 1, "B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30}[m[2]]
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("%q is out of range", s)
	}
	return n * mult, nil
}

// coerce converts an environment or flag string to the tree value of a leaf field.
func coerce(f *Field, s string) (any, error) {
	switch f.Kind {
	case KindString, KindText:
		return s, nil
	case KindInt:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", s)
		}
		return n, nil
	case KindUint:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a non-negative integer", s)
		}
		return n, nil
	case KindFloat:
		x, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		return x, nil
	case KindBool:
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not a boolean (use true or false)", s)
	case KindDuration:
		if _, err := time.ParseDuration(s); err != nil {
			return nil, fmt.Errorf("%q is not a duration (examples: 500ms, 30s, 5m, 1h)", s)
		}
		return s, nil
	case KindBytes:
		return ParseBytes(s)
	}
	return nil, fmt.Errorf("%s cannot be set from a string", f.Kind)
}

// coerceList splits a comma-separated value for a list of scalars.
func coerceList(f *Field, s string) (any, error) {
	out := []any{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		v, err := coerce(f.Elem, strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
