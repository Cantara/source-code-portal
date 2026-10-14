package python

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
	"github.com/cantara/source-code-portal/internal/manifests/internal/tomlx"
)

type pipfile struct{ base }

func NewPipfile() manifests.Parser {
	return pipfile{base{id: PipfileID, match: func(p string) bool { return baseName(p) == "Pipfile" && !vendoredPath(p) }, kind: manifests.KindManifest}}
}

func (p pipfile) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, rej := p.admit(f, lim)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindManifest, Toolchain: map[string]string{"language": "python", "tool": "pipenv"}}
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "python.syntax"), 0, "Pipfile cannot be parsed: %v", err))
		return out, nil
	}
	if v, ok := tomlx.Str(tomlx.At(tree, "requires", "python_version")); ok {
		out.Toolchain["python"] = "==" + manifests.CleanText(v, 20) + ".*"
	} else if v, ok := tomlx.Str(tomlx.At(tree, "requires", "python_full_version")); ok {
		out.Toolchain["python"] = "==" + manifests.CleanText(v, 20)
	}
	cs := &ctxState{out: &out}
	for _, t := range []struct {
		key   string
		scope manifests.Scope
		group string
	}{{"packages", manifests.ScopeRuntime, "packages"}, {"dev-packages", manifests.ScopeDev, "dev-packages"}} {
		tbl := tomlx.Map(tree[t.key])
		for _, name := range ix.KeysBySource(t.key, tbl) {
			d := manifests.Dependency{Target: idOf(name), Scope: t.scope, Group: t.group, Line: int32(ix.Line(t.key + "." + name))}
			d.SetTargetDisplay(cleanName(name))
			spec := ""
			switch x := tbl[name].(type) {
			case string:
				spec = x
			case map[string]any:
				spec, _ = tomlx.Str(x["version"])
				switch {
				case x["git"] != nil:
					g, _ := tomlx.Str(x["git"])
					ref := redactURL(g)
					if r, ok := tomlx.Str(x["ref"]); ok {
						ref += "#" + manifests.CleanText(r, 100)
					}
					d.SetSource(manifests.SourceGit, ref)
				case x["path"] != nil:
					pth, _ := tomlx.Str(x["path"])
					rel, _ := repopath.Rel(pth) // relative to the Pipfile; "" when absolute
					d.SetSource(manifests.SourcePath, rel)
				case x["file"] != nil:
					u, _ := tomlx.Str(x["file"])
					d.SetSource(manifests.SourceURL, redactURL(u))
				}
				if m, ok := tomlx.Str(x["markers"]); ok {
					d.SetCondition(manifests.CleanText(m, 300))
				}
			default:
				cs.out.Diagnostics = append(cs.out.Diagnostics, diag("python.type-mismatch", int(d.Line), "package %s has an unsupported value type", manifests.CleanText(name, 60)))
			}
			if spec != "*" {
				d.Requirement = manifests.CleanText(spec, 200)
			}
			if v, ok := ExactPin(d.Requirement); ok {
				d.Version, d.Resolution = v, manifests.ResPinned
			}
			out.Dependencies = append(out.Dependencies, d)
		}
	}
	return out, nil
}

type pipfileLock struct{ lockBase }

func NewPipfileLock() manifests.Parser {
	return pipfileLock{newLock(PipfileLockID, func(p string) bool { return baseName(p) == "Pipfile.lock" && !vendoredPath(p) })}
}

func (p pipfileLock) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, rej := p.admit(f, lim)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	var doc struct {
		Meta struct {
			Requires map[string]string `json:"requires"`
		} `json:"_meta"`
		Default map[string]pipEntry `json:"default"`
		Develop map[string]pipEntry `json:"develop"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "python.syntax"), 0, "Pipfile.lock cannot be parsed: %v", err))
		return out, nil
	}
	if pv := doc.Meta.Requires["python_version"]; pv != "" {
		out.Toolchain = map[string]string{"python": "==" + manifests.CleanText(pv, 20) + ".*"}
	}
	for _, sec := range []struct {
		m     map[string]pipEntry
		scope manifests.Scope
	}{{doc.Default, manifests.ScopeRuntime}, {doc.Develop, manifests.ScopeDev}} {
		names := make([]string, 0, len(sec.m))
		for n := range sec.m {
			names = append(names, n)
		}
		sortStrings(names)
		for _, n := range names {
			e := sec.m[n]
			lc := manifests.LockedComponent{ID: idOf(n), Display: cleanName(n), Version: manifests.CleanText(strings.TrimPrefix(e.Version, "=="), 128), Scope: sec.scope, Relationship: manifests.RelUnknown}
			if len(e.Hashes) > 0 {
				lc.Integrity = manifests.CleanText(e.Hashes[0], 120)
			}
			if e.Git != "" && e.Ref != "" {
				lc.Integrity = "git:" + manifests.CleanText(e.Ref, 64)
			}
			out.Locked = append(out.Locked, lc)
		}
	}
	return out, nil
}

type pipEntry struct {
	Version string   `json:"version"`
	Hashes  []string `json:"hashes"`
	Git     string   `json:"git"`
	Ref     string   `json:"ref"`
}
