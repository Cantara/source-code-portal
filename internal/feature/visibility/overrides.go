package visibility

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Overrides is the stored document (design/config-baseline.md, CF1): the admins' changes to the configuration's `visibility:`. A
// product or repository with a setting overrides the file's choice; one with null drops it, so the item inherits again. Rules, when
// present, replace the file's list. A store of 2.2.19 and 2.2.20 holds a whole document without a version, read as overrides of an
// empty file whose empty rules mean no change: those releases wrote `"rules": []` for "no rules", not for "none of the file's".
type Overrides struct {
	Version int                 `json:"version,omitempty"` // overridesVersion; 0: a whole document of 2.2.20 or before
	Groups  map[string]*Setting `json:"groups,omitempty"`
	Repos   map[string]*Setting `json:"repos,omitempty"`
	Rules   *[]Rule             `json:"rules,omitempty"`
}

// overridesVersion marks a document written as overrides, whose rules, even empty, replace the file's.
const overridesVersion = 2

// Count is how many changes o holds: one per product and repository, one for the rules.
func (o Overrides) Count() int {
	n := len(o.Groups) + len(o.Repos)
	if o.Rules != nil {
		n++
	}
	return n
}

// ParseOverrides decodes and checks the stored document: at most 256 KiB of one JSON object, unknown fields refused.
func ParseOverrides(b []byte) (Overrides, error) {
	if len(b) > maxBytes {
		return Overrides{}, fmt.Errorf("larger than %d KiB", maxBytes>>10)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var o Overrides
	if err := dec.Decode(&o); err != nil {
		return Overrides{}, fmt.Errorf("not a visibility document: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Overrides{}, errors.New("not a visibility document: more than one JSON value")
	}
	// The settings and the rules are checked as a document is; the nulls are kept apart, their keys checked here.
	for field, m := range map[string]map[string]*Setting{"groups": o.Groups, "repos": o.Repos} {
		for k, st := range m {
			if st == nil && (field == "groups" && !groupID.MatchString(lower(k)) || field == "repos" && !repoKey.MatchString(lower(k))) {
				return Overrides{}, fmt.Errorf("%s: %q is not a product id or an address", field, k)
			}
		}
	}
	if o.Version == 0 && o.Rules != nil && len(*o.Rules) == 0 {
		o.Rules = nil // a whole document's "no rules": no change to the file's
	}
	d := Doc{Groups: set(o.Groups), Repos: set(o.Repos)}
	if o.Rules != nil {
		d.Rules = *o.Rules
	}
	c, err := clean(d)
	if err != nil {
		return Overrides{}, err
	}
	out := Overrides{Version: o.Version, Groups: unset(o.Groups, c.Groups), Repos: unset(o.Repos, c.Repos)}
	if o.Rules != nil {
		out.Rules = &c.Rules
	}
	return out, nil
}

// set is the non-null settings of m.
func set(m map[string]*Setting) map[string]Setting {
	out := map[string]Setting{}
	for k, st := range m {
		if st != nil {
			out[k] = *st
		}
	}
	return out
}

// unset is the cleaned settings with the nulls of m added back, the keys in lower case.
func unset(m map[string]*Setting, cleaned map[string]Setting) map[string]*Setting {
	if len(m) == 0 {
		return nil
	}
	out := map[string]*Setting{}
	for k, st := range cleaned {
		out[k] = &st
	}
	for k, st := range m {
		if st == nil {
			out[lower(k)] = nil
		}
	}
	return out
}

// Merge is the document in force: the file's base with the overrides o applied.
func Merge(base Doc, o Overrides) Doc {
	d := Doc{Groups: maps.Clone(base.Groups), Repos: maps.Clone(base.Repos), Rules: base.Rules}
	apply := func(dst map[string]Setting, src map[string]*Setting) {
		for k, st := range src {
			if st == nil {
				delete(dst, k)
			} else {
				dst[k] = *st
			}
		}
	}
	apply(d.Groups, o.Groups)
	apply(d.Repos, o.Repos)
	if o.Rules != nil {
		d.Rules = *o.Rules
	}
	return d
}

// Diff is the overrides that turn the file's base into the document d: nothing for what d leaves as the file has it.
func Diff(base, d Doc) Overrides {
	diff := func(b, n map[string]Setting) map[string]*Setting {
		out := map[string]*Setting{}
		for k, st := range n {
			if was, ok := b[k]; !ok || was != st {
				out[k] = &st
			}
		}
		for k := range b {
			if _, ok := n[k]; !ok {
				out[k] = nil
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	o := Overrides{Version: overridesVersion, Groups: diff(base.Groups, d.Groups), Repos: diff(base.Repos, d.Repos)}
	if !slices.EqualFunc(base.Rules, d.Rules, func(a, b Rule) bool { return a.Pattern == b.Pattern && a.Setting == b.Setting }) {
		rules := slices.Clone(d.Rules)
		if rules == nil {
			rules = []Rule{}
		}
		o.Rules = &rules
	}
	return o
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
