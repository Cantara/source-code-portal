package manifests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/mod/modfile"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

const (
	maxDependencies = 1000 // per file
	maxName         = 256  // runes
	maxVersion      = 128  // runes
	maxMessage      = 200  // runes
)

func name(s string) string    { return untrusted.Text(s, maxName) }
func version(s string) string { return untrusted.Text(s, maxVersion) }

func problem(pointer, code, format string, args ...any) snapshot.Problem {
	return snapshot.Problem{Pointer: pointer, Code: code, Message: untrusted.Text(fmt.Sprintf(format, args...), maxMessage)}
}

// deps collects dependencies up to the bound, skipping empty names and repeats of a name in the same scope.
type deps struct {
	out  []snapshot.Dependency
	seen map[[2]string]bool
	cut  bool
}

func (d *deps) add(n, v, scope string) {
	n, v = name(n), version(v)
	if n == "" || d.seen[[2]string{n, scope}] {
		return
	}
	if len(d.out) == maxDependencies {
		d.cut = true
		return
	}
	if d.seen == nil {
		d.seen = map[[2]string]bool{}
	}
	d.seen[[2]string{n, scope}] = true
	d.out = append(d.out, snapshot.Dependency{Name: n, Version: v, Scope: scope})
}

func (d *deps) finish(m *snapshot.Manifest) {
	m.Dependencies = d.out
	if d.cut {
		m.Problems = append(m.Problems, problem("", "too-many-dependencies", "only the first %d dependencies are kept", maxDependencies))
	}
}

// parseGoMod reads a go.mod file.
func parseGoMod(b []byte) snapshot.Manifest {
	m := snapshot.Manifest{Ecosystem: "go", Language: "Go", BuildTool: "Go modules"}
	if !utf8.Valid(b) {
		m.Problems = append(m.Problems, problem("", "encoding", "the file is not UTF-8"))
		return m
	}
	f, err := modfile.Parse("go.mod", b, nil)
	if err != nil {
		line := ""
		var list modfile.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			line = fmt.Sprintf("/line/%d", list[0].Pos.Line)
		}
		m.Problems = append(m.Problems, problem(line, "syntax", "%s", firstError(err)))
		// Directives newer than this parser fail the strict parse; the lax one still reads module, go and require.
		if f, err = modfile.ParseLax("go.mod", b, nil); err != nil {
			return m
		}
	}
	if f.Module != nil {
		m.Name = name(f.Module.Mod.Path)
	}
	if f.Go != nil {
		m.Toolchain = append(m.Toolchain, snapshot.Toolchain{Name: "go", Version: version(f.Go.Version)})
	}
	if f.Toolchain != nil && f.Toolchain.Name != "" {
		if v := version(strings.TrimPrefix(f.Toolchain.Name, "go")); f.Go == nil || v != f.Go.Version {
			m.Toolchain = append(m.Toolchain, snapshot.Toolchain{Name: "go", Version: v})
		}
	}
	var d deps
	for _, r := range f.Require {
		scope := "runtime"
		if r.Indirect {
			scope = "indirect"
		}
		d.add(r.Mod.Path, r.Mod.Version, scope)
	}
	for _, t := range f.Tool {
		d.add(t.Path, "", "tool")
	}
	d.finish(&m)
	return m
}

// firstError is the first line of err without the file name prefix.
func firstError(err error) string {
	s := untrusted.FirstLine(err.Error())
	if _, rest, ok := strings.Cut(s, ": "); ok && strings.HasPrefix(s, "go.mod:") {
		s = rest
	}
	return s
}

// lockfiles name the package manager by the lockfile beside package.json, in this order.
var lockfiles = []struct{ file, tool string }{ //nolint:gochecknoglobals // fixed table
	{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"package-lock.json", "npm"}}

// npmScopes are the dependency fields of package.json and their scopes.
var npmScopes = []struct{ field, scope string }{ //nolint:gochecknoglobals // fixed table
	{"dependencies", "runtime"}, {"peerDependencies", "peer"}, {"optionalDependencies", "optional"}, {"devDependencies", "dev"}}

// parsePackageJSON reads a package.json; beside reports whether a file sits in the same directory.
func parsePackageJSON(b []byte, beside func(name string) bool) snapshot.Manifest {
	m := snapshot.Manifest{Ecosystem: "npm", Language: "JavaScript", BuildTool: "npm"}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		m.Problems = append(m.Problems, problem("", "syntax", "%s", untrusted.FirstLine(err.Error())))
		return m
	}
	str := func(field string) string {
		var s string
		if raw, ok := top[field]; ok && json.Unmarshal(raw, &s) != nil {
			m.Problems = append(m.Problems, problem("/"+field, "invalid", "%s is not a string", field))
		}
		return s
	}
	obj := func(field string) map[string]string {
		raw, ok := top[field]
		if !ok {
			return nil
		}
		var vals map[string]json.RawMessage
		if json.Unmarshal(raw, &vals) != nil {
			m.Problems = append(m.Problems, problem("/"+field, "invalid", "%s is not an object", field))
			return nil
		}
		out := make(map[string]string, len(vals))
		for k, v := range vals {
			var s string
			if json.Unmarshal(v, &s) == nil {
				out[k] = s
			}
		}
		return out
	}
	m.Name, m.Version = name(str("name")), version(str("version"))
	if manager := str("packageManager"); manager != "" {
		tool, _, _ := strings.Cut(manager, "@")
		if slices.Contains([]string{"npm", "pnpm", "yarn", "bun"}, tool) {
			m.BuildTool = tool
		}
	} else {
		for _, l := range lockfiles {
			if beside(l.file) {
				m.BuildTool = l.tool
				break
			}
		}
	}
	if node := obj("engines")["node"]; node != "" {
		m.Toolchain = append(m.Toolchain, snapshot.Toolchain{Name: "node", Version: version(node)})
	}
	var d deps
	for _, s := range npmScopes {
		vals := obj(s.field)
		for _, k := range slices.Sorted(maps.Keys(vals)) {
			d.add(k, vals[k], s.scope)
			if k == "typescript" {
				m.Language = "TypeScript"
			}
		}
	}
	if beside("tsconfig.json") {
		m.Language = "TypeScript"
	}
	d.finish(&m)
	return m
}
