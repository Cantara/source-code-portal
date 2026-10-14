package npm

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type pnpmLock struct{ base }

func NewPnpmLock() manifests.Parser {
	return pnpmLock{base{id: PnpmLockID, match: func(p string) bool { return isFile("pnpm-lock.yaml")(p) && !ignoredPath(p) }, kind: manifests.KindLockfile}}
}

type pnpmWorkspace struct{ base }

func NewPnpmWorkspace() manifests.Parser {
	return pnpmWorkspace{base{id: PnpmWorkspaceID, match: func(p string) bool { return isFile("pnpm-workspace.yaml")(p) && !ignoredPath(p) }, kind: manifests.KindWorkspace}}
}

// pnpmKey splits a package/snapshot key ("/name@1.2.3(peer@1)", "name@1.2.3", "@s/n@1.0.0") into name, version
// (peer suffix kept in rest).
func pnpmKey(k string) (name, ver, suffix string) {
	k = strings.TrimPrefix(k, "/")
	if i := strings.IndexByte(k, '('); i > 0 {
		k, suffix = k[:i], k[i:]
	}
	if i := strings.IndexByte(k[min(1, len(k)):], '@'); i >= 0 {
		i++
		return k[:i], k[i+1:], suffix
	}
	return k, "", suffix
}

// pnpmVersionRef interprets a dependency value of a snapshot or importer: "1.2.3(peer@1)", "link:../x",
// "real@1.2.3(...)" (alias, v9). It returns the snapshot key to look up, the name and the plain version.
func pnpmVersionRef(depName, val string) (key, name, ver string, link bool) {
	if strings.HasPrefix(val, "link:") || strings.HasPrefix(val, "file:") && !strings.Contains(val, "@") {
		return "", depName, val, true
	}
	plain := val
	if i := strings.IndexByte(plain, '('); i > 0 {
		plain = plain[:i]
	}
	if i := strings.IndexByte(plain[min(1, len(plain)):], '@'); i >= 0 && !strings.Contains(plain, "://") && !strings.HasPrefix(plain, "github.com") {
		n, v, _ := pnpmKey(val)
		return val, n, v, false
	}
	return depName + "@" + val, depName, plain, false
}

func (p pnpmLock) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	docs, err := decodeDocs(data)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "npm.syntax")
		return out, nil
	}
	if len(docs) == 0 {
		out.Diagnostics = append(out.Diagnostics, diag("npm.syntax", 0, "empty lockfile"))
		return out, nil
	}
	w := &yw{g: limits.NewGuard(ctx, lim)}
	for _, d := range docs {
		if err := w.measure(d); err != nil {
			fail(&out, err, "npm.syntax")
			return out, nil
		}
	}
	doc := docs[len(docs)-1]
	if len(docs) > 1 {
		out.Diagnostics = append(out.Diagnostics, diag("npm.yaml-multi-document", doc.Line, "%d documents; using the last", len(docs)))
		// environment document: pnpm itself
		if pm := w.get(w.get(w.get(docs[0], "importers"), "."), "packageManagerDependencies"); pm != nil {
			if v := w.str(w.get(w.get(pm, "pnpm"), "version")); v != "" {
				out.Toolchain = map[string]string{"tool": "pnpm", "toolVersion": manifests.CleanText(v, 64)}
			}
		}
	}
	if doc.Kind != yaml.MappingNode {
		out.Diagnostics = append(out.Diagnostics, diag("npm.type-mismatch", doc.Line, "lockfile must be a mapping"))
		return out, nil
	}
	lvs := w.str(w.get(doc, "lockfileVersion"))
	lv, _ := strconv.ParseFloat(lvs, 64)
	setProp(&out, "lockfileVersion", manifests.CleanText(lvs, 20))
	if lv < 6 || lv >= 10 {
		out.Diagnostics = append(out.Diagnostics, diag("npm.lockfile-version-unsupported", doc.Line, "pnpm lockfileVersion %q (supported: 6.0 to 9.x)", lvs))
		if lv < 6 {
			return out, nil
		}
	}
	for _, kv := range w.pairs(w.get(doc, "overrides")) {
		if v := w.str(kv.val); v != "" && len(out.Properties) < maxOverrideProps {
			setProp(&out, "override:"+manifests.CleanText(kv.key, 200), manifests.CleanText(v, 200))
		}
	}
	for _, cat := range w.pairs(w.get(doc, "catalogs")) {
		for _, e := range w.pairs(cat.val) {
			if s := w.str(w.get(e.val, "specifier")); s != "" && len(out.Properties) < 4*maxOverrideProps {
				setProp(&out, "catalog:"+manifests.CleanText(cat.key, 100)+":"+manifests.CleanText(e.key, 214), manifests.CleanText(s, 200))
			}
		}
	}

	// importers; the pre-v7 single-project layout has the sections at top level
	type imp struct {
		dir  string
		node *yaml.Node
		line int
	}
	var imps []imp
	if im := w.get(doc, "importers"); im != nil {
		for _, kv := range w.pairs(im) {
			imps = append(imps, imp{kv.key, kv.val, kv.line})
		}
	} else if w.get(doc, "dependencies") != nil || w.get(doc, "devDependencies") != nil || w.get(doc, "optionalDependencies") != nil {
		imps = append(imps, imp{".", doc, doc.Line})
	}
	snaps := w.get(doc, "snapshots")
	pkgs := w.get(doc, "packages")
	snapPairs := w.pairs(snaps)
	pkgPairs := w.pairs(pkgs)
	if snaps == nil { // v6/v7: packages carry the edges
		snapPairs = pkgPairs
	}
	type node struct {
		id      manifests.ComponentID
		ver     string
		integ   string
		line    int
		edges   map[string]manifests.Scope // target node key -> edge strength (Runtime or Optional)
		optFlag bool
	}
	nodes := map[string]*node{} // key: id.String()+"@"+ver
	nodeOfSnap := map[string]*node{}
	nk := func(id manifests.ComponentID, ver string) string { return id.String() + "@" + ver }
	integrity := map[string]string{} // "name@ver" -> integrity
	for _, kv := range pkgPairs {
		n, v, _ := pnpmKey(kv.key)
		res := w.get(kv.val, "resolution")
		if i := w.str(w.get(res, "integrity")); i != "" {
			integrity[n+"@"+v] = manifests.CleanText(i, 200)
		}
	}
	for _, kv := range snapPairs {
		n, v, _ := pnpmKey(kv.key)
		if n == "" || v == "" {
			out.Diagnostics = append(out.Diagnostics, diag("npm.lock-entry-invalid", kv.line, "unrecognised key %q", manifests.CleanText(kv.key, 80)))
			continue
		}
		id := idOf(n)
		k := nk(id, v)
		nd := nodes[k]
		if nd == nil {
			nd = &node{id: id, ver: manifests.CleanText(v, 200), line: kv.line, edges: map[string]manifests.Scope{}, integ: integrity[n+"@"+v]}
			nodes[k] = nd
		}
		nodeOfSnap[kv.key] = nd
	}
	// edges
	for _, kv := range snapPairs {
		src := nodeOfSnap[kv.key]
		if src == nil {
			continue
		}
		for _, sec := range []struct {
			key string
			sc  manifests.Scope
		}{{"dependencies", manifests.ScopeRuntime}, {"optionalDependencies", manifests.ScopeOptional}} {
			for _, d := range w.pairs(w.get(kv.val, sec.key)) {
				dv := w.str(d.val)
				key, name, ver, link := pnpmVersionRef(d.key, dv)
				if link {
					continue
				}
				_ = key
				t := nk(idOf(name), ver)
				if _, ok := nodes[t]; !ok {
					continue
				}
				if cur, ok := src.edges[t]; !ok || scopeRank(sec.sc) < scopeRank(cur) {
					src.edges[t] = sec.sc
				}
			}
		}
		if w.str(w.get(kv.val, "optional")) == "true" {
			src.optFlag = true
		}
	}
	// roots per importer
	type root struct {
		key string
		sc  manifests.Scope
	}
	var roots []root
	direct := map[string]bool{}
	var members []string
	for _, im := range imps {
		dir := im.dir
		if dir != "." {
			if m, ok := memberOf(dir); ok {
				members = append(members, m)
			} else {
				out.Diagnostics = append(out.Diagnostics, diag("npm.member-invalid", 0, "workspace directory is absolute or malformed"))
			}
		}
		for _, sec := range []struct {
			key string
			sc  manifests.Scope
		}{{"dependencies", manifests.ScopeRuntime}, {"devDependencies", manifests.ScopeDev}, {"optionalDependencies", manifests.ScopeOptional}} {
			for _, d := range w.pairs(w.get(im.node, sec.key)) {
				var val string
				if d.val.Kind == yaml.ScalarNode { // v6 'specifiers' layout
					val = d.val.Value
				} else {
					val = w.str(w.get(d.val, "version"))
				}
				_, name, ver, link := pnpmVersionRef(d.key, val)
				if link {
					continue
				}
				t := nk(idOf(name), ver)
				if _, ok := nodes[t]; !ok {
					continue
				}
				roots = append(roots, root{t, sec.sc})
				direct[t] = true
				setProp(&out, "lock:"+manifests.CleanText(dir, 300)+":"+manifests.CleanText(d.key, 214), manifests.CleanText(ver, 128))
			}
		}
	}
	// scope by relaxation: a node takes the best rank over all paths; optional edges cap the rank at Optional
	rank := map[string]manifests.Scope{}
	var work []string
	relax := func(k string, sc manifests.Scope) {
		if cur, ok := rank[k]; !ok || scopeRank(sc) < scopeRank(cur) {
			rank[k] = sc
			work = append(work, k)
		}
	}
	for _, r := range roots {
		relax(r.key, r.sc)
	}
	for len(work) > 0 {
		if err := w.g.Node(); err != nil {
			fail(&out, err, "npm.syntax")
			return out, nil
		}
		k := work[len(work)-1]
		work = work[:len(work)-1]
		cur := rank[k]
		for t, es := range nodes[k].edges {
			next := cur
			if es == manifests.ScopeOptional && scopeRank(cur) < scopeRank(manifests.ScopeOptional) {
				next = manifests.ScopeOptional
			}
			relax(t, next)
		}
	}
	// materialise
	keys := sortedKeys(nodes)
	sort.SliceStable(keys, func(a, b int) bool {
		x, y := nodes[keys[a]], nodes[keys[b]]
		if x.id != y.id {
			return x.id.String() < y.id.String()
		}
		return x.ver < y.ver
	})
	idx := map[string]int{}
	for i, k := range keys {
		idx[k] = i
	}
	for _, k := range keys {
		nd := nodes[k]
		lc := manifests.LockedComponent{ID: nd.id, Display: displayOf(nd.id), Version: nd.ver, Integrity: nd.integ, Scope: rank[k], Relationship: manifests.RelTransitive, Loc: manifests.Location{Line: nd.line}}
		if _, ok := rank[k]; !ok {
			lc.Scope = manifests.ScopeRuntime // unreachable from any importer; keep, mark nothing
			lc.Relationship = manifests.RelUnknown
		}
		if direct[k] {
			lc.Relationship = manifests.RelDirect
		}
		for t := range nd.edges {
			lc.DependsOn = append(lc.DependsOn, idx[t])
		}
		sort.Ints(lc.DependsOn)
		out.Locked = append(out.Locked, lc)
	}
	sort.Strings(members)
	out.Members = members
	return out, nil
}

func (p pnpmWorkspace) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindWorkspace}
	docs, err := decodeDocs(data)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "npm.syntax")
		return out, nil
	}
	if len(docs) == 0 {
		return out, nil
	}
	w := &yw{g: limits.NewGuard(ctx, lim)}
	doc := docs[0]
	if err := w.measure(doc); err != nil {
		fail(&out, err, "npm.syntax")
		return out, nil
	}
	if doc.Kind != yaml.MappingNode {
		out.Diagnostics = append(out.Diagnostics, diag("npm.type-mismatch", doc.Line, "pnpm-workspace.yaml must be a mapping"))
		return out, nil
	}
	for _, g := range w.strList(w.get(doc, "packages")) {
		if m, ok := memberOf(g); ok {
			out.Members = append(out.Members, m)
		} else {
			out.Diagnostics = append(out.Diagnostics, diag("npm.member-invalid", doc.Line, "workspace pattern is absolute or malformed"))
		}
	}
	for _, kv := range w.pairs(w.get(doc, "catalog")) {
		if v := w.str(kv.val); v != "" {
			setProp(&out, "catalog:default:"+manifests.CleanText(kv.key, 214), manifests.CleanText(v, 200))
		}
	}
	for _, cat := range w.pairs(w.get(doc, "catalogs")) {
		for _, kv := range w.pairs(cat.val) {
			if v := w.str(kv.val); v != "" {
				setProp(&out, "catalog:"+manifests.CleanText(cat.key, 100)+":"+manifests.CleanText(kv.key, 214), manifests.CleanText(v, 200))
			}
		}
	}
	for _, kv := range w.pairs(w.get(doc, "overrides")) {
		if v := w.str(kv.val); v != "" && len(out.Properties) < 4*maxOverrideProps {
			setProp(&out, "override:"+manifests.CleanText(kv.key, 200), manifests.CleanText(v, 200))
		}
	}
	return out, nil
}
