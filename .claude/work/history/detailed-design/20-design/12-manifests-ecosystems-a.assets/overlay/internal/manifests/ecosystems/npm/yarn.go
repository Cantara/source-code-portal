package npm

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type yarnLock struct{ base }

func NewYarnLock() manifests.Parser {
	return yarnLock{base{id: YarnLockID, match: func(p string) bool { return isFile("yarn.lock")(p) && !ignoredPath(p) }, kind: manifests.KindLockfile}}
}

// yarnEntry is one lockfile record before resolution of edges.
type yarnEntry struct {
	descs     []string
	name, ver string
	integrity string
	line      int
	deps      []yarnDep
	local     bool // workspace, portal, link, file: first-party or path, not a locked component
}

type yarnDep struct {
	name, rng string
	optional  bool
}

// splitDescriptor splits "name@range" at the first '@' after index 0.
func splitDescriptor(d string) (name, rng string) {
	if i := strings.IndexByte(d[min(1, len(d)):], '@'); i >= 0 {
		i++
		return d[:i], d[i+1:]
	}
	return d, ""
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return strings.Trim(s, `"`)
	}
	return s
}

// splitKeys splits a lockfile header on top-level commas outside quotes.
func splitKeys(h string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c == '"':
			inQ = !inQ
			cur.WriteByte(c)
		case c == ',' && !inQ:
			out = append(out, unquote(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, unquote(s))
	}
	return out
}

func (p yarnLock) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	g := limits.NewGuard(ctx, lim)
	head := data
	if len(head) > 2048 {
		head = head[:2048]
	}
	var entries []yarnEntry
	switch {
	case bytes.Contains(head, []byte("__metadata:")):
		var err error
		entries, err = parseBerry(data, &out, f.Path, g)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			fail(&out, err, "npm.syntax")
			return out, nil
		}
	case bytes.Contains(head, []byte("yarn lockfile v1")), bytes.Contains(head, []byte("yarn.lock")), !bytes.Contains(head, []byte("__metadata")):
		setProp(&out, "lockfileVersion", "1")
		var err error
		entries, err = parseYarn1(data, f.Path, g, &out)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			fail(&out, err, "npm.syntax")
			return out, nil
		}
	default:
		out.Diagnostics = append(out.Diagnostics, diag("npm.yarn-version-unsupported", 1, "unrecognised yarn.lock header"))
		return out, nil
	}
	finishYarn(entries, &out, f.Path)
	return out, nil
}

func parseYarn1(data []byte, file string, g *limits.Guard, out *manifests.Parsed) ([]yarnEntry, error) {
	var entries []yarnEntry
	var cur *yarnEntry
	section := ""
	line := 0
	for len(data) > 0 {
		var ln []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			ln, data = data[:i], data[i+1:]
		} else {
			ln, data = data, nil
		}
		line++
		if err := g.Node(); err != nil {
			return nil, err
		}
		s := strings.TrimRight(string(ln), "\r \t")
		if s == "" || strings.HasPrefix(strings.TrimSpace(s), "#") {
			continue
		}
		indent := len(s) - len(strings.TrimLeft(s, " "))
		t := strings.TrimSpace(s)
		switch {
		case indent == 0:
			if !strings.HasSuffix(t, ":") {
				out.Diagnostics = append(out.Diagnostics, diag("npm.lock-entry-invalid", line, "expected an entry header"))
				cur = nil
				continue
			}
			entries = append(entries, yarnEntry{descs: splitKeys(strings.TrimSuffix(t, ":")), line: line})
			cur = &entries[len(entries)-1]
			section = ""
		case cur == nil:
		case indent == 2:
			k, v, hasV := strings.Cut(t, " ")
			section = ""
			if !hasV {
				section = strings.TrimSuffix(k, ":")
				continue
			}
			switch k {
			case "version":
				cur.ver = manifests.CleanText(unquote(v), 200)
			case "integrity":
				cur.integrity = manifests.CleanText(unquote(v), 200)
			}
		case indent == 4 && (section == "dependencies" || section == "optionalDependencies"):
			k, v, ok := cutSpace(t)
			if !ok {
				continue
			}
			cur.deps = append(cur.deps, yarnDep{name: unquote(k), rng: unquote(v), optional: section == "optionalDependencies"})
		}
	}
	// name of an entry: from its first descriptor (alias descriptors name the alias, resolved via the resolved URL is not
	// parsed; v1 aliases keep the descriptor name).
	for i := range entries {
		e := &entries[i]
		if len(e.descs) > 0 {
			e.name, _ = splitDescriptor(e.descs[0])
		}
		for _, d := range e.descs {
			if _, r := splitDescriptor(d); strings.HasPrefix(r, "npm:") {
				// alias: the real name is in the range
				if rest := r[4:]; strings.LastIndexByte(rest, '@') > 0 {
					e.name = rest[:strings.LastIndexByte(rest, '@')]
				}
			}
		}
	}
	return entries, nil
}

func cutSpace(t string) (string, string, bool) {
	if strings.HasPrefix(t, `"`) { // quoted name
		end := strings.Index(t[1:], `"`)
		if end < 0 {
			return "", "", false
		}
		end += 2
		if end >= len(t) {
			return "", "", false
		}
		return t[:end], strings.TrimSpace(t[end:]), true
	}
	k, v, ok := strings.Cut(t, " ")
	return k, strings.TrimSpace(v), ok
}

func parseBerry(data []byte, out *manifests.Parsed, file string, g *limits.Guard) ([]yarnEntry, error) {
	docs, err := decodeDocs(data)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 || docs[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	w := &yw{g: g}
	if err := w.measure(docs[0]); err != nil {
		return nil, err
	}
	var entries []yarnEntry
	for _, kv := range w.pairs(docs[0]) {
		if kv.key == "__metadata" {
			setProp(out, "lockfileVersion", manifests.CleanText(w.str(w.get(kv.val, "version")), 10))
			continue
		}
		e := yarnEntry{descs: splitKeys(kv.key), line: kv.line}
		res := w.str(w.get(kv.val, "resolution"))
		e.ver = manifests.CleanText(w.str(w.get(kv.val, "version")), 200)
		name, rng := splitDescriptor(res)
		e.name = name
		if name == "" && len(e.descs) > 0 {
			e.name, _ = splitDescriptor(e.descs[0])
		}
		for _, pfx := range []string{"workspace:", "portal:", "link:", "file:", "exec:"} {
			if strings.HasPrefix(rng, pfx) {
				e.local = true
			}
		}
		if w.str(w.get(kv.val, "languageName")) == "unknown" {
			e.local = true
		}
		meta := w.get(kv.val, "dependenciesMeta")
		for _, d := range w.pairs(w.get(kv.val, "dependencies")) {
			opt := w.str(w.get(w.get(meta, d.key), "optional")) == "true"
			e.deps = append(e.deps, yarnDep{name: d.key, rng: w.str(d.val), optional: opt})
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func finishYarn(entries []yarnEntry, out *manifests.Parsed, file string) {
	type nk struct {
		id  manifests.ComponentID
		ver string
	}
	byNode := map[nk]int{}
	type acc struct {
		lc    manifests.LockedComponent
		edges map[int]bool
		deps  []yarnDep
	}
	var accs []*acc
	descTo := map[string]int{}     // descriptor -> acc index
	localDesc := map[string]bool{} // descriptors of first-party entries
	for _, e := range entries {
		if e.local {
			for _, d := range e.descs {
				localDesc[d] = true
			}
			continue
		}
		if e.name == "" || e.ver == "" {
			out.Diagnostics = append(out.Diagnostics, diag("npm.lock-entry-invalid", e.line, "entry without name or version"))
			continue
		}
		id := idOf(e.name)
		k := nk{id, e.ver}
		i, ok := byNode[k]
		if !ok {
			i = len(accs)
			byNode[k] = i
			accs = append(accs, &acc{lc: manifests.LockedComponent{ID: id, Display: displayOf(id), Version: e.ver, Integrity: e.integrity, Loc: manifests.Location{Line: e.line}}, edges: map[int]bool{}})
		}
		accs[i].deps = append(accs[i].deps, e.deps...)
		for _, d := range e.descs {
			descTo[d] = i
		}
	}
	for _, a := range accs {
		for _, d := range a.deps {
			for _, cand := range []string{d.name + "@" + d.rng, d.name + "@npm:" + d.rng} {
				if t, ok := descTo[cand]; ok {
					a.edges[t] = true
					break
				}
			}
		}
	}
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
		for t := range a.edges {
			lc.DependsOn = append(lc.DependsOn, newIdx[t])
		}
		sort.Ints(lc.DependsOn)
		out.Locked = append(out.Locked, lc)
	}
	// descriptor index for the assembler: "desc:<descriptor>" = version, or "real@version" for aliases
	// Sorted iteration: two descriptors can clean to the same property key and the 200,000 cap must keep the same subset on every run
	// (found by FuzzParsers: map order leaked into the output).
	descs := make([]string, 0, len(descTo))
	for d := range descTo {
		descs = append(descs, d)
	}
	sort.Strings(descs)
	for _, d := range descs {
		lc := accs[descTo[d]].lc
		n, _ := splitDescriptor(d)
		v := lc.Version
		if n != displayOf(lc.ID) {
			v = displayOf(lc.ID) + "@" + lc.Version
		}
		if len(out.Properties) < 200000 {
			k := "desc:" + manifests.CleanText(d, 300)
			if _, dup := out.Properties[k]; !dup {
				setProp(out, k, manifests.CleanText(v, 300))
			}
		}
	}
	locals := make([]string, 0, len(localDesc))
	for d := range localDesc {
		locals = append(locals, d)
	}
	sort.Strings(locals)
	for _, d := range locals {
		if len(out.Properties) < 200000 {
			k := "desc:" + manifests.CleanText(d, 300)
			if _, dup := out.Properties[k]; !dup {
				setProp(out, k, "workspace")
			}
		}
	}
}
