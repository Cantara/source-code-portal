package python

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
	"github.com/cantara/source-code-portal/internal/manifests/internal/tomlx"
)

type tomlLock struct {
	lockBase
	parse func(ctx context.Context, p tomlLock, f manifests.File, data []byte, out *manifests.Parsed) error
}

func newLock(id string, match func(string) bool) lockBase {
	return lockBase{base{id: id, match: match, kind: manifests.KindLockfile}}
}

// NewPoetryLock returns the poetry.lock parser.
func NewPoetryLock() manifests.Parser {
	return tomlLock{newLock(PoetryLockID, func(p string) bool { return baseName(p) == "poetry.lock" && !vendoredPath(p) }), parsePoetryLock}
}

// NewUVLock returns the uv.lock parser.
func NewUVLock() manifests.Parser {
	return tomlLock{newLock(UVLockID, func(p string) bool { return baseName(p) == "uv.lock" && !vendoredPath(p) }), parseUVLock}
}

// NewPylock returns the pylock.toml (PEP 751) parser.
func NewPylock() manifests.Parser { return tomlLock{newLock(PylockID, matchPylock), parsePylock} }

func (l tomlLock) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, rej := l.admit(f, lim)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile, Properties: map[string]string{}}
	if err := l.parse(ctx, l, f, data, &out); err != nil {
		if ctx.Err() != nil {
			return manifests.Parsed{}, ctx.Err()
		}
		return out, nil
	}
	tidyEdges(out.Locked)
	if len(out.Properties) == 0 {
		out.Properties = nil
	}
	return out, nil
}

// tidyEdges removes self edges and duplicates and sorts every DependsOn list (hostile locks may declare a package as its own dependency).
func tidyEdges(ls []manifests.LockedComponent) {
	for i := range ls {
		d := ls[i].DependsOn
		if len(d) == 0 {
			continue
		}
		sort.Ints(d)
		out := d[:0]
		for _, e := range d {
			if e == i || (len(out) > 0 && out[len(out)-1] == e) {
				continue
			}
			out = append(out, e)
		}
		if len(out) == 0 {
			out = nil
		}
		ls[i].DependsOn = out
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

type nameIndex struct {
	byKey  map[[2]string]int
	byName map[string][]int
}

func newNameIndex() *nameIndex {
	return &nameIndex{byKey: map[[2]string]int{}, byName: map[string][]int{}}
}
func (n *nameIndex) add(i int, name, ver string) {
	nn := NormalizeName(name)
	n.byKey[[2]string{nn, ver}] = i
	n.byName[nn] = append(n.byName[nn], i)
}
func (n *nameIndex) find(name, ver string) []int {
	nn := NormalizeName(name)
	if ver != "" {
		if i, ok := n.byKey[[2]string{nn, ver}]; ok {
			return []int{i}
		}
	}
	return n.byName[nn]
}

// ---------------------------------------------------------------- poetry.lock

func parsePoetryLock(ctx context.Context, l tomlLock, f manifests.File, data []byte, out *manifests.Parsed) error {
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "python.syntax"), 0, "poetry.lock cannot be parsed: %v", err))
		return err
	}
	if lv, ok := tomlx.Str(tomlx.At(tree, "metadata", "lock-version")); ok {
		out.Properties["lock.version"] = manifests.CleanText(lv, 16)
	}
	if py, ok := tomlx.Str(tomlx.At(tree, "metadata", "python-versions")); ok {
		out.Toolchain = map[string]string{"python": manifests.CleanText(py, 100)}
	}
	pkgs := tomlx.List(tree["package"])
	idx := newNameIndex()
	for i, p := range pkgs {
		m := tomlx.Map(p)
		n, _ := tomlx.Str(m["name"])
		v, _ := tomlx.Str(m["version"])
		lc := manifests.LockedComponent{ID: idOf(n), Display: cleanName(n), Version: manifests.CleanText(v, 128), Loc: manifests.Location{Line: ix.Line("package[" + itoa(i) + "]")}}
		var groups []string
		for _, g := range tomlx.List(m["groups"]) {
			if s, ok := g.(string); ok {
				groups = append(groups, s)
			}
		}
		if cat, ok := tomlx.Str(m["category"]); ok && len(groups) == 0 { // Poetry 1.x
			groups = []string{map[bool]string{true: "main", false: "dev"}[cat == "main"]}
		}
		opt, _ := m["optional"].(bool)
		lc.Scope = poetryScope(groups, opt)
		if src := tomlx.Map(m["source"]); src != nil {
			if rr, ok := tomlx.Str(src["resolved_reference"]); ok {
				lc.Integrity = "git:" + manifests.CleanText(rr, 64)
			}
		}
		if lc.Integrity == "" {
			for _, fe := range tomlx.List(m["files"]) {
				if h, ok := tomlx.Str(tomlx.Map(fe)["hash"]); ok {
					lc.Integrity = manifests.CleanText(h, 120)
					break
				}
			}
		}
		out.Locked = append(out.Locked, lc)
		idx.add(i, n, v)
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	for i, p := range pkgs {
		deps := tomlx.Map(tomlx.Map(p)["dependencies"])
		for _, name := range tomlx.SortedKeys(deps) {
			out.Locked[i].DependsOn = append(out.Locked[i].DependsOn, idx.find(name, "")...)
		}
		out.Locked[i].DependsOn = dedupInts(out.Locked[i].DependsOn)
	}
	return nil
}

func poetryScope(groups []string, optional bool) manifests.Scope {
	sc := manifests.ScopeUnknown
	for _, g := range groups {
		var s manifests.Scope
		if g == "main" {
			s = manifests.ScopeRuntime
			if optional {
				s = manifests.ScopeOptional
			}
		} else {
			s = scopeForGroup(g)
		}
		if sc == manifests.ScopeUnknown || rank(s) < rank(sc) {
			sc = s
		}
	}
	return sc
}

// rank orders scopes by "how needed": runtime < optional < build < test < dev < unknown.
func rank(s manifests.Scope) int {
	switch s {
	case manifests.ScopeRuntime:
		return 0
	case manifests.ScopeOptional:
		return 1
	case manifests.ScopeBuild:
		return 2
	case manifests.ScopeTest:
		return 3
	case manifests.ScopeDev:
		return 4
	}
	return 9
}

func dedupInts(in []int) []int {
	sort.Ints(in)
	out := in[:0]
	for i, v := range in {
		if i == 0 || v != in[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// ---------------------------------------------------------------- uv.lock

func parseUVLock(ctx context.Context, l tomlLock, f manifests.File, data []byte, out *manifests.Parsed) error {
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "python.syntax"), 0, "uv.lock cannot be parsed: %v", err))
		return err
	}
	if v, ok := tree["version"].(int64); ok {
		out.Properties["lock.version"] = itoa(int(v))
	}
	if rp, ok := tomlx.Str(tree["requires-python"]); ok {
		out.Toolchain = map[string]string{"python": manifests.CleanText(rp, 100)}
	}
	pkgs := tomlx.List(tree["package"])
	idx := newNameIndex()
	isRoot := make([]bool, len(pkgs))
	for i, p := range pkgs {
		m := tomlx.Map(p)
		n, _ := tomlx.Str(m["name"])
		v, _ := tomlx.Str(m["version"])
		src := tomlx.Map(m["source"])
		lc := manifests.LockedComponent{ID: idOf(n), Display: cleanName(n), Version: manifests.CleanText(v, 128), Loc: manifests.Location{Line: ix.Line("package[" + itoa(i) + "]")}}
		for _, k := range []string{"editable", "virtual"} {
			if s, ok := tomlx.Str(src[k]); ok {
				isRoot[i] = true
				if rel, ok := repopath.Rel(s); ok {
					out.Properties["root:"+itoa(i)] = rel // relative to the directory of uv.lock
				}
			}
		}
		if g, ok := tomlx.Str(src["git"]); ok {
			if h := strings.LastIndexByte(g, '#'); h >= 0 {
				lc.Integrity = "git:" + manifests.CleanText(g[h+1:], 64)
			}
		}
		if lc.Integrity == "" {
			if h, ok := tomlx.Str(tomlx.Map(m["sdist"])["hash"]); ok {
				lc.Integrity = manifests.CleanText(h, 120)
			} else if ws := tomlx.List(m["wheels"]); len(ws) > 0 {
				if h, ok := tomlx.Str(tomlx.Map(ws[0])["hash"]); ok {
					lc.Integrity = manifests.CleanText(h, 120)
				}
			}
		}
		out.Locked = append(out.Locked, lc)
		idx.add(i, n, v)
	}
	type edge struct {
		to     []int
		extras []string
	}
	edgesOf := func(list []any) []edge {
		var es []edge
		for _, e := range list {
			m := tomlx.Map(e)
			n, _ := tomlx.Str(m["name"])
			v, _ := tomlx.Str(m["version"])
			var ex []string
			for _, x := range tomlx.List(m["extra"]) {
				if s, ok := x.(string); ok {
					ex = append(ex, s)
				}
			}
			if to := idx.find(n, v); len(to) > 0 {
				es = append(es, edge{to, ex})
			}
		}
		return es
	}
	deps := make([][]edge, len(pkgs))
	optional := make([]map[string][]edge, len(pkgs))
	dev := make([]map[string][]edge, len(pkgs))
	for i, p := range pkgs {
		m := tomlx.Map(p)
		deps[i] = edgesOf(tomlx.List(m["dependencies"]))
		optional[i], dev[i] = map[string][]edge{}, map[string][]edge{}
		for k, v := range tomlx.Map(m["optional-dependencies"]) {
			optional[i][k] = edgesOf(tomlx.List(v))
		}
		for k, v := range tomlx.Map(m["dev-dependencies"]) {
			dev[i][k] = edgesOf(tomlx.List(v))
		}
		seen := map[int]bool{}
		add := func(es []edge) {
			for _, e := range es {
				for _, t := range e.to {
					if !seen[t] {
						seen[t] = true
						out.Locked[i].DependsOn = append(out.Locked[i].DependsOn, t)
					}
				}
			}
		}
		add(deps[i])
		for _, k := range tomlx.SortedKeys(tomlx.Map(m["optional-dependencies"])) {
			if isRoot[i] {
				add(optional[i][k])
			}
		}
		for _, k := range tomlx.SortedKeys(tomlx.Map(m["dev-dependencies"])) {
			add(dev[i][k])
		}
		sort.Ints(out.Locked[i].DependsOn)
	}
	// scope and relationship by reachability from every root, strongest scope first
	type seed struct {
		to    int
		scope manifests.Scope
	}
	var seeds []seed
	for i := range pkgs {
		if !isRoot[i] {
			continue
		}
		for _, e := range deps[i] {
			for _, t := range e.to {
				seeds = append(seeds, seed{t, manifests.ScopeRuntime})
			}
		}
		for _, k := range tomlx.SortedKeys(tomlx.Map(tomlx.Map(pkgs[i])["optional-dependencies"])) {
			for _, e := range optional[i][k] {
				for _, t := range e.to {
					seeds = append(seeds, seed{t, manifests.ScopeOptional})
				}
			}
		}
		for _, k := range tomlx.SortedKeys(tomlx.Map(tomlx.Map(pkgs[i])["dev-dependencies"])) {
			for _, e := range dev[i][k] {
				for _, t := range e.to {
					seeds = append(seeds, seed{t, scopeForGroup(k)})
				}
			}
		}
	}
	sort.SliceStable(seeds, func(a, b int) bool { return rank(seeds[a].scope) < rank(seeds[b].scope) })
	for _, s := range seeds {
		if out.Locked[s.to].Relationship == manifests.RelUnknown {
			out.Locked[s.to].Relationship = manifests.RelDirect
		}
	}
	for _, s := range seeds {
		stack := []int{s.to}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if out.Locked[n].Scope != manifests.ScopeUnknown && rank(out.Locked[n].Scope) <= rank(s.scope) {
				continue
			}
			out.Locked[n].Scope = s.scope
			if isRoot[n] {
				continue
			}
			for _, c := range out.Locked[n].DependsOn {
				stack = append(stack, c)
			}
		}
	}
	for i := range out.Locked {
		if isRoot[i] {
			out.Locked[i].Relationship, out.Locked[i].Scope = manifests.RelUnknown, manifests.ScopeUnknown
		} else if out.Locked[i].Relationship == manifests.RelUnknown {
			out.Locked[i].Relationship = manifests.RelTransitive
		}
	}
	return nil
}

// ---------------------------------------------------------------- pylock.toml (PEP 751)

func parsePylock(ctx context.Context, l tomlLock, f manifests.File, data []byte, out *manifests.Parsed) error {
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "python.syntax"), 0, "pylock.toml cannot be parsed: %v", err))
		return err
	}
	if lv, ok := tomlx.Str(tree["lock-version"]); ok {
		out.Properties["lock.version"] = manifests.CleanText(lv, 16)
	}
	if rp, ok := tomlx.Str(tree["requires-python"]); ok {
		out.Toolchain = map[string]string{"python": manifests.CleanText(rp, 100)}
	}
	pkgs := tomlx.List(tree["packages"])
	idx := newNameIndex()
	for i, p := range pkgs {
		m := tomlx.Map(p)
		n, _ := tomlx.Str(m["name"])
		v, _ := tomlx.Str(m["version"])
		lc := manifests.LockedComponent{ID: idOf(n), Display: cleanName(n), Version: manifests.CleanText(v, 128), Scope: manifests.ScopeRuntime, Loc: manifests.Location{Line: ix.Line("packages[" + itoa(i) + "]")}}
		hashOf := func(t map[string]any) string {
			if h, ok := tomlx.Str(tomlx.Map(t["hashes"])["sha256"]); ok {
				return "sha256:" + manifests.CleanText(h, 80)
			}
			return ""
		}
		switch {
		case m["vcs"] != nil:
			if c, ok := tomlx.Str(tomlx.Map(m["vcs"])["commit-id"]); ok {
				lc.Integrity = "git:" + manifests.CleanText(c, 64)
			}
		case m["archive"] != nil:
			lc.Integrity = hashOf(tomlx.Map(m["archive"]))
		case m["sdist"] != nil:
			lc.Integrity = hashOf(tomlx.Map(m["sdist"]))
		default:
			if ws := tomlx.List(m["wheels"]); len(ws) > 0 {
				lc.Integrity = hashOf(tomlx.Map(ws[0]))
			}
		}
		out.Locked = append(out.Locked, lc)
		idx.add(i, n, v)
	}
	for i, p := range pkgs {
		for _, d := range tomlx.List(tomlx.Map(p)["dependencies"]) {
			dm := tomlx.Map(d)
			n, _ := tomlx.Str(dm["name"])
			v, _ := tomlx.Str(dm["version"])
			out.Locked[i].DependsOn = append(out.Locked[i].DependsOn, idx.find(n, v)...)
		}
		out.Locked[i].DependsOn = dedupInts(out.Locked[i].DependsOn)
	}
	return nil
}
