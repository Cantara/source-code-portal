package config

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Reserved environment variables that are bootstrap inputs, not configuration keys.
var reservedEnv = map[string]bool{"SCP_CONFIG": true}

// Kubernetes injects {SERVICE}_SERVICE_HOST, {SERVICE}_PORT_... for a Service named "scp": never an error.
var kubeServiceLink = regexp.MustCompile(`^SCP_(SERVICE_(HOST|PORT)(_.*)?|PORT(_.*)?)$`)

// overlayEnv applies SCP_* variables to the tree using names derived from the yaml paths.
// Rules: SCP_<PATH>, path segments upper-cased with '-' as '_', list indices as numbers
// (SCP_GITHUB_SOURCES_0_AUTH_KIND); a list of scalars is one comma-separated variable; a map entry that already exists in the defaults or the file is addressed by its key (SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY), env never creates entries;
// a secret leaf takes SCP_X (literal) or SCP_X_FILE (path), never both; an empty value counts as unset.
func (l *loader) overlayEnv(root *Field) {
	l.envUsed = map[string]bool{}
	keys := make([]string, 0, len(l.env))
	for k := range l.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	l.envKeys = keys
	for _, c := range root.Children {
		l.envField(c, "SCP_"+envSegment(c.Name), []seg{{key: c.Name}})
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, "SCP_") || l.envUsed[k] || reservedEnv[k] || kubeServiceLink.MatchString(k) || l.env[k] == "" {
			continue
		}
		msg := fmt.Sprintf("environment variable %s matches no configuration key", k)
		if strings.Contains(k, ".") {
			msg += " (dotted names are not supported; derive the name from the key path in upper snake case, for example SCP_SERVER_PUBLIC_LISTEN)"
		} else if s := l.suggestEnv(k, root); s != "" {
			msg += "; did you mean " + s + "?"
		}
		l.warnings = append(l.warnings, msg)
	}
}

func (l *loader) envField(f *Field, name string, path []seg) {
	if f.NoEnv {
		return
	}
	switch f.Kind {
	case KindStruct:
		for _, c := range f.Children {
			l.envField(c, name+"_"+envSegment(c.Name), append(append([]seg(nil), path...), seg{key: c.Name}))
		}
	case KindMap:
		// Env can set fields of entries that already exist (from the embedded defaults or the file); it cannot create entries.
		m, _ := getAt(l.tree, path)
		mm, _ := m.(map[string]any)
		keys := make([]string, 0, len(mm))
		for k := range mm {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return len(keys[i]) > len(keys[j]) || (len(keys[i]) == len(keys[j]) && keys[i] < keys[j])
		})
		for _, k := range keys { // longest key first: SCP_X_A_B_F belongs to key "a_b" before key "a"
			l.envField(f.Elem, name+"_"+envSegment(k), append(append([]seg(nil), path...), seg{key: k}))
		}
	case KindList:
		if f.Elem.Kind != KindStruct {
			if v := l.env[name]; v != "" {
				l.envUsed[name] = true
				val, err := coerceList(f, v)
				if err != nil {
					l.viol(joinPath(path), name+": "+err.Error(), "env")
					return
				}
				l.setLeaf(path, val, name)
			}
			return
		}
		for _, i := range l.listIndexes(name + "_") {
			elem := append(append([]seg(nil), path...), seg{idx: i, isI: true})
			for _, c := range f.Elem.Children {
				l.envField(c, name+"_"+strconv.Itoa(i)+"_"+envSegment(c.Name), append(append([]seg(nil), elem...), seg{key: c.Name}))
			}
		}
	case KindSecret:
		lit, file := l.env[name], l.env[name+"_FILE"]
		l.envUsed[name], l.envUsed[name+"_FILE"] = true, true
		switch {
		case lit != "" && file != "":
			l.viol(joinPath(path), fmt.Sprintf("both %s and %s_FILE are set; use one", name, name), "env")
		case file != "":
			l.setLeaf(path, map[string]any{"file": file}, name+"_FILE")
		case lit != "":
			l.setLeaf(path, literalSecret(lit), name)
		}
	default:
		v := l.env[name]
		if v == "" || l.envUsed[name] {
			return
		}
		l.envUsed[name] = true
		val, err := coerce(f, v)
		if err != nil {
			l.viol(joinPath(path), name+": "+err.Error(), "env")
			return
		}
		l.setLeaf(path, val, name)
	}
}

// listIndexes returns the sorted distinct indices i for which a variable starts with prefix+i+"_".
func (l *loader) listIndexes(prefix string) []int {
	seen := map[int]bool{}
	for _, k := range l.envKeys {
		if !strings.HasPrefix(k, prefix) || l.env[k] == "" {
			continue
		}
		num, _, _ := strings.Cut(k[len(prefix):], "_")
		if n, err := strconv.Atoi(num); err == nil && n >= 0 {
			seen[n] = true
		}
	}
	out := make([]int, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// EnvNames lists every settable environment variable template ({i} stands for a list index), for docs and the unknown-variable hint.
func EnvNames(root *Field) []string {
	var out []string
	root.Walk(func(f *Field) {
		if f.Env == "" || f.NoEnv || f.Kind == KindStruct || f.Kind == KindMap || (f.Kind == KindList && f.Elem.Kind == KindStruct) {
			return
		}
		out = append(out, f.Env)
		if f.Kind == KindSecret {
			out = append(out, f.Env+"_FILE")
		}
	})
	sort.Strings(out)
	return out
}

func (l *loader) suggestEnv(k string, root *Field) string {
	best, bd := "", 4
	for _, n := range EnvNames(root) {
		c := strings.ReplaceAll(n, "{i}", "0")
		if d := levenshtein(k, c); d < bd {
			best, bd = n, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
