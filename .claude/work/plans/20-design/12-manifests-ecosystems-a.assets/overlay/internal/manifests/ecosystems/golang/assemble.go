package golang

import (
	"context"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

type assembler struct{}

// NewAssembler returns the Go assembler.
func NewAssembler() manifests.Assembler { return assembler{} }

func (assembler) Ecosystem() manifests.EcosystemID { return manifests.Golang }

// AssemblerVersion takes part in Registry.SetHash: bump it on ANY change of the units this assembler builds (cached facts are then rebuilt).
func (assembler) AssemblerVersion() int { return assemblerVer }

const assemblerVer = 1

// Assemble makes one unit per go.mod. A go.work attaches to every module it lists (use directives resolved against its own directory)
// and contributes its go version, its replaces (which override go.mod replaces) and workspace-local resolution of modules produced
// by another member (the workspace use overrides the registry).
func (assembler) Assemble(ctx context.Context, files manifests.FileSet, parsed []manifests.Parsed) ([]manifests.Unit, error) {
	var mods, works []manifests.Parsed
	for _, p := range parsed {
		switch p.File.ParserID {
		case ModParserID:
			mods = append(mods, p)
		case WorkParserID:
			works = append(works, p)
		}
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].File.Path < mods[j].File.Path })
	sort.Slice(works, func(i, j int) bool { return works[i].File.Path < works[j].File.Path })

	moduleDirs := map[string]int{} // dir -> index in mods
	producer := map[manifests.ComponentID]string{}
	for i, m := range mods {
		moduleDirs[repopath.Dir(m.File.Path)] = i
		for _, a := range m.Provides {
			if _, dup := producer[a.ID]; !dup {
				producer[a.ID] = repopath.Dir(m.File.Path)
			}
		}
	}
	type wsInfo struct {
		file     manifests.Parsed
		replaces map[string]string // go.work replaces with local paths already joined to repo-relative form
	}
	memberOf := map[string]*wsInfo{}
	diags := map[string][]manifests.Diagnostic{}
	for _, w := range works {
		wdir := repopath.Dir(w.File.Path)
		wi := &wsInfo{file: w, replaces: map[string]string{}}
		for k, v := range w.Properties {
			if rest, ok := strings.CutPrefix(v, "path:"); ok {
				if jp, ok := repopath.Join(wdir, rest); ok {
					v = "path:" + jp
				} else {
					continue // escapes the repository: not followed
				}
			}
			wi.replaces[k] = v
		}
		for _, rel := range w.Members {
			u, ok := repopath.Join(wdir, rel)
			if !ok {
				diags[wdir] = append(diags[wdir], diagAt("golang.use-missing", w.File.Path, 0, "go.work uses %s which is outside the repository", rel))
				continue
			}
			if _, ok := moduleDirs[u]; !ok {
				diags[wdir] = append(diags[wdir], diagAt("golang.use-missing", w.File.Path, 0, "go.work uses %s but no go.mod was found there", rel))
				continue
			}
			if _, taken := memberOf[u]; !taken {
				memberOf[u] = wi
			}
		}
	}

	units := make([]manifests.Unit, 0, len(mods))
	for _, m := range mods {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir := repopath.Dir(m.File.Path)
		u := manifests.Unit{Ecosystem: manifests.Golang, Dir: dirOrDot(dir), Files: []manifests.FileRef{m.File},
			Provides: slices.Clone(m.Provides), Locked: slices.Clone(m.Locked), Toolchain: cloneMap(m.Toolchain), Diagnostics: append([]manifests.Diagnostic(nil), m.Diagnostics...)}
		ws := memberOf[dir]
		if ws != nil {
			u.Files = append(u.Files, ws.file.File)
			u.Toolchain["workspace"] = ws.file.File.Path
			if g := ws.file.Toolchain["go"]; g != "" {
				u.Toolchain["workspace.go"] = g
			}
		}
		for _, d := range m.Dependencies {
			d = d.Clone()
			if ws != nil && d.Source != manifests.SourcePath {
				if v, ok := lookupReplace(ws.replaces, d.TargetDisplay(), d.Version); ok { // go.work replaces override go.mod
					if rest, isPath := strings.CutPrefix(v, "path:"); isPath {
						d.SetSource(manifests.SourcePath, rest)
					} else {
						d.SetSource(d.Source, v)
					}
				} else if pd, ok := producer[d.Target]; ok && pd != dir {
					if memberOf[pd] == ws { // workspace use overrides the registry
						d.SetSource(manifests.SourcePath, dirOrDot(pd))
					}
				}
			}
			u.Dependencies = append(u.Dependencies, d)
		}
		u.Diagnostics = append(u.Diagnostics, diags[dir]...)
		units = append(units, u)
	}
	if len(units) == 0 {
		return nil, nil // nothing placed (for example a go.work without go.mod files): the root's default assembly shows the files
	}
	return units, nil
}

func dirOrDot(d string) string {
	if d == "" {
		return "."
	}
	return path.Clean(d)
}

func lookupReplace(m map[string]string, mod, ver string) (string, bool) {
	if v, ok := m["replace:"+mod+"@"+ver]; ok {
		return v, true
	}
	v, ok := m["replace:"+mod]
	return v, ok
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Register wires the ecosystem into the registry (called from cmd/scp, in this order: spec, parsers, assembler).
func Register(r *manifests.Registry) error {
	if err := r.RegisterEcosystem(Spec()); err != nil {
		return err
	}
	if err := r.Register(NewModParser()); err != nil {
		return err
	}
	if err := r.Register(NewWorkParser()); err != nil {
		return err
	}
	return r.RegisterAssembler(NewAssembler())
}
