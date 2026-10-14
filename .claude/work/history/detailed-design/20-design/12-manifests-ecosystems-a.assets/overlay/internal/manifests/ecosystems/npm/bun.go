package npm

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type bunLock struct{ base }

func NewBunLock() manifests.Parser {
	return bunLock{base{id: BunLockID, match: func(p string) bool { return isFile("bun.lock")(p) && !ignoredPath(p) }, kind: manifests.KindLockfile}}
}

type bunBinary struct{ base }

func NewBunBinary() manifests.Parser {
	return bunBinary{base{id: BunBinaryID, match: func(p string) bool { return isFile("bun.lockb")(p) && !ignoredPath(p) }, kind: manifests.KindLockfile}}
}

// Parse for bun.lockb never reads the content: the format is binary and undocumented.
func (b bunBinary) Parse(_ context.Context, f manifests.File, _ manifests.Limits) (manifests.Parsed, error) {
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	out.Diagnostics = []manifests.Diagnostic{diag("npm.bun-binary-lockfile", 0, "binary lockfile is not parsed; run bun install to produce bun.lock")}
	return out, nil
}

func (p bunLock) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	g := limits.NewGuard(ctx, lim)
	root, err := parseJSON(stripJSONC(data), g)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "npm.syntax")
		return out, nil
	}
	if root.kind != 'o' {
		out.Diagnostics = append(out.Diagnostics, diag("npm.type-mismatch", 1, "bun.lock must be an object"))
		return out, nil
	}
	lv := root.get("lockfileVersion").text()
	setProp(&out, "lockfileVersion", manifests.CleanText(lv, 10))
	if n, _ := strconv.Atoi(lv); n > 1 || n < 0 {
		out.Diagnostics = append(out.Diagnostics, diag("npm.lockfile-version-unsupported", 1, "bun lockfileVersion %s", manifests.CleanText(lv, 10)))
	}
	for _, k := range keysOf(root.get("overrides")) {
		if v := root.get("overrides").get(k).text(); v != "" && len(out.Properties) < maxOverrideProps {
			setProp(&out, "override:"+manifests.CleanText(k, 200), manifests.CleanText(v, 200))
		}
	}
	for _, h := range []*jnode{root, root.get("workspaces")} {
		readCatalogs(h, &out)
	}
	type pk struct {
		key, name, ver string
		integ          string
		line           int
		deps           map[string]bool // dep names (dependencies, optionalDependencies, peerDependencies that are installed)
		member         bool
	}
	pkgs := map[string]*pk{}
	var order []string
	if pn := root.get("packages"); pn != nil && pn.kind == 'o' {
		for _, key := range pn.keys {
			arr := pn.obj[key]
			if arr.kind != 'a' || len(arr.arr) == 0 || arr.arr[0].kind != 's' {
				out.Diagnostics = append(out.Diagnostics, diag("npm.lock-entry-invalid", arr.line, "package %q is not a tuple", manifests.CleanText(key, 80)))
				continue
			}
			name, ver := splitDescriptor(arr.arr[0].str)
			e := &pk{key: key, name: name, ver: manifests.CleanText(ver, 200), line: arr.line, deps: map[string]bool{}}
			if strings.HasPrefix(ver, "workspace:") {
				e.member = true
			}
			for _, el := range arr.arr[1:] {
				if el.kind == 'o' {
					for _, sec := range []string{"dependencies", "optionalDependencies", "peerDependencies"} {
						for _, d := range keysOf(el.get(sec)) {
							e.deps[d] = true
						}
					}
				}
				if el.kind == 's' && strings.HasPrefix(el.str, "sha") {
					e.integ = manifests.CleanText(el.str, 200)
				}
			}
			pkgs[key] = e
			order = append(order, key)
		}
	}
	// resolve(name, from): bun keys are install paths: "name" or "parent/name" or "parent/child/name".
	resolve := func(name, from string) *pk {
		for {
			cand := name
			if from != "" {
				cand = from + "/" + name
			}
			if e, ok := pkgs[cand]; ok {
				return e
			}
			if from == "" {
				return nil
			}
			// strip the last package segment (one or two path segments if scoped)
			if i := strings.LastIndexByte(from, '/'); i >= 0 {
				if j := strings.LastIndexByte(from[:i], '/'); j >= 0 && strings.HasPrefix(from[j+1:], "@") {
					from = from[:j]
				} else if strings.HasPrefix(from, "@") && j < 0 {
					from = ""
				} else {
					from = from[:i]
				}
			} else {
				from = ""
			}
		}
	}
	type nk struct {
		id  manifests.ComponentID
		ver string
	}
	idx := map[string]int{}
	byNode := map[nk]int{}
	type acc struct {
		lc    manifests.LockedComponent
		edges map[int]bool
		scope manifests.Scope
	}
	var accs []*acc
	for _, key := range order {
		e := pkgs[key]
		if e.member || e.name == "" {
			continue
		}
		id := idOf(e.name)
		k := nk{id, e.ver}
		i, ok := byNode[k]
		if !ok {
			i = len(accs)
			byNode[k] = i
			accs = append(accs, &acc{lc: manifests.LockedComponent{ID: id, Display: displayOf(id), Version: e.ver, Integrity: e.integ, Relationship: manifests.RelTransitive, Loc: manifests.Location{Line: e.line}}, edges: map[int]bool{}, scope: manifests.ScopeUnknown})
		}
		idx[key] = i
	}
	for _, key := range order {
		si, ok := idx[key]
		if !ok {
			continue
		}
		for d := range pkgs[key].deps {
			if t := resolve(d, key); t != nil {
				if ti, ok := idx[t.key]; ok && ti != si {
					accs[si].edges[ti] = true
				}
			}
		}
	}
	// roots: workspaces
	rank := func(i int, sc manifests.Scope) bool {
		if accs[i].scope == manifests.ScopeUnknown || scopeRank(sc) < scopeRank(accs[i].scope) {
			accs[i].scope = sc
			return true
		}
		return false
	}
	var work []int
	var members []string
	if ws := root.get("workspaces"); ws != nil && ws.kind == 'o' {
		for _, dir := range ws.keys {
			wn := ws.obj[dir]
			if wn.kind != 'o' {
				continue
			}
			d := dir
			if d == "" {
				d = "."
			} else {
				if m, ok := memberOf(dir); ok {
					members = append(members, m)
				} else {
					out.Diagnostics = append(out.Diagnostics, diag("npm.member-invalid", 0, "workspace directory is absolute or malformed"))
				}
			}
			for _, sec := range []struct {
				k  string
				sc manifests.Scope
			}{{"dependencies", manifests.ScopeRuntime}, {"devDependencies", manifests.ScopeDev}, {"optionalDependencies", manifests.ScopeOptional}, {"peerDependencies", manifests.ScopePeer}} {
				for _, name := range keysOf(wn.get(sec.k)) {
					t := resolve(name, wn.get("name").text()) // "<workspace name>/<dep>" when the workspace needs its own copy, else the hoisted key
					if t == nil || t.member {
						continue
					}
					ti, ok := idx[t.key]
					if !ok {
						continue
					}
					accs[ti].lc.Relationship = manifests.RelDirect
					setProp(&out, "lock:"+manifests.CleanText(d, 300)+":"+manifests.CleanText(name, 214), accs[ti].lc.Version)
					if rank(ti, sec.sc) {
						work = append(work, ti)
					}
				}
			}
		}
	}
	for len(work) > 0 {
		if err := g.Node(); err != nil {
			fail(&out, err, "npm.syntax")
			return out, nil
		}
		i := work[len(work)-1]
		work = work[:len(work)-1]
		for t := range accs[i].edges {
			sc := accs[i].scope
			if sc == manifests.ScopePeer {
				sc = manifests.ScopeRuntime
			}
			if rank(t, sc) {
				work = append(work, t)
			}
		}
	}
	ord := make([]int, len(accs))
	for i := range ord {
		ord[i] = i
	}
	sort.SliceStable(ord, func(a, b int) bool {
		x, y := accs[ord[a]].lc, accs[ord[b]].lc
		if x.ID != y.ID {
			return x.ID.String() < y.ID.String()
		}
		return x.Version < y.Version
	})
	newIdx := make([]int, len(accs))
	for ni, oi := range ord {
		newIdx[oi] = ni
	}
	for _, oi := range ord {
		a := accs[oi]
		lc := a.lc
		lc.Scope = a.scope
		if lc.Scope == manifests.ScopeUnknown {
			lc.Scope, lc.Relationship = manifests.ScopeRuntime, manifests.RelUnknown
		}
		for t := range a.edges {
			lc.DependsOn = append(lc.DependsOn, newIdx[t])
		}
		sort.Ints(lc.DependsOn)
		out.Locked = append(out.Locked, lc)
	}
	sort.Strings(members)
	out.Members = members
	return out, nil
}

func keysOf(n *jnode) []string {
	if n == nil || n.kind != 'o' {
		return nil
	}
	return n.keys
}
