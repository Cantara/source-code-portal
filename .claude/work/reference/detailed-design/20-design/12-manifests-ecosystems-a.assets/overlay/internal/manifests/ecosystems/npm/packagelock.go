package npm

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type packageLock struct{ base }

func NewPackageLock() manifests.Parser {
	return packageLock{base{id: PackageLockID, match: func(p string) bool {
		return isFile("package-lock.json", "npm-shrinkwrap.json")(p) && !ignoredPath(p)
	}, kind: manifests.KindLockfile}}
}

// lockEntry is one installed package (or workspace member) of a package-lock "packages" map.
type lockEntry struct {
	key, name, version, integrity, license, resolved string
	dev, optional, devOptional, peer, link           bool
	member                                           bool
	declared                                         [4]map[string]string // runtime, dev, optional, peer
	edges                                            []string             // dependency names that create graph edges
	line                                             int
}

// locker is the shared tail of the flattened lock formats (package-lock v1/v2/v3): entries keyed by node_modules path.
type locker struct {
	entries map[string]*lockEntry
	order   []string
}

func (l *locker) add(e *lockEntry) {
	if _, dup := l.entries[e.key]; !dup {
		l.order = append(l.order, e.key)
	}
	l.entries[e.key] = e
}

// parentOf returns the location whose node_modules is searched after from's own.
func parentOf(from string) string {
	i := strings.LastIndex(from, "node_modules/")
	if i < 0 {
		return ""
	}
	if i == 0 || from[i-1] == '/' {
		// strip "node_modules/<name>" (two segments when scoped) at i
		return strings.TrimSuffix(from[:i], "/")
	}
	return ""
}

// resolve implements Node module resolution over the flattened tree.
func (l *locker) resolve(name, from string) *lockEntry {
	for n := 0; n < 512; n++ {
		cand := "node_modules/" + name
		if from != "" {
			cand = from + "/" + cand
		}
		if e, ok := l.entries[cand]; ok {
			if e.link {
				if t, ok := l.entries[e.resolved]; ok {
					return t
				}
				return nil
			}
			return e
		}
		if from == "" {
			return nil
		}
		from = parentOf(from)
	}
	return nil
}

func nameFromKey(key string) string {
	i := strings.LastIndex(key, "node_modules/")
	if i < 0 {
		return ""
	}
	return key[i+len("node_modules/"):]
}

func (p packageLock) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	g := limits.NewGuard(ctx, lim)
	root, err := parseJSON(data, g)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "npm.syntax")
		return out, nil
	}
	if root.kind != 'o' {
		out.Diagnostics = append(out.Diagnostics, diag("npm.type-mismatch", 1, "lockfile must be an object"))
		return out, nil
	}
	lv := 0
	if n := root.get("lockfileVersion"); n != nil {
		lv, _ = strconv.Atoi(n.text())
	}
	setProp(&out, "lockfileVersion", strconv.Itoa(lv))
	if lv > 3 || lv < 1 {
		out.Diagnostics = append(out.Diagnostics, diag("npm.lockfile-version-unsupported", 1, "lockfileVersion %d", lv))
	}
	l := &locker{entries: map[string]*lockEntry{}}
	if pk := root.get("packages"); pk != nil && pk.kind == 'o' {
		for _, key := range pk.keys {
			e := readEntry(key, pk.obj[key])
			l.add(e)
		}
	} else if dp := root.get("dependencies"); dp != nil && dp.kind == 'o' {
		// lockfileVersion 1: nested tree; synthesise the flat form
		l.add(&lockEntry{key: "", member: true})
		flattenV1(l, "", dp, g, &out, f.Path, 0)
	} else if lv != 0 {
		out.Diagnostics = append(out.Diagnostics, diag("npm.lock-entry-invalid", 1, "no packages or dependencies section"))
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	finishLock(l, &out, f.Path, lv >= 2)
	return out, nil
}

func readEntry(key string, n *jnode) *lockEntry {
	e := &lockEntry{key: manifests.CleanText(key, 512), line: n.line}
	e.version = manifests.CleanText(n.get("version").text(), 200)
	e.integrity = manifests.CleanText(n.get("integrity").text(), 200)
	e.resolved = n.get("resolved").text()
	e.link = n.get("link").isTrue()
	e.dev, e.optional, e.devOptional, e.peer = n.get("dev").isTrue(), n.get("optional").isTrue(), n.get("devOptional").isTrue(), n.get("peer").isTrue()
	if l := n.get("license"); l != nil && l.kind == 's' {
		e.license = manifests.CleanText(l.str, 200)
	}
	if !strings.Contains(key, "node_modules/") {
		e.member = true // "" (root) or a workspace member directory
	}
	if nm := n.get("name").text(); nm != "" {
		e.name = nm
	} else if !e.member {
		e.name = nameFromKey(key)
	} else if key != "" {
		e.name = key[strings.LastIndexByte(key, '/')+1:]
	}
	secs := [4]string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"}
	for i, s := range secs {
		d := n.get(s)
		if d == nil || d.kind != 'o' {
			continue
		}
		e.declared[i] = map[string]string{}
		for _, k := range d.keys {
			e.declared[i][k] = d.obj[k].text()
			if i != 1 || e.member { // devDependencies create edges only for the root and members
				if !(i == 3 && n.get("peerDependenciesMeta").get(k).get("optional").isTrue()) {
					e.edges = append(e.edges, k)
				}
			}
		}
	}
	if !e.member || key == "" {
		// nothing more
	}
	return e
}

func flattenV1(l *locker, prefix string, deps *jnode, g *limits.Guard, out *manifests.Parsed, file string, depth int) {
	for _, name := range deps.keys {
		if g.Node() != nil {
			return
		}
		n := deps.obj[name]
		if n.kind != 'o' {
			continue
		}
		key := "node_modules/" + name
		if prefix != "" {
			key = prefix + "/" + key
		}
		e := &lockEntry{key: manifests.CleanText(key, 512), name: name, line: n.line}
		ver := n.get("version").text()
		if strings.HasPrefix(ver, "npm:") { // alias: npm:real@1.2.3
			if i := strings.LastIndexByte(ver, '@'); i > 4 {
				e.name, ver = ver[4:i], ver[i+1:]
			}
		}
		e.version = manifests.CleanText(ver, 200)
		e.integrity = manifests.CleanText(n.get("integrity").text(), 200)
		e.resolved = n.get("resolved").text()
		e.dev, e.optional = n.get("dev").isTrue(), n.get("optional").isTrue()
		if rq := n.get("requires"); rq != nil && rq.kind == 'o' {
			for _, k := range rq.keys {
				e.edges = append(e.edges, k)
			}
		}
		l.add(e)
		if sub := n.get("dependencies"); sub != nil && sub.kind == 'o' && depth < 200 {
			flattenV1(l, key, sub, g, out, file, depth+1)
		}
	}
}

type nodeKey struct {
	id  manifests.ComponentID
	ver string
}

func finishLock(l *locker, out *manifests.Parsed, file string, hasRoot bool) {
	// 1. group installed packages into locked components
	var idxOf = map[string]int{} // entry key -> locked index (pre-sort)
	type acc struct {
		lc    manifests.LockedComponent
		edges map[int]bool
		scope manifests.Scope
	}
	var accs []*acc
	byNode := map[nodeKey]int{}
	for _, k := range l.order {
		e := l.entries[k]
		if e.member || e.link || e.name == "" {
			continue
		}
		id := idOf(e.name)
		nk := nodeKey{id, e.version}
		i, ok := byNode[nk]
		sc := manifests.ScopeRuntime
		switch {
		case e.dev || e.devOptional:
			sc = manifests.ScopeDev
		case e.optional:
			sc = manifests.ScopeOptional
		case e.peer:
			sc = manifests.ScopePeer
		}
		if !ok {
			i = len(accs)
			byNode[nk] = i
			accs = append(accs, &acc{lc: manifests.LockedComponent{ID: id, Display: displayOf(id), Version: e.version, Integrity: e.integrity, Relationship: manifests.RelUnknown, Loc: manifests.Location{Line: e.line}}, edges: map[int]bool{}, scope: sc})
			if e.license != "" {
				accs[i].lc.Licenses = []string{e.license}
			}
		} else {
			accs[i].scope = betterScope(accs[i].scope, sc)
			if accs[i].lc.Integrity == "" {
				accs[i].lc.Integrity = e.integrity
			}
		}
		idxOf[k] = i
	}
	// 2. edges
	for _, k := range l.order {
		e := l.entries[k]
		si, ok := idxOf[k]
		if !ok {
			continue
		}
		for _, d := range e.edges {
			t := l.resolve(d, k)
			if t == nil || t.member {
				continue
			}
			if ti, ok := idxOf[t.key]; ok && ti != si {
				accs[si].edges[ti] = true
			}
		}
	}
	// 3. importers: root and members
	direct := map[int]bool{}
	var members []string
	for _, k := range l.order {
		e := l.entries[k]
		if !e.member {
			continue
		}
		dir := k
		if dir == "" {
			dir = "."
		} else {
			if m, ok := memberOf(k); ok {
				members = append(members, m)
			} else {
				out.Diagnostics = append(out.Diagnostics, diag("npm.member-invalid", 0, "workspace directory is absolute or malformed"))
			}
		}
		for si, sc := range [4]manifests.Scope{manifests.ScopeRuntime, manifests.ScopeDev, manifests.ScopeOptional, manifests.ScopePeer} {
			_ = sc
			for name := range e.declared[si] {
				t := l.resolve(name, k)
				if t == nil || t.member {
					continue
				}
				if ti, ok := idxOf[t.key]; ok {
					direct[ti] = true
					setProp(out, "lock:"+dir+":"+manifests.CleanText(name, 214), accs[ti].lc.Version)
				}
			}
		}
	}
	// 4. deterministic order: id, version
	order := make([]int, len(accs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := accs[order[a]].lc, accs[order[b]].lc
		if x.ID != y.ID {
			return x.ID.String() < y.ID.String()
		}
		return x.Version < y.Version
	})
	newIdx := make([]int, len(accs))
	for ni, oi := range order {
		newIdx[oi] = ni
	}
	for _, oi := range order {
		a := accs[oi]
		lc := a.lc
		lc.Scope = a.scope
		switch {
		case direct[oi]:
			lc.Relationship = manifests.RelDirect
		case hasRoot:
			lc.Relationship = manifests.RelTransitive
		}
		for t := range a.edges {
			lc.DependsOn = append(lc.DependsOn, newIdx[t])
		}
		sort.Ints(lc.DependsOn)
		out.Locked = append(out.Locked, lc)
	}
	sort.Strings(members)
	out.Members = members
}
